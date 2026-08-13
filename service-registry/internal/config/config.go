package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerPort     int           `json:"server_port"`
	ReadTimeout    time.Duration `json:"read_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout"`
	IdleTimeout    time.Duration `json:"idle_timeout"`
	ShutdownDelay  time.Duration `json:"shutdown_delay"`
	ReaperInterval time.Duration `json:"reaper_interval"`
	MetricsInterval time.Duration `json:"metrics_interval"`
	PersistencePath string        `json:"persistence_path"`
	LogLevel       string        `json:"log_level"`
	MaxRequestSize int64         `json:"max_request_size"`
	EnableMetrics  bool          `json:"enable_metrics"`
	EnableReaper   bool          `json:"enable_reaper"`
	HealthCheckPath string       `json:"health_check_path"`
	APIVersion     string        `json:"api_version"`
	MaxTTL         int           `json:"max_ttl"`
	MinTTL         int           `json:"min_ttl"`
}

func Default() *Config {
	return &Config{
		ServerPort:      8080,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownDelay:   5 * time.Second,
		ReaperInterval:  5 * time.Second,
		MetricsInterval: 30 * time.Second,
		PersistencePath: "registry_dump.json",
		LogLevel:        "INFO",
		MaxRequestSize:  1 << 20,
		EnableMetrics:   true,
		EnableReaper:    true,
		HealthCheckPath: "/health",
		APIVersion:      "v1",
		MaxTTL:          3600,
		MinTTL:          1,
	}
}

func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	cfg.Validate()
	return cfg, nil
}

func (c *Config) Validate() error {
	if c.ServerPort <= 0 || c.ServerPort > 65535 {
		return fmt.Errorf("invalid server port: %d", c.ServerPort)
	}
	if c.ReaperInterval < 1*time.Second {
		return fmt.Errorf("reaper interval too short: %v", c.ReaperInterval)
	}
	if c.MetricsInterval < 1*time.Second {
		return fmt.Errorf("metrics interval too short: %v", c.MetricsInterval)
	}
	if c.ShutdownDelay < 1*time.Second {
		return fmt.Errorf("shutdown delay too short: %v", c.ShutdownDelay)
	}
	if c.ReadTimeout < 100*time.Millisecond {
		return fmt.Errorf("read timeout too short: %v", c.ReadTimeout)
	}
	if c.WriteTimeout < 100*time.Millisecond {
		return fmt.Errorf("write timeout too short: %v", c.WriteTimeout)
	}
	if c.MaxTTL < c.MinTTL {
		return fmt.Errorf("max TTL (%d) < min TTL (%d)", c.MaxTTL, c.MinTTL)
	}
	return nil
}

func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Config) Address() string {
	return fmt.Sprintf(":%d", c.ServerPort)
}

func LoadFromEnv() *Config {
	cfg := Default()
	if v := os.Getenv("REGISTRY_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.ServerPort = port
		}
	}
	if v := os.Getenv("REGISTRY_REAPER_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ReaperInterval = d
		}
	}
	if v := os.Getenv("REGISTRY_METRICS_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MetricsInterval = d
		}
	}
	if v := os.Getenv("REGISTRY_PERSISTENCE_PATH"); v != "" {
		cfg.PersistencePath = v
	}
	if v := os.Getenv("REGISTRY_LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToUpper(v)
	}
	if v := os.Getenv("REGISTRY_SHUTDOWN_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ShutdownDelay = d
		}
	}
	if v := os.Getenv("REGISTRY_ENABLE_METRICS"); v != "" {
		cfg.EnableMetrics = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("REGISTRY_ENABLE_REAPER"); v != "" {
		cfg.EnableReaper = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("REGISTRY_MIN_TTL"); v != "" {
		if ttl, err := strconv.Atoi(v); err == nil {
			cfg.MinTTL = ttl
		}
	}
	if v := os.Getenv("REGISTRY_MAX_TTL"); v != "" {
		if ttl, err := strconv.Atoi(v); err == nil {
			cfg.MaxTTL = ttl
		}
	}
	cfg.Validate()
	return cfg
}

func (c *Config) WithOverrides(overrides map[string]string) *Config {
	cfg := *c
	for k, v := range overrides {
		switch strings.ToUpper(k) {
		case "PORT":
			if port, err := strconv.Atoi(v); err == nil {
				cfg.ServerPort = port
			}
		case "REAPER_INTERVAL":
			if d, err := time.ParseDuration(v); err == nil {
				cfg.ReaperInterval = d
			}
		case "METRICS_INTERVAL":
			if d, err := time.ParseDuration(v); err == nil {
				cfg.MetricsInterval = d
			}
		case "PERSISTENCE_PATH":
			cfg.PersistencePath = v
		case "LOG_LEVEL":
			cfg.LogLevel = strings.ToUpper(v)
		}
	}
	cfg.Validate()
	return &cfg
}

func (c *Config) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Server Port: %d\n", c.ServerPort))
	sb.WriteString(fmt.Sprintf("Reaper Interval: %v\n", c.ReaperInterval))
	sb.WriteString(fmt.Sprintf("Metrics Interval: %v\n", c.MetricsInterval))
	sb.WriteString(fmt.Sprintf("Persistence Path: %s\n", c.PersistencePath))
	sb.WriteString(fmt.Sprintf("Log Level: %s\n", c.LogLevel))
	sb.WriteString(fmt.Sprintf("Shutdown Delay: %v\n", c.ShutdownDelay))
	sb.WriteString(fmt.Sprintf("Enable Reaper: %v\n", c.EnableReaper))
	sb.WriteString(fmt.Sprintf("Enable Metrics: %v\n", c.EnableMetrics))
	sb.WriteString(fmt.Sprintf("TTL Range: %d - %d seconds\n", c.MinTTL, c.MaxTTL))
	return sb.String()
}