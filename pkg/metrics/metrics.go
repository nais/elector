package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	Namespace = "elector"

	LabelResourceType = "resource_type"
)

var (
	ElectionsWon = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "elections_won",
		Namespace: Namespace,
		Help:      "number of elections won",
	}, []string{})

	ElectionsLost = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "elections_lost",
		Namespace: Namespace,
		Help:      "number of elections lost",
	}, []string{})

	KubernetesResourcesWritten = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "kubernetes_resources_written",
		Namespace: Namespace,
		Help:      "number of kubernetes resources written to the cluster",
	}, []string{LabelResourceType})
)

func Register(registry prometheus.Registerer) {
	registry.MustRegister(
		KubernetesResourcesWritten,
		ElectionsWon,
		ElectionsLost,
	)
}
