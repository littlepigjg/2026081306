package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"service-registry/internal/config"
	"service-registry/internal/handler"
	"service-registry/internal/metrics"
	"service-registry/internal/persistence"
	"service-registry/internal/reaper"
	"service-registry/internal/registry"
	"service-registry/pkg/logger"
)

var (
	version   = "1.0.0"
	buildTime = "unknown"
)

func main() {
	log := logger.Default()

	printBanner(log)

	log.Info("Starting Service Registry v%s", version)
	log.Info("Build time: %s, Go version: %s", buildTime, runtime.Version())

	cfg := config.LoadFromEnv()
	log.Info("Configuration:\n%s", cfg.Summary())

	store := registry.NewStore()

	persist := persistence.NewPersistence(cfg.PersistencePath, log)

	if snap, err := persist.Load(); err != nil {
		log.Warn("Failed to load snapshot: %v", err)
	} else if len(snap) > 0 {
		store.ReplaceAll(snap)
		log.Info("Restored %d instances from snapshot", len(snap))
	}

	h := handler.NewHandler(store, cfg, log)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	recorder := metrics.NewMetrics(store, cfg, log)
	mux.HandleFunc("/metrics", recorder.HandleMetrics)
	mux.HandleFunc("/version", handleVersion)
	mux.HandleFunc("/ready", handleReady(store))

	var wg sync.WaitGroup
	mainServer := &http.Server{
		Addr:         cfg.Address(),
		Handler:      logMiddleware(requestMetricsMiddleware(mux, recorder), log),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("HTTP server listening on %s", cfg.Address())
		if err := mainServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("HTTP server failed: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var backgroundWG sync.WaitGroup

	if cfg.EnableReaper {
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			reaper.NewReaper(store, cfg, log).Start(ctx)
		}()
	}

	if cfg.EnableMetrics {
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			recorder.Start(ctx)
		}()
	}

	metricsExporter := metrics.NewMetricsExporter(recorder, ":9090")
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("Metrics exporter starting on :9090")
		if err := metricsExporter.Start(); err != nil && err != http.ErrServerClosed {
			log.Warn("Metrics exporter failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	sig := <-sigCh
	log.Info("Received signal: %v, initiating graceful shutdown...", sig)

	secondSigCh := make(chan os.Signal, 1)
	signal.Notify(secondSigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		secondSig := <-secondSigCh
		log.Warn("Received second signal: %v, forcing immediate exit", secondSig)
		os.Exit(1)
	}()

	log.Info("Stopping background services...")
	cancel()
	backgroundWG.Wait()
	log.Info("Background services stopped")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownDelay)
	defer shutdownCancel()

	log.Info("Dumping registry snapshot...")
	snapshot := store.Snapshot()
	if err := persist.Save(snapshot); err != nil {
		log.Error("Failed to save snapshot: %v", err)
	} else {
		log.Info("Snapshot saved with %d instances", len(snapshot))
	}

	log.Info("Shutting down HTTP server (timeout: %v)...", cfg.ShutdownDelay)
	if err := mainServer.Shutdown(shutdownCtx); err != nil {
		log.Error("HTTP server shutdown error: %v", err)
	}

	log.Info("Shutting down metrics exporter...")
	metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer metricsShutdownCancel()
	if err := metricsExporter.Shutdown(metricsShutdownCtx); err != nil {
		log.Warn("Metrics exporter shutdown error: %v", err)
	}

	log.Info("Waiting for remaining requests to complete...")
	time.Sleep(500 * time.Millisecond)

	wg.Wait()

	log.Info("Service Registry stopped successfully.")
	fmt.Fprintf(os.Stdout, "Server stopped. Goodbye!\n")
}

func printBanner(log *logger.Logger) {
	banner := `
  ____                            _     
 / ___|  ___ _ ____   _____ _ __ ___(_)___ 
 \___ \ / _ \ '__\ \ / / _ \ '__/ __| / __|
  ___) |  __/ |   \ V /  __/ | | (__| \__ \
 |____/ \___|_|    \_/ \___|_|  \___|_|___/
`
	log.Info("%s", banner)
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"version":"%s","build_time":"%s","go_version":"%s"}`,
		version, buildTime, runtime.Version())
}

func handleReady(store *registry.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		count := store.Count()
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ready","instances":%d,"timestamp":"%s"}`,
			count, time.Now().Format(time.RFC3339))
	}
}

func logMiddleware(next http.Handler, log *logger.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Info("[HTTP] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		log.Debug("[HTTP] %s %s completed in %v", r.Method, r.URL.Path, duration)
	})
}

func requestMetricsMiddleware(next http.Handler, recorder *metrics.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.IncrementQueries()
		if strings.HasPrefix(r.URL.Path, "/api/v1/register") {
			recorder.IncrementRegistered()
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/heartbeat") {
			recorder.IncrementHeartbeats()
		}
		next.ServeHTTP(w, r)
	})
}