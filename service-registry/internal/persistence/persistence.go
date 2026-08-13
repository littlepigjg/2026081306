package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"service-registry/internal/registry"
	"service-registry/pkg/logger"
)

type Persistence struct {
	filePath   string
	tempPath   string
	backupPath string
	log        *logger.Logger
	interval   time.Duration
	enabled    bool
}

func NewPersistence(filePath string, log *logger.Logger) *Persistence {
	dir := filepath.Dir(filePath)
	base := filepath.Base(filePath)
	return &Persistence{
		filePath:   filePath,
		tempPath:   filepath.Join(dir, base+".tmp"),
		backupPath: filepath.Join(dir, base+".bak"),
		log:        log,
		interval:   0,
		enabled:    true,
	}
}

func (p *Persistence) SetEnabled(enabled bool) {
	p.enabled = enabled
}

func (p *Persistence) IsEnabled() bool {
	return p.enabled
}

func (p *Persistence) SetInterval(interval time.Duration) {
	p.interval = interval
}

func (p *Persistence) Save(instances []*registry.ServiceInstance) error {
	if !p.enabled {
		return nil
	}

	p.log.Info("Saving registry snapshot to %s (%d instances)", p.filePath, len(instances))

	data, err := json.MarshalIndent(instances, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal instances: %w", err)
	}

	dataWithHeader := append([]byte("// Service Registry Snapshot\n// Generated at: "+time.Now().Format(time.RFC3339)+"\n\n"), data...)

	if err := os.WriteFile(p.tempPath, dataWithHeader, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if _, err := os.Stat(p.filePath); err == nil {
		if err := os.Rename(p.filePath, p.backupPath); err != nil {
			p.log.Warn("Failed to create backup: %v", err)
		}
	}

	if err := os.Rename(p.tempPath, p.filePath); err != nil {
		if err2 := os.Remove(p.tempPath); err2 != nil {
			p.log.Warn("Failed to remove temp file after rename failure: %v", err2)
		}
		return fmt.Errorf("failed to commit snapshot: %w", err)
	}

	p.log.Info("Registry snapshot saved successfully (%d bytes)", len(dataWithHeader))
	return nil
}

func (p *Persistence) Load() ([]*registry.ServiceInstance, error) {
	p.log.Info("Loading registry snapshot from %s", p.filePath)

	data, err := os.ReadFile(p.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.log.Info("No existing snapshot found, starting with empty registry")
			return make([]*registry.ServiceInstance, 0), nil
		}
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	cleaned := cleanData(data)

	var instances []*registry.ServiceInstance
	if err := json.Unmarshal(cleaned, &instances); err != nil {
		p.log.Warn("Failed to parse main snapshot, trying backup: %v", err)
		return p.loadBackup()
	}

	p.log.Info("Loaded %d instances from snapshot", len(instances))
	return instances, nil
}

func (p *Persistence) loadBackup() ([]*registry.ServiceInstance, error) {
	data, err := os.ReadFile(p.backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file: %w", err)
	}

	cleaned := cleanData(data)

	var instances []*registry.ServiceInstance
	if err := json.Unmarshal(cleaned, &instances); err != nil {
		return nil, fmt.Errorf("failed to parse backup snapshot: %w", err)
	}

	p.log.Info("Loaded %d instances from backup snapshot", len(instances))
	return instances, nil
}

func cleanData(data []byte) []byte {
	startIdx := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '[' || data[i] == '{' {
			startIdx = i
			break
		}
	}
	return data[startIdx:]
}

func (p *Persistence) Remove() error {
	if err := os.Remove(p.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove snapshot: %w", err)
	}
	if err := os.Remove(p.backupPath); err != nil && !os.IsNotExist(err) {
		p.log.Warn("Failed to remove backup file: %v", err)
	}
	p.log.Info("Snapshot files removed")
	return nil
}

func (p *Persistence) Exists() bool {
	_, err := os.Stat(p.filePath)
	return err == nil
}

func (p *Persistence) Path() string {
	return p.filePath
}

func (p *Persistence) BackupPath() string {
	return p.backupPath
}

func (p *Persistence) TempPath() string {
	return p.tempPath
}

func (p *Persistence) Directory() string {
	return filepath.Dir(p.filePath)
}

func EnsureDirectory(filePath string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return nil
}

func WriteSnapshotAtomic(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}

func WriteSnapshotWithBackup(filePath string, data []byte) error {
	if _, err := os.Stat(filePath); err == nil {
		backupPath := filePath + ".bak"
		if err := os.Rename(filePath, backupPath); err != nil {
			return err
		}
	}

	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}

func ReadSnapshotSafe(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err == nil {
		return data, nil
	}
	if os.IsNotExist(err) {
		return nil, nil
	}

	backupPath := filePath + ".bak"
	data, err2 := os.ReadFile(backupPath)
	if err2 != nil {
		return nil, fmt.Errorf("both main and backup files failed: main=%v, backup=%v", err, err2)
	}
	return data, nil
}