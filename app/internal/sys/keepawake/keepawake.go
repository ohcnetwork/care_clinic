package keepawake

import "sync"

type Hold struct {
	done    chan struct{}
	release func()
	once    sync.Once
}

func (h *Hold) Alive() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

func (h *Hold) Release() { h.once.Do(h.release) }
