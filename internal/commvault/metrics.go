package commvault

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "api",
			Name:      "requests_total",
			Help:      "Total Commvault API requests made by the exporter.",
		}, []string{"endpoint", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "api",
			Name:      "request_duration_seconds",
			Help:      "Commvault API request duration by endpoint.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"endpoint"}),
	}
}

func (m *Metrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{m.requests, m.duration}
}

func (m *Metrics) observe(endpoint, code string, seconds float64) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(endpoint, code).Inc()
	m.duration.WithLabelValues(endpoint).Observe(seconds)
}
