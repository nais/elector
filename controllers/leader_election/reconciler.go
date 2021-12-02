package leader_election

import (
	"context"
	"errors"
	"fmt"
	"github.com/nais/elector/pkg/metrics"
	"github.com/nais/elector/pkg/utils"
	coordination_v1 "k8s.io/api/coordination/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	requeueInterval   = time.Second * 10
	leaseWriteTimeout = time.Second * 2
)

func NewReconciler(mgr manager.Manager, logger *log.Logger, electionResults chan<- string) *LeaseReconciler {
	return &LeaseReconciler{
		Client:          mgr.GetClient(),
		Logger:          logger.WithFields(log.Fields{"component": "LeaseReconciler"}),
		ElectionResults: electionResults,
	}
}

type LeaseReconciler struct {
	client.Client
	Logger          log.FieldLogger
	ElectionResults chan<- string
	OwnerReference  metav1.OwnerReference
	Hostname        string
}

func (r *LeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: We only care about *our* lease, matching our name/namespace
	logger := r.Logger.WithFields(log.Fields{
		"leader_election": req.Name,
		"namespace":       req.Namespace,
	})

	logger.Infof("Processing request")

	return r.Process(ctx, req.NamespacedName, logger)
}

func (r *LeaseReconciler) Process(ctx context.Context, namespacedName types.NamespacedName, logger log.FieldLogger) (ctrl.Result, error) {
	var lease coordination_v1.Lease

	fail := func(err error) (ctrl.Result, error) {
		if err != nil {
			logger.Error(err)
		}
		cr := ctrl.Result{}

		if !errors.Is(err, utils.UnrecoverableError) {
			cr.RequeueAfter = requeueInterval
		}

		return cr, nil
	}

	err := r.Get(ctx, namespacedName, &lease)
	switch {
	case k8serrors.IsNotFound(err):
		err = r.election(ctx, namespacedName)
		if err != nil {
			return fail(fmt.Errorf("error while participating in election: %s", err))
		}
	case err != nil:
		return fail(fmt.Errorf("unable to retrieve resource from cluster: %s", err))
	default:
		r.ElectionResults <- *lease.Spec.HolderIdentity
	}

	return ctrl.Result{}, nil
}

func (r *LeaseReconciler) election(ctx context.Context, namespacedName types.NamespacedName) error {
	ctx, cancel := context.WithTimeout(ctx, leaseWriteTimeout)
	defer cancel()

	lease := coordination_v1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				r.OwnerReference,
			},
		},
		Spec: coordination_v1.LeaseSpec{
			HolderIdentity: &r.Hostname,
			AcquireTime: &metav1.MicroTime{
				Time: time.Now(),
			},
		},
	}

	err := r.Create(ctx, &lease)
	if err != nil {
		if k8serrors.IsAlreadyExists(err) || k8serrors.IsConflict(err) {
			metrics.ElectionsLost.WithLabelValues().Inc()
			return nil
		} else {
			return err
		}
	}

	// TODO: Do I need to handle my lease, or will I get reconcile on the create?
	metrics.ElectionsWon.WithLabelValues().Inc()
	return nil
}

func (r *LeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	initialize := func(ctx context.Context) error {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("unable to get hostname: %w", err)
		}
		r.Hostname = hostname

		// TODO: Figure out namespace we're in, and name of pod
		key := client.ObjectKey{
			Namespace: "",
			Name:      "",
		}
		pod := &v1.Pod{}
		err = r.Get(ctx, key, pod)
		if err != nil {
			return fmt.Errorf("failed to get current pod: %w", err)
		}

		// Block until done
		select {
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	err := mgr.Add(manager.RunnableFunc(initialize))
	if err != nil {
		return fmt.Errorf("unable to add initializer to manager: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&coordination_v1.Lease{}).
		Complete(r)
}
