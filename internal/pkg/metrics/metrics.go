package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type ServiceMetrics struct {
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	dependencyHealth *prometheus.GaugeVec
}

func New(namespace string, registerer prometheus.Registerer) *ServiceMetrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	metrics := &ServiceMetrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of handled requests.",
		}, []string{"transport", "method", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Duration of handled requests.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"transport", "method"}),
		dependencyHealth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "dependency_health",
			Help:      "Health status of infrastructure dependencies.",
		}, []string{"dependency"}),
	}

	registerer.MustRegister(metrics.requestsTotal, metrics.requestDuration, metrics.dependencyHealth)

	return metrics
}

func (m *ServiceMetrics) ObserveRequest(transport, method string, startedAt time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	m.requestsTotal.WithLabelValues(transport, method, status).Inc()
	m.requestDuration.WithLabelValues(transport, method).Observe(time.Since(startedAt).Seconds())
}

func (m *ServiceMetrics) SetDependency(name string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1
	}

	m.dependencyHealth.WithLabelValues(name).Set(value)
}
