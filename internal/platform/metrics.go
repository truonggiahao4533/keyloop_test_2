package platform

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the service's Prometheus collectors on a private registry.
type Metrics struct {
	Registry      *prometheus.Registry
	HTTPRequests  *prometheus.CounterVec   // route, method, status
	HTTPDuration  *prometheus.HistogramVec // route
	ActionsLogged *prometheus.CounterVec   // action_type
	IngestRows    *prometheus.CounterVec   // result: inserted | updated | unchanged
}

// NewMetrics registers all collectors on a new registry.
func NewMetrics() *Metrics {
	m := &Metrics{
		Registry: prometheus.NewRegistry(),
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "HTTP requests by route template, method and status.",
		}, []string{"route", "method", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by route template.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"route"}),
		ActionsLogged: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "actions_logged_total", Help: "Vehicle actions logged (replays excluded).",
		}, []string{"action_type"}),
		IngestRows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ingest_rows_total", Help: "Ingested vehicle rows by result.",
		}, []string{"result"}),
	}
	m.Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.ActionsLogged, m.IngestRows,
	)
	return m
}

// Handler serves the registry in the Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// RegisterPool exports pgxpool statistics as db_pool_* metrics.
func (m *Metrics) RegisterPool(pool *pgxpool.Pool) {
	m.Registry.MustRegister(&poolCollector{pool: pool})
}

type poolCollector struct{ pool *pgxpool.Pool }

var (
	poolAcquired     = prometheus.NewDesc("db_pool_acquired_conns", "Connections currently in use.", nil, nil)
	poolIdle         = prometheus.NewDesc("db_pool_idle_conns", "Idle connections.", nil, nil)
	poolTotal        = prometheus.NewDesc("db_pool_total_conns", "Total open connections.", nil, nil)
	poolMax          = prometheus.NewDesc("db_pool_max_conns", "Maximum pool size.", nil, nil)
	poolAcquires     = prometheus.NewDesc("db_pool_acquire_total", "Successful connection acquires.", nil, nil)
	poolEmptyAcquire = prometheus.NewDesc("db_pool_empty_acquire_total", "Acquires that had to wait for a connection.", nil, nil)
	poolAcquireSecs  = prometheus.NewDesc("db_pool_acquire_duration_seconds_total", "Total time spent acquiring connections.", nil, nil)
)

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{poolAcquired, poolIdle, poolTotal, poolMax, poolAcquires, poolEmptyAcquire, poolAcquireSecs} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(poolAcquired, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(poolIdle, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(poolTotal, prometheus.GaugeValue, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(poolMax, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(poolAcquires, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(poolEmptyAcquire, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(poolAcquireSecs, prometheus.CounterValue, s.AcquireDuration().Seconds())
}
