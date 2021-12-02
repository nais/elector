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
)

type testRig struct {
	t          *testing.T
	kubernetes *envtest.Environment
	client     client.Client
	manager    ctrl.Manager
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

	return rig, nil
}

func (r testRig) assertExists(ctx context.Context, resource client.Object, objectKey client.ObjectKey) {
	r.t.Helper()
	err := r.client.Get(ctx, objectKey, resource)
	assert.NoError(r.t, err)
	assert.NotNil(r.t, resource)
}

func (r testRig) assertNotExists(ctx context.Context, resource client.Object, objectKey client.ObjectKey) {
	r.t.Helper()
	err := r.client.Get(ctx, objectKey, resource)
	assert.True(r.t, errors.IsNotFound(err), "the resource found in the cluster should not be there")
}

func (r testRig) createForTest(ctx context.Context, obj client.Object) {
	kind := obj.DeepCopyObject().GetObjectKind()
	r.t.Logf("Creating %s", describe(obj))
	if err := r.client.Create(ctx, obj); err != nil {
		r.t.Fatalf("resource %s cannot be persisted to fake Kubernetes: %s", describe(obj), err)
		return
	}
	// Create clears GVK for whatever reason, so we add it back here, so we can continue to use this object
	obj.GetObjectKind().SetGroupVersionKind(kind.GroupVersionKind())
	r.t.Cleanup(func() {
		r.t.Logf("Deleting %s", describe(obj))
		if err := r.client.Delete(ctx, obj); err != nil {
			r.t.Errorf("failed to delete resource %s: %s", describe(obj), err)
		}
	})
}

func TestCandidate_WinElection(t *testing.T) {
	rig, err := newTestRig(t)
	if err != nil {
		t.Errorf("unable to run controller integration tests: %s", err)
		t.FailNow()
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Errorf("unable to get hostname: %v", err)
		t.FailNow()
	}

	electionResults := make(chan string)

	fakeClock := clock.FakeClock{}
	candidate := Candidate{
		Client:          rig.client,
		Clock:           &fakeClock,
		Logger:          logrus.New(),
		ElectionResults: electionResults,
		ElectionName: types.NamespacedName{
			Namespace: namespace,
			Name:      electionName,
		},
	}

	// Allow 15 seconds for test to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	rig.createForTest(ctx, &core_v1.Namespace{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: namespace,
		},
	})
	rig.createForTest(ctx, &core_v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      hostname,
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

	rig.assertNotExists(ctx, &coordination_v1.Lease{}, candidate.ElectionName)

	go func() {
		err := candidate.Start(ctx)
		switch {
		case err == context.Canceled:
			return
		case err != nil:
			t.Errorf("candidate errored out: %v", err)
		}
	}()

	select {
	case <-ctx.Done():
		t.Logf("Context closed while waiting for results: %v", ctx.Err())
		t.FailNow()
	case result := <-electionResults:
		assert.Equal(t, candidate.hostname, result)
	}
}

func describe(obj client.Object) string {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return fmt.Sprintf("%s/%s/%s: %s/%s", gvk.Group, gvk.Version, gvk.Kind, obj.GetNamespace(), obj.GetName())
}
