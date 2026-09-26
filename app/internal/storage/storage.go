package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	GB = uint64(1) << 30
	MB = uint64(1) << 20

	InstallMinFree      = 30 * GB
	InstallDirMinFree   = 1 * GB
	RunningLowFree      = 10 * GB
	RunningCriticalFree = 5 * GB
)

type Level string

const (
	LevelOK       Level = "ok"
	LevelLow      Level = "low"
	LevelCritical Level = "critical"
	LevelUnknown  Level = "unknown"
)

func (l Level) rank() int {
	switch l {
	case LevelCritical:
		return 3
	case LevelLow:
		return 2
	case LevelUnknown:
		return 1
	}
	return 0
}

func Worst(levels ...Level) Level {
	out := LevelOK
	for _, l := range levels {
		if l.rank() > out.rank() {
			out = l
		}
	}
	return out
}

func RunningLevel(free uint64) Level {
	switch {
	case free < RunningCriticalFree:
		return LevelCritical
	case free < RunningLowFree:
		return LevelLow
	}
	return LevelOK
}

func DockerDataDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "rancher-desktop", "lima")
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "rancher-desktop")
	}
	return "/var/lib/docker"
}

func Human(b uint64) string {
	switch {
	case b >= 10*GB:
		return fmt.Sprintf("%.0f GB", float64(b)/float64(GB))
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(MB))
	}
	return fmt.Sprintf("%d KB", b>>10)
}

func ParseDF(out string) (free, total uint64, err error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("unexpected df output %q", out)
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, 0, fmt.Errorf("unexpected df output %q", out)
	}
	blocks, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	avail, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return avail << 10, blocks << 10, nil
}
