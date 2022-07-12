package election

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	coordination_v1 "k8s.io/api/coordination/v1"
	core_v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/nais/elector/pkg/logging"
	"github.com/nais/elector/pkg/metrics"
)

type Candidate struct {
	client.Client
	Clock           clock.Clock
	Logger          logrus.FieldLogger
	ElectionResults chan<- string
	ElectionName    types.NamespacedName

	ownerReference *meta_v1.OwnerReference
	hostname       string
	setupLock      sync.Mutex
	campaignLock   sync.Mutex
}

func AddCandidateToManager(mgr ctrl.Manager, logger logrus.FieldLogger, electionResults chan<- string, electionName types.NamespacedName) error {
	candidate := Candidate{
		Client:          mgr.GetClient(),
		Clock:           &clock.RealClock{},
		Logger:          logger.WithField(logging.FieldComponent, "Candidate"),
		ElectionResults: electionResults,
		ElectionName:    electionName,
	}

	err := mgr.AddReadyzCheck("candidate", candidate.readyz)
	if err != nil {
		return fmt.Errorf("failed to add candidate readiness check to controller-runtime manager: %w", err)
	}

	err = mgr.Add(&candidate)
	if err != nil {
		return fmt.Errorf("failed to add candidate runnable to controller-runtime manager: %w", err)
	}

	err = ctrl.NewControllerManagedBy(mgr).
		For(&coordination_v1.Lease{}).
		Complete(&candidate)
	if err != nil {
		return fmt.Errorf("failed to add candidate controller to controller-runtime manager: %w", err)
	}

	return nil
}

func (c *Candidate) readyz(_ *http.Request) error {
	if c.ownerReference == nil {
		return fmt.Errorf("candidate has not performed setup")
	}
	return nil
}

func (c *Candidate) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if c.ownerReference == nil {
		err := c.setup(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to initialize candidate: %w", err)
		}
	}

	c.Logger.Debugf("Checking reconcile request: %v", request.NamespacedName)
	if request.NamespacedName != c.ElectionName {
		return ctrl.Result{}, nil
	}

	return c.checkLease(ctx)
}

func (c *Candidate) Start(ctx context.Context) error {
	ticker := c.Clock.NewTicker(time.Minute * 1)

	if c.ownerReference == nil {
		err := c.setup(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize candidate: %w", err)
		}
	}

	for {
		_, err := c.checkLease(ctx)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
			continue
		}
	}
}

func (c *Candidate) checkLease(ctx context.Context) (ctrl.Result, error) {
	var err error
	var lease *coordination_v1.Lease

	c.Logger.Debugf("Checking Lease %v", c.ElectionName)
	if lease, err = c.getLease(ctx); err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, err
	}
	if lease == nil {
		c.Logger.Infof("No existing Lease, running campaign for %v", c.ElectionName)
		lease, err = c.runCampaign(ctx)
		if err != nil {
			err = fmt.Errorf("error during campaign: %w", err)
			c.Logger.Error(err)
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, err
		}
	}
	c.updateElection(lease)
	return ctrl.Result{}, nil
}

func (c *Candidate) getLease(ctx context.Context) (*coordination_v1.Lease, error) {
	var lease coordination_v1.Lease
	err := c.Get(ctx, c.ElectionName, &lease)
	switch {
	case k8serrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	default:
		return &lease, nil
	}
}

func (c *Candidate) runCampaign(ctx context.Context) (*coordination_v1.Lease, error) {
	c.campaignLock.Lock()
	defer c.campaignLock.Unlock()

	lease := &coordination_v1.Lease{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      c.ElectionName.Name,
			Namespace: c.ElectionName.Namespace,
			OwnerReferences: []meta_v1.OwnerReference{
				*c.ownerReference,
			},
		},
		Spec: coordination_v1.LeaseSpec{
			HolderIdentity: &c.hostname,
			AcquireTime: &meta_v1.MicroTime{
				Time: time.Now(),
			},
		},
	}

	err := c.Create(ctx, lease)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) || k8serrors.IsConflict(err) {
			metrics.ElectionsLost.WithLabelValues().Inc()
			c.Logger.Infof("Lost election %v", c.ElectionName)
			return c.getLease(ctx)
		} else {
			return nil, err
		}
	}
	metrics.ElectionsWon.WithLabelValues().Inc()
	c.Logger.Infof("Won election %v", c.ElectionName)
	return lease, nil
}

func (c *Candidate) updateElection(lease *coordination_v1.Lease) {
	if lease != nil {
		c.Logger.Debugf("Sending election results, leader is: %v", *lease.Spec.HolderIdentity)
		c.ElectionResults <- *lease.Spec.HolderIdentity
	}
}

func (c *Candidate) setup(ctx context.Context) error {
	c.setupLock.Lock()
	defer c.setupLock.Unlock()

	c.Logger.Info("Starting candidate setup")
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %w", err)
	}

	pod := &core_v1.Pod{}

	key := client.ObjectKey{
		Namespace: c.ElectionName.Namespace,
		Name:      hostname,
	}
	err = c.Get(ctx, key, pod)
	if err != nil {
		return fmt.Errorf("unable to get current Pod: %w", err)
	}

	c.hostname = hostname
	c.ownerReference = &meta_v1.OwnerReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Name:       pod.Name,
		UID:        pod.UID,
	}
	c.Logger.Info("Candidate setup complete")

	return nil
}
