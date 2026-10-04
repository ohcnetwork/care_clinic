// Package applog writes the application's diagnostic log to a file on disk.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const maxLine = 8 << 10

type Logger struct {
	// OnFatal is called by Fatal instead of exiting, letting the app report the
	// failure the way it reports every other one. Nil means exit.
	OnFatal func(string)

	mu   sync.Mutex
	f    *os.File
	path string
	size int64
}

func Open() *Logger { return openWithDir(DefaultLogDir()) }

// openWithDir is Open against a given directory, for tests
func openWithDir(dir string) *Logger {
	l := &Logger{}
	if dir == "" {
		return l
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return l
	}
	path := filepath.Join(dir, logName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return l
	}
	l.f, l.path = f, path
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
	l.Write(sessionRule())
	return l
}

func (l *Logger) Write(line string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) > maxLine {
		line = line[:maxLine] + " ...[truncated]"
	}
	b := []byte(time.Now().Format("2006-01-02T15:04:05.000Z07:00") + "  " + line + "\n")
	l.rotateIfFull(len(b))
	n, err := l.f.Write(b)
	if err != nil {
		return // nothing useful to do; never propagate
	}
	l.size += int64(n)
}

func (l *Logger) Writef(format string, args ...any) { l.Write(fmt.Sprintf(format, args...)) }

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

func (l *Logger) Folder() string {
	if p := l.Path(); p != "" {
		return filepath.Dir(p)
	}
	return ""
}

func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	_ = l.f.Close()
	l.f = nil
}

func (l *Logger) PurgeFolder() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	dir := ""
	if l.path != "" {
		dir = filepath.Dir(l.path)
	}
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	l.mu.Unlock()

	if dir == "" {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}

	fresh := openWithDir(dir)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.f, l.path, l.size = fresh.f, fresh.path, fresh.size
	return nil
}

func (l *Logger) Header(version, installDir, clinicName string) {
	l.Writef("CARE Clinic %s · %s/%s · Go %s", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	l.Writef("install dir: %s", fallback(installDir, "(none yet)"))
	l.Writef("clinic name: %s", fallback(clinicName, "(unset)"))
	l.Writef("log file: %s", l.Path())
}

func fallback(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

func sessionRule() string {
	return "──────── session start ────────"
}

const wailsTag = "wails "

func (l *Logger) Print(m string)   { l.Write(wailsTag + m) }
func (l *Logger) Trace(m string)   { l.Write(wailsTag + "TRACE " + m) }
func (l *Logger) Debug(m string)   { l.Write(wailsTag + "DEBUG " + m) }
func (l *Logger) Info(m string)    { l.Write(wailsTag + "INFO  " + m) }
func (l *Logger) Warning(m string) { l.Write(wailsTag + "WARN  " + m) }
func (l *Logger) Error(m string)   { l.Write(wailsTag + "ERROR " + m) }

func (l *Logger) Fatal(m string) {
	l.Write(wailsTag + "FATAL " + m)
	if l != nil && l.OnFatal != nil {
		l.OnFatal(m)
		return
	}
	l.Close()
	os.Exit(1)
}
