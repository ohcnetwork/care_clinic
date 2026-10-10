package applog

import (
	"os"
	"path/filepath"
	"strconv"
)

var (
	maxSize  int64 = 10 << 20
	maxFiles       = 5
)

func (l *Logger) slot(n int) string {
	if n == 0 {
		return l.path
	}
	return filepath.Join(filepath.Dir(l.path), logStem+"."+strconv.Itoa(n)+".log")
}

func (l *Logger) rotateIfFull(next int) {
	if l.f != nil {
		if st, err := l.f.Stat(); err == nil {
			l.size = st.Size()
		}
	}
	if l.f == nil || l.size+int64(next) <= maxSize {
		return
	}
	_ = l.f.Close()
	l.f = nil

	for i := maxFiles - 1; i >= 0; i-- {
		_ = os.Rename(l.slot(i), l.slot(i+1))
	}
	l.reopen()
	_ = os.Remove(l.slot(maxFiles))
}

func (l *Logger) reopen() {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	l.f = f
	l.size = 0
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
}
