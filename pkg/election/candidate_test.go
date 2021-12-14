//go:build integration

package election

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	coordination_v1 "k8s.io/api/coordination/v1"
	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s_runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"os"
	"path/filepath"
	"runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"testing"
	"time"
)

const (
	namespace    = "test-namespace"
	electionName = "test-election"
	testUID      = "test-uuid"
	notMe        = "not-my-hostname"
)

type testRig struct {
	t               *testing.T
	kubernetes      *envtest.Environment
	client          client.Client
	manager         ctrl.Manager
	hostname        string
	candidate       Candidate
	electionResults chan string
	fakeClock       clock.FakeClock
}

func testBinDirectory() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.testbin/"))
}

func newTestRig(t *testing.T) (*testRig, error) {
	err := os.Setenv("KUBEBUILDER_ASSETS", testBinDirectory())
	if err != nil {
		return nil, fmt.Errorf("failed to set environment variable: %w", err)
	}

	scheme := k8s_runtime.NewScheme()

	err = clientgoscheme.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add client schemes: %w", err)
	}

	rig := &testRig{
		t: t,
		kubernetes: &envtest.Environment{
			Scheme: scheme,
		},
	}

	t.Log("Starting Kubernetes")
	cfg, err := rig.kubernetes.Start()
	if err != nil {
		return nil, fmt.Errorf("setup Kubernetes test environment: %w", err)
	}

	t.Cleanup(func() {
		t.Log("Stopping Kubernetes")
		if err := rig.kubernetes.Stop(); err != nil {
			t.Errorf("failed to stop kubernetes test rig: %s", err)
		}
	})

	rig.manager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             rig.kubernetes.Scheme,
		MetricsBindAddress: "0",
	})
	if err != nil {
		return nil, fmt.Errorf("initialize manager: %w", err)
	}

	go func() {
		cache := rig.manager.GetCache()
		err := cache.Start(context.Background())
		if err != nil {
			t.Errorf("unable to start informer cache: %v", err)
		}
	}()
	rig.client = rig.manager.GetClient()

	hostname, err := os.Hostname()
	if err != nil {
		t.Errorf("unable to get hostname: %v", err)
		t.FailNow()
	}
	rig.hostname = hostname

	rig.electionResults = make(chan string)

	rig.fakeClock = clock.FakeClock{}
	rig.candidate = Candidate{
		Client:          rig.client,
		Clock:           &rig.fakeClock,
		Logger:          logrus.New(),
		ElectionResults: rig.electionResults,
		ElectionName: types.NamespacedName{
			Namespace: namespace,
			Name:      electionName,
		},
	}

	return rig, nil
}

func (rig testRig) assertExists(ctx context.Context, resource client.Object, objectKey client.ObjectKey) {
	rig.t.Helper()
	err := rig.client.Get(ctx, objectKey, resource)
	assert.NoError(rig.t, err)
	assert.NotNil(rig.t, resource)
}

func (rig testRig) assertNotExists(ctx context.Context, resource client.Object, objectKey client.ObjectKey) {
	rig.t.Helper()
	err := rig.client.Get(ctx, objectKey, resource)
	assert.True(rig.t, errors.IsNotFound(err), "the resource found in the cluster should not be there")
}

func (rig testRig) createForTest(ctx context.Context, obj client.Object) {
	kind := obj.DeepCopyObject().GetObjectKind()
	rig.t.Logf("Creating %s", describe(obj))
	if err := rig.client.Create(ctx, obj); err != nil {
		rig.t.Fatalf("resource %s cannot be persisted to fake Kubernetes: %s", describe(obj), err)
		return
	}
	// Create clears GVK for whatever reason, so we add it back here, so we can continue to use this object
	obj.GetObjectKind().SetGroupVersionKind(kind.GroupVersionKind())
	rig.t.Cleanup(func() {
		rig.t.Logf("Deleting %s", describe(obj))
		if err := rig.client.Delete(ctx, obj); err != nil {
			rig.t.Errorf("failed to delete resource %s: %s", describe(obj), err)
		}
	})
}

func (rig *testRig) commonSetupForTest(ctx context.Context) {
	rig.createForTest(ctx, &core_v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: namespace,
		},
	})
	rig.createForTest(ctx, &core_v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      rig.hostname,
			Namespace: namespace,
			UID:       testUID,
		},
		Spec: core_v1.PodSpec{
			Containers: []core_v1.Container{
				{
					Name:  "container",
					Image: "nginx:latest",
				},
			},
		},
	})
}

func TestCandidate_WinSimpleElection(t *testing.T) {
	rig, err := newTestRig(t)
	if err != nil {
		t.Errorf("unable to run controller integration tests: %s", err)
		t.FailNow()
	}

	// Allow 15 seconds for test to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	rig.commonSetupForTest(ctx)
	rig.assertNotExists(ctx, &coordination_v1.Lease{}, rig.candidate.ElectionName)

	run(t, rig, ctx)

	select {
	case <-ctx.Done():
		t.Logf("Context closed while waiting for results: %v", ctx.Err())
		t.FailNow()
	case result := <-rig.electionResults:
		assert.Equal(t, rig.hostname, result)
	}
}

func TestCandidate_WinRaceToElection(t *testing.T) {
	rig, err := newTestRig(t)
	if err != nil {
		t.Errorf("unable to run controller integration tests: %s", err)
		t.FailNow()
	}

	// Allow 15 seconds for test to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	rig.commonSetupForTest(ctx)
	lease := coordination_v1.Lease{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      rig.candidate.ElectionName.Name,
			Namespace: rig.candidate.ElectionName.Namespace,
		},
		Spec: coordination_v1.LeaseSpec{
			HolderIdentity: pointer.String(notMe),
			AcquireTime: &meta_v1.MicroTime{
				Time: time.Now(),
			},
		},
	}
	rig.createForTest(ctx, &lease)
	rig.assertExists(ctx, &coordination_v1.Lease{}, rig.candidate.ElectionName)

	go func() {
		time.Sleep(1 * time.Second)
		t.Log("Deleting existing lease")
		err := rig.client.Delete(ctx, &lease)
		if err != nil {
			t.Errorf("failed to delete lease")
			cancel()
		}
		rig.fakeClock.Step(1 * time.Minute)
	}()

	run(t, rig, ctx)

	select {
	case <-ctx.Done():
		t.Logf("Context closed while waiting for results: %v", ctx.Err())
		t.FailNow()
	case result := <-rig.electionResults:
		assert.Equal(t, notMe, result)
		select {
		case <-ctx.Done():
			t.Logf("Context closed while waiting for results: %v", ctx.Err())
			t.FailNow()
		case result := <-rig.electionResults:
			assert.Equal(t, rig.hostname, result)
		}
	}
}

func run(t *testing.T, rig *testRig, ctx context.Context) {
	go func() {
		err := rig.candidate.Start(ctx)
		switch {
		case err == context.Canceled:
			return
		case err != nil:
			t.Errorf("candidate errored out: %v", err)
		}
	}()
}

func describe(obj client.Object) string {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return fmt.Sprintf("%s/%s/%s: %s/%s", gvk.Group, gvk.Version, gvk.Kind, obj.GetNamespace(), obj.GetName())
}
