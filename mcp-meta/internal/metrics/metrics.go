package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ToolCallsTotal counts tool calls by tool name and status.
	ToolCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_tool_calls_total",
		Help: "Total tool calls by tool name and status",
	}, []string{"tool", "status"})

	// ToolCallDuration records tool call duration in seconds.
	ToolCallDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_tool_call_duration_seconds",
		Help:    "Tool call duration in seconds",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"tool"})

	// PodSpawnsTotal counts pod spawn events.
	PodSpawnsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_pod_spawns_total",
		Help: "Total pod spawn events",
	}, []string{"pod"})

	// PodPoolSize tracks current number of pods per pool.
	PodPoolSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_pod_pool_size",
		Help: "Current number of pods in pool",
	}, []string{"pod"})

	// PodPendingRequests tracks in-flight requests per pool.
	PodPendingRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_pod_pending_requests",
		Help: "Current pending request count per pool",
	}, []string{"pod"})

	// CircuitBreakerState tracks circuit breaker state per pod type.
	// 0=closed, 1=half-open, 2=open
	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
	}, []string{"pod"})

	// RetryTotal counts retry attempts.
	RetryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_retry_total",
		Help: "Total retry attempts by tool and attempt number",
	}, []string{"tool", "attempt"})
)
