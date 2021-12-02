package election

import (
	"context"
	"fmt"
	"github.com/nais/elector/pkg/metrics"
	"github.com/sirupsen/logrus"
	coordination_v1 "k8s.io/api/coordination/v1"
	core_v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type Candidate struct {
	client.Client
	Clock           clock.Clock
	Logger          logrus.FieldLogger
	ElectionResults chan<- string
	ElectionName    types.NamespacedName

	ownerReference meta_v1.OwnerReference
	hostname       string
}

func (c *Candidate) Start(ctx context.Context) error {
	var err error
	var lease *coordination_v1.Lease

	if err = c.setup(ctx); err != nil {
		return err
	}

	ticker := c.Clock.NewTicker(time.Minute * 1)
	for {
		c.Logger.Infof("Checking Lease %v", c.ElectionName)
		if lease, err = c.getLease(ctx); err != nil {
			return err
		}
		if lease == nil {
			c.Logger.Infof("No existing Lease, running campaign for %v", c.ElectionName)
			lease, err = c.runCampaign(ctx)
			if err != nil {
				return err
			}
		}
		c.updateElection(lease)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
			continue
		}
	}
}

func (c *Candidate) InjectClient(client client.Client) error {
	c.Client = client
	return nil
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
	lease := &coordination_v1.Lease{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      c.ElectionName.Name,
			Namespace: c.ElectionName.Namespace,
			OwnerReferences: []meta_v1.OwnerReference{
				c.ownerReference,
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
		c.ElectionResults <- *lease.Spec.HolderIdentity
	}
}

func (c *Candidate) setup(ctx context.Context) error {
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %w", err)
	}
	c.hostname = hostname

	pod := &core_v1.Pod{}

	key := client.ObjectKey{
		Namespace: c.ElectionName.Namespace,
		Name:      hostname,
	}
	err = c.Get(ctx, key, pod)
	if err != nil {
		return fmt.Errorf("unable to get current Pod: %w", err)
	}

	c.ownerReference = meta_v1.OwnerReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Name:       pod.Name,
		UID:        pod.UID,
	}
	return nil
}
