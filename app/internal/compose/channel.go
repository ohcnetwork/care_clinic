package compose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ohcnetwork/care_desktop/app/internal/lines"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

// LockFile records which published CARE release each service runs.
const LockFile = "line.json"

// legacyLockFile was written by app versions that built CARE from source.
const legacyLockFile = "channel.lock"

const (
	Backend  = "backend"
	Frontend = "frontend"
)

// Release is one published CARE image, copied from the lines manifest so the
// clinic keeps working offline.
type Release = lines.Image

type Channel struct {
	Current  Release  `json:"current"`
	Next     *Release `json:"next,omitempty"`
	Declined string   `json:"declined,omitempty"`
}

type Lock struct {
	Line     string  `json:"line,omitempty"`
	NextLine string  `json:"next_line,omitempty"`
	Backend  Channel `json:"backend"`
	Frontend Channel `json:"frontend"`
}

func (l Lock) Get(service string) Channel {
	if service == Frontend {
		return l.Frontend
	}
	return l.Backend
}

func (l *Lock) Set(service string, c Channel) {
	if service == Frontend {
		l.Frontend = c
		return
	}
	l.Backend = c
}

var lockMu sync.Mutex

func readLock(dir string) Lock {
	var l Lock
	data, err := os.ReadFile(filepath.Join(dir, LockFile))
	if err != nil {
		return l
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return Lock{}
	}
	return l
}

func writeLock(dir string, l Lock) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, LockFile), append(data, '\n'), 0o644)
}

func ReadLock(dir string) Lock {
	lockMu.Lock()
	defer lockMu.Unlock()
	return readLock(dir)
}

func WriteLock(dir string, l Lock) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	return writeLock(dir, l)
}

func ModifyLock(dir string, fn func(*Lock) error) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	l := readLock(dir)
	if err := fn(&l); err != nil {
		return err
	}
	return writeLock(dir, l)
}

func (b *Builder) updateLock(fn func(*Lock)) error {
	return ModifyLock(b.dir, func(l *Lock) error {
		fn(l)
		return nil
	})
}

// removeLegacySource deletes the git checkouts and lock left by source builds.
func removeLegacySource(dir string) {
	_ = os.RemoveAll(filepath.Join(dir, "src"))
	_ = os.Remove(filepath.Join(dir, legacyLockFile))
}
