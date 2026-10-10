package applog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const LogBudget int64 = 500 << 20
const logAge = 7 * 24 * time.Hour

// Stream opens a private, rotating log. Writes never retain an open descriptor
// across rotation, so the retention sweep can safely truncate an active file.
type Stream struct {
	mu   sync.Mutex
	path string
}

func NewStream(path string) (*Stream, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	err = f.Close()
	return &Stream{path: path}, err
}

func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for len(p) > 0 {
		info, err := os.Stat(s.path)
		if err != nil && !os.IsNotExist(err) {
			return total, err
		}
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		if size >= maxSize {
			for i := 4; i >= 1; i-- {
				from := s.path + fmt.Sprintf(".%d", i)
				if i == 4 {
					err = os.Remove(from)
				} else {
					err = os.Rename(from, s.path+fmt.Sprintf(".%d", i+1))
				}
				if err != nil && !os.IsNotExist(err) {
					return total, err
				}
			}
			if err := os.Rename(s.path, s.path+".1"); err != nil {
				return total, err
			}
			size = 0
		}
		n := min(len(p), int(maxSize-size))
		f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return total, err
		}
		written, err := f.Write(p[:n])
		closeErr := f.Close()
		total += written
		if err != nil {
			return total, err
		}
		if closeErr != nil {
			return total, closeErr
		}
		if written != n {
			return total, io.ErrShortWrite
		}
		p = p[n:]
	}
	return total, nil
}

func IsLog(name string) bool {
	return name == "care-engine-diagnostics.txt" || strings.HasSuffix(name, ".log") || strings.Contains(name, ".log.") ||
		(strings.HasPrefix(name, "care-log.") && strings.HasSuffix(name, ".log"))
}

// Maintain removes expired logs and enforces a shared disk budget. Active files
// are truncated rather than unlinked, preserving descriptors held by the app.
func Maintain(root string, now time.Time) error {
	type entry struct {
		path string
		info os.FileInfo
	}
	var files []entry
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !IsLog(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, entry{path, info})
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().Before(files[j].info.ModTime()) })
	for _, f := range files {
		if now.Sub(f.info.ModTime()) <= logAge && total <= LogBudget {
			continue
		}
		if strings.HasSuffix(f.path, ".log") && (filepath.Base(f.path) == logName || !strings.HasPrefix(filepath.Base(f.path), "care-log.")) {
			err = os.Truncate(f.path, 0)
		} else {
			err = os.Remove(f.path)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		total -= f.info.Size()
	}
	return nil
}
