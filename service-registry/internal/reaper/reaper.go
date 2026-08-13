package reaper

import (
	"context"
	"fmt"
	"sync"
	"time"

	"service-registry/internal/config"
	"service-registry/internal/registry"
	"service-registry/pkg/logger"
)

type Reaper struct {
	store    *registry.Store
	cfg      *config.Config
	log      *logger.Logger
	interval time.Duration
	stopCh   chan struct{}
	mu       sync.Mutex
	running  bool
	stats    ReaperStats
}

type ReaperStats struct {
	TotalRuns      int       `json:"total_runs"`
	TotalDeleted   int       `json:"total_deleted"`
	LastRunAt      time.Time `json:"last_run_at"`
	LastRunDeleted int       `json:"last_run_deleted"`
}

func NewReaper(store *registry.Store, cfg *config.Config, log *logger.Logger) *Reaper {
	return &Reaper{
		store:    store,
		cfg:      cfg,
		log:      log,
		interval: cfg.ReaperInterval,
		stopCh:   make(chan struct{}),
	}
}

func (r *Reaper) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	r.log.Info("TTL Reaper started with interval: %v", r.interval)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("TTL Reaper context cancelled, stopping")
			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
			return
		case <-r.stopCh:
			r.log.Info("TTL Reaper received stop signal")
			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
			return
		case <-ticker.C:
			r.runOnce()
		}
	}
}

func (r *Reaper) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		close(r.stopCh)
		r.running = false
	}
}

func (r *Reaper) runOnce() {
	now := time.Now()
	deleted := r.store.PurgeExpiredAt(now)

	r.mu.Lock()
	r.stats.TotalRuns++
	r.stats.TotalDeleted += deleted
	r.stats.LastRunAt = now
	r.stats.LastRunDeleted = deleted
	r.mu.Unlock()

	if deleted > 0 {
		r.log.Info("TTL Reaper cleaned up %d expired instances (total running: %d)",
			deleted, r.store.Count())
	} else {
		r.log.Debug("TTL Reaper scan complete, no expired instances found (total: %d)",
			r.store.Count())
	}
}

func (r *Reaper) RunOnceNow() int {
	r.runOnce()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats.LastRunDeleted
}

func (r *Reaper) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

func (r *Reaper) Stats() ReaperStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

func (r *Reaper) SetInterval(interval time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interval = interval
}

func (r *Reaper) Interval() time.Duration {
	return r.interval
}

func (r *Reaper) ForcePurge() int {
	return r.store.DeleteExpired()
}

type ReaperManager struct {
	reapers map[string]*Reaper
	mu      sync.RWMutex
	log     *logger.Logger
}

func NewReaperManager(log *logger.Logger) *ReaperManager {
	return &ReaperManager{
		reapers: make(map[string]*Reaper),
		log:     log,
	}
}

func (rm *ReaperManager) Register(name string, reaper *Reaper) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.reapers[name] = reaper
}

func (rm *ReaperManager) Unregister(name string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.reapers, name)
}

func (rm *ReaperManager) Get(name string) (*Reaper, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	r, ok := rm.reapers[name]
	if !ok {
		return nil, fmt.Errorf("reaper %s not found", name)
	}
	return r, nil
}

func (rm *ReaperManager) StopAll() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for name, r := range rm.reapers {
		r.Stop()
		delete(rm.reapers, name)
	}
	rm.log.Info("All reapers stopped")
}

func (rm *ReaperManager) List() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	names := make([]string, 0, len(rm.reapers))
	for name := range rm.reapers {
		names = append(names, name)
	}
	return names
}

type ReaperConfig struct {
	Enabled       bool          `json:"enabled"`
	Interval      time.Duration `json:"interval"`
	BatchSize     int           `json:"batch_size"`
	MaxLockTime   time.Duration `json:"max_lock_time"`
	NotifyChannel string        `json:"notify_channel"`
}

func DefaultReaperConfig() ReaperConfig {
	return ReaperConfig{
		Enabled:       true,
		Interval:      5 * time.Second,
		BatchSize:     100,
		MaxLockTime:   2 * time.Second,
		NotifyChannel: "",
	}
}

func (rc ReaperConfig) Validate() error {
	if rc.Interval < 1*time.Second {
		return fmt.Errorf("reaper interval must be at least 1 second, got %v", rc.Interval)
	}
	if rc.BatchSize <= 0 {
		return fmt.Errorf("batch size must be positive, got %d", rc.BatchSize)
	}
	return nil
}