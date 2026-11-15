package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, by method, route and status.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)

	ServiceOrderStatusTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "service_order_status_transitions_total",
			Help: "Total number of service order status transitions, by new status.",
		},
		[]string{"status"},
	)

	ServiceOrderNotificationFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "service_order_notification_failures_total",
		Help: "Total number of failed customer status-change notifications.",
	})

	// OutboxPending is the number of outbox rows not published yet (refreshed by the relay).
	OutboxPending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_pending",
		Help: "Number of outbox rows waiting to be published to SNS.",
	})

	SagaStartedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "saga_started_total",
		Help: "Total number of approval sagas started.",
	})
	SagaCompletedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "saga_completed_total",
		Help: "Total number of approval sagas completed (service order finished).",
	})
	SagaCompensatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "saga_compensated_total",
		Help: "Total number of approval sagas compensated (compensation commands sent).",
	})

	// SagaStepDuration measures, per step, the time between the command that starts the step and
	// the reply that closes it.
	SagaStepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "saga_step_duration_seconds",
			Help:    "Time from the command that starts a saga step to its reply, by step.",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900, 3600, 86400},
		},
		[]string{"step"},
	)
)

func init() {
	prometheus.MustRegister(
		HTTPRequestDuration,
		ServiceOrderStatusTransitionsTotal,
		ServiceOrderNotificationFailuresTotal,
		OutboxPending,
		SagaStartedTotal,
		SagaCompletedTotal,
		SagaCompensatedTotal,
		SagaStepDuration,
	)
}
