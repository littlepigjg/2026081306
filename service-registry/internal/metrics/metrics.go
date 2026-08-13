package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"service-registry/internal/config"
	"service-registry/internal/registry"
	"service-registry/pkg/logger"
)

type Metrics struct {
	store    *registry.Store
	cfg      *config.Config
	log      *logger.Logger
	interval time.Duration
	stopCh   chan struct{}
	mu       sync.Mutex
	running  bool
	counters MetricsCounters
}

type MetricsCounters struct {
	TotalRegistered   int64         `json:"total_registered"`
	TotalDeregistered int64         `json:"total_deregistered"`
	TotalHeartbeats   int64         `json:"total_heartbeats"`
	TotalQueries      int64         `json:"total_queries"`
	TotalErrors       int64         `json:"total_errors"`
	LastSnapshotAt    time.Time     `json:"last_snapshot_at"`
	Snapshots         []SnapshotEntry `json:"snapshots"`
}

type SnapshotEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Total     int               `json:"total"`
	Services  map[string]int    `json:"services"`
}

func NewMetrics(store *registry.Store, cfg *config.Config, log *logger.Logger) *Metrics {
	return &Metrics{
		store:    store,
		cfg:      cfg,
		log:      log,
		interval: cfg.MetricsInterval,
		stopCh:   make(chan struct{}),
		counters: MetricsCounters{
			Snapshots: make([]SnapshotEntry, 0, 10),
		},
	}
}

func (m *Metrics) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.log.Info("Metrics collector started with interval: %v", m.interval)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Metrics collector context cancelled, stopping")
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
			return
		case <-m.stopCh:
			m.log.Info("Metrics collector received stop signal")
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
			return
		case <-ticker.C:
			m.collect()
		}
	}
}

func (m *Metrics) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		close(m.stopCh)
		m.running = false
	}
}

func (m *Metrics) collect() {
	now := time.Now()
	stats := m.store.Stats()
	total := m.store.Count()

	var serviceNames []string
	var totalInstances int
	for name, count := range stats {
		serviceNames = append(serviceNames, name)
		totalInstances += count
	}

	sort.Strings(serviceNames)

	m.mu.Lock()
	m.counters.LastSnapshotAt = now
	entry := SnapshotEntry{
		Timestamp: now,
		Total:     total,
		Services:  stats,
	}
	m.counters.Snapshots = append(m.counters.Snapshots, entry)
	if len(m.counters.Snapshots) > 10 {
		m.counters.Snapshots = m.counters.Snapshots[len(m.counters.Snapshots)-10:]
	}
	m.mu.Unlock()

	var serviceSummary string
	if len(serviceNames) <= 5 {
		for _, name := range serviceNames {
			serviceSummary += fmt.Sprintf("%s=%d ", name, stats[name])
		}
	} else {
		serviceSummary = fmt.Sprintf("(%d services)", len(serviceNames))
	}

	m.log.Info("[Metrics] Total instances: %d | Services: %s", total, serviceSummary)
}

func (m *Metrics) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	m.mu.Lock()
	counters := m.counters
	m.mu.Unlock()

	currentStats := m.store.Stats()
	total := m.store.Count()
	names := m.store.ListNames()

	resp := map[string]interface{}{
		"uptime_metrics": map[string]interface{}{
			"total_registered":   counters.TotalRegistered,
			"total_deregistered": counters.TotalDeregistered,
			"total_heartbeats":   counters.TotalHeartbeats,
			"total_queries":      counters.TotalQueries,
			"total_errors":       counters.TotalErrors,
			"last_snapshot_at":   counters.LastSnapshotAt.Format(time.RFC3339),
		},
		"current_state": map[string]interface{}{
			"total_instances": total,
			"service_count":   len(names),
			"service_stats":   currentStats,
			"service_names":   names,
		},
		"snapshots": counters.Snapshots,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (m *Metrics) IncrementRegistered() {
	m.mu.Lock()
	m.counters.TotalRegistered++
	m.mu.Unlock()
}

func (m *Metrics) IncrementDeregistered() {
	m.mu.Lock()
	m.counters.TotalDeregistered++
	m.mu.Unlock()
}

func (m *Metrics) IncrementHeartbeats() {
	m.mu.Lock()
	m.counters.TotalHeartbeats++
	m.mu.Unlock()
}

func (m *Metrics) IncrementQueries() {
	m.mu.Lock()
	m.counters.TotalQueries++
	m.mu.Unlock()
}

func (m *Metrics) IncrementErrors() {
	m.mu.Lock()
	m.counters.TotalErrors++
	m.mu.Unlock()
}

func (m *Metrics) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *Metrics) SetInterval(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interval = interval
}

func (m *Metrics) Interval() time.Duration {
	return m.interval
}

func (m *Metrics) RunOnceNow() {
	m.collect()
}

func (m *Metrics) GetSnapshot() SnapshotEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.counters.Snapshots) == 0 {
		return SnapshotEntry{}
	}
	return m.counters.Snapshots[len(m.counters.Snapshots)-1]
}

func (m *Metrics) HandleMetricsPrometheus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	m.mu.Lock()
	counters := m.counters
	m.mu.Unlock()

	currentStats := m.store.Stats()
	total := m.store.Count()

	var output string
	output += fmt.Sprintf("# HELP registry_registered_total Total number of service registrations\n")
	output += fmt.Sprintf("# TYPE registry_registered_total counter\n")
	output += fmt.Sprintf("registry_registered_total %d\n", counters.TotalRegistered)
	output += fmt.Sprintf("\n")
	output += fmt.Sprintf("# HELP registry_deregistered_total Total number of service deregistrations\n")
	output += fmt.Sprintf("# TYPE registry_deregistered_total counter\n")
	output += fmt.Sprintf("registry_deregistered_total %d\n", counters.TotalDeregistered)
	output += fmt.Sprintf("\n")
	output += fmt.Sprintf("# HELP registry_active_instances Current number of active instances\n")
	output += fmt.Sprintf("# TYPE registry_active_instances gauge\n")
	output += fmt.Sprintf("registry_active_instances %d\n", total)
	output += fmt.Sprintf("\n")
	output += fmt.Sprintf("# HELP registry_service_instances Number of instances per service\n")
	output += fmt.Sprintf("# TYPE registry_service_instances gauge\n")
	for name, count := range currentStats {
		output += fmt.Sprintf("registry_service_instances{name=\"%s\"} %d\n", name, count)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(output))
}

type MetricsMiddleware struct {
	metrics *Metrics
}

func NewMetricsMiddleware(metrics *Metrics) *MetricsMiddleware {
	return &MetricsMiddleware{metrics: metrics}
}

func (mw *MetricsMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mw.metrics.IncrementQueries()
		next.ServeHTTP(w, r)
	})
}

type MetricsExporter struct {
	metrics    *Metrics
	httpServer *http.Server
	mux        *http.ServeMux
}

func NewMetricsExporter(metrics *Metrics, addr string) *MetricsExporter {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metrics.HandleMetrics)
	mux.HandleFunc("/metrics/prometheus", metrics.HandleMetricsPrometheus)
	mux.HandleFunc("/metrics/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	return &MetricsExporter{
		metrics: metrics,
		mux:     mux,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

func (me *MetricsExporter) Start() error {
	me.metrics.log.Info("Metrics exporter starting on %s", me.httpServer.Addr)
	return me.httpServer.ListenAndServe()
}

func (me *MetricsExporter) Shutdown(ctx context.Context) error {
	return me.httpServer.Shutdown(ctx)
}