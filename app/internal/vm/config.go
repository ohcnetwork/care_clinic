package vm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Config sizes the VM. Zero values fall back to defaults.
type Config struct {
	CPUs     int `json:"cpus"`
	MemoryMB int `json:"memory_mb"`
	DiskGB   int `json:"disk_gb"`
}

const (
	defaultMemoryMB = 4096
	defaultDiskGB   = 64
	minMemoryMB     = 2048
)

func configPath() string { return filepath.Join(StateDir(), "config.json") }

// LoadConfig reads the saved sizing, filling in defaults.
func LoadConfig() Config {
	var c Config
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c.withDefaults(runtime.NumCPU())
}

func SaveConfig(c Config) error {
	if err := os.MkdirAll(StateDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0o600)
}

func (c Config) withDefaults(hostCPUs int) Config {
	if c.CPUs <= 0 {
		c.CPUs = min(max(hostCPUs/2, 2), 4)
	}
	if hostCPUs > 0 && c.CPUs > hostCPUs {
		c.CPUs = hostCPUs
	}
	if c.MemoryMB <= 0 {
		c.MemoryMB = defaultMemoryMB
	}
	c.MemoryMB = max(c.MemoryMB, minMemoryMB)
	if c.DiskGB <= 0 {
		c.DiskGB = defaultDiskGB
	}
	return c
}

var errNotInstalled = errors.New("the CARE engine is not installed")
