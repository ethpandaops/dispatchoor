package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "dispatchoor"

// Metrics contains all Prometheus metrics for dispatchoor.
type Metrics struct {
	// Jobs.
	JobsCreated   *prometheus.CounterVec
	JobsTriggered *prometheus.CounterVec
	JobsCompleted *prometheus.CounterVec
	JobsFailed    *prometheus.CounterVec
	JobsCancelled *prometheus.CounterVec

	// Queue.
	QueueSize *prometheus.GaugeVec

	// Runners.
	RunnersTotal  *prometheus.GaugeVec
	RunnersOnline *prometheus.GaugeVec
	RunnersBusy   *prometheus.GaugeVec

	// HTTP.
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

	// Dispatcher.
	DispatcherCyclesTotal     prometheus.Counter
	DispatcherDispatchesTotal prometheus.Counter
	DispatcherErrorsTotal     prometheus.Counter
	DispatcherLastCycleTime   prometheus.Gauge

	// GitHub API.
	GitHubAPIRequestsTotal   *prometheus.CounterVec
	GitHubAPIErrorsTotal     *prometheus.CounterVec
	GitHubRateLimitRemaining prometheus.Gauge

	// Build info.
	BuildInfo *prometheus.GaugeVec
}

// New creates a new Metrics instance and registers all metrics.
func New() *Metrics {
	m := &Metrics{
		// Jobs.
		JobsCreated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "jobs_created_total",
				Help:      "Total number of jobs created",
			},
			[]string{"group"},
		),
		JobsTriggered: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "jobs_triggered_total",
				Help:      "Total number of jobs triggered",
			},
			[]string{"group"},
		),
		JobsCompleted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "jobs_completed_total",
				Help:      "Total number of jobs completed successfully",
			},
			[]string{"group"},
		),
		JobsFailed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "jobs_failed_total",
				Help:      "Total number of jobs failed",
			},
			[]string{"group"},
		),
		JobsCancelled: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "jobs_cancelled_total",
				Help:      "Total number of jobs cancelled",
			},
			[]string{"group"},
		),

		// Queue.
		QueueSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "queue_size",
				Help:      "Current number of jobs in queue",
			},
			[]string{"group", "status"},
		),

		// Runners.
		RunnersTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "runners_total",
				Help:      "Total number of runners",
			},
			[]string{"group"},
		),
		RunnersOnline: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "runners_online",
				Help:      "Number of online runners",
			},
			[]string{"group"},
		),
		RunnersBusy: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "runners_busy",
				Help:      "Number of busy runners",
			},
			[]string{"group"},
		),

		// HTTP.
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),

		// Dispatcher.
		DispatcherCyclesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "dispatcher_cycles_total",
				Help:      "Total number of dispatcher cycles",
			},
		),
		DispatcherDispatchesTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "dispatcher_dispatches_total",
				Help:      "Total number of jobs dispatched",
			},
		),
		DispatcherErrorsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "dispatcher_errors_total",
				Help:      "Total number of dispatcher errors",
			},
		),
		DispatcherLastCycleTime: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "dispatcher_last_cycle_timestamp",
				Help:      "Timestamp of the last dispatcher cycle",
			},
		),

		// GitHub API.
		GitHubAPIRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "github_api_requests_total",
				Help:      "Total number of GitHub API requests",
			},
			[]string{"endpoint"},
		),
		GitHubAPIErrorsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "github_api_errors_total",
				Help:      "Total number of GitHub API errors",
			},
			[]string{"endpoint"},
		),
		GitHubRateLimitRemaining: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "github_rate_limit_remaining",
				Help:      "Remaining GitHub API rate limit",
			},
		),

		// Build info.
		BuildInfo: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "build_info",
				Help:      "Build information",
			},
			[]string{"version", "commit", "date"},
		),
	}

	return m
}

// SetBuildInfo sets the build info metric.
func (m *Metrics) SetBuildInfo(version, commit, date string) {
	m.BuildInfo.WithLabelValues(version, commit, date).Set(1)
}

// RecordJobCreated increments the jobs created counter.
func (m *Metrics) RecordJobCreated(group string) {
	m.JobsCreated.WithLabelValues(group).Inc()
}

// RecordJobTriggered increments the jobs triggered counter.
func (m *Metrics) RecordJobTriggered(group string) {
	m.JobsTriggered.WithLabelValues(group).Inc()
}

// RecordJobCompleted increments the jobs completed counter.
func (m *Metrics) RecordJobCompleted(group string) {
	m.JobsCompleted.WithLabelValues(group).Inc()
}

// RecordJobFailed increments the jobs failed counter.
func (m *Metrics) RecordJobFailed(group string) {
	m.JobsFailed.WithLabelValues(group).Inc()
}

// RecordJobCancelled increments the jobs cancelled counter.
func (m *Metrics) RecordJobCancelled(group string) {
	m.JobsCancelled.WithLabelValues(group).Inc()
}

// SetQueueSize sets the queue size gauge.
func (m *Metrics) SetQueueSize(group, status string, size float64) {
	m.QueueSize.WithLabelValues(group, status).Set(size)
}

// SetRunnerCounts sets runner count gauges.
func (m *Metrics) SetRunnerCounts(group string, total, online, busy float64) {
	m.RunnersTotal.WithLabelValues(group).Set(total)
	m.RunnersOnline.WithLabelValues(group).Set(online)
	m.RunnersBusy.WithLabelValues(group).Set(busy)
}

// RecordHTTPRequest records an HTTP request.
func (m *Metrics) RecordHTTPRequest(method, path, status string, duration float64) {
	m.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
}

// RecordDispatcherCycle records a dispatcher cycle.
func (m *Metrics) RecordDispatcherCycle() {
	m.DispatcherCyclesTotal.Inc()
	m.DispatcherLastCycleTime.SetToCurrentTime()
}

// RecordDispatch records a successful dispatch.
func (m *Metrics) RecordDispatch() {
	m.DispatcherDispatchesTotal.Inc()
}

// RecordDispatcherError records a dispatcher error.
func (m *Metrics) RecordDispatcherError() {
	m.DispatcherErrorsTotal.Inc()
}

// RecordGitHubAPIRequest records a GitHub API request.
func (m *Metrics) RecordGitHubAPIRequest(endpoint string) {
	m.GitHubAPIRequestsTotal.WithLabelValues(endpoint).Inc()
}

// RecordGitHubAPIError records a GitHub API error.
func (m *Metrics) RecordGitHubAPIError(endpoint string) {
	m.GitHubAPIErrorsTotal.WithLabelValues(endpoint).Inc()
}

// SetGitHubRateLimit sets the GitHub rate limit remaining gauge.
func (m *Metrics) SetGitHubRateLimit(remaining float64) {
	m.GitHubRateLimitRemaining.Set(remaining)
}
