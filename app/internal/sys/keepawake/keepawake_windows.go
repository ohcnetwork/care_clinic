//go:build windows

package keepawake

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

func Start() (*Hold, error) {
	if err := setThreadExecutionState.Find(); err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		if r, _, err := setThreadExecutionState.Call(esContinuous | esSystemRequired); r == 0 {
			started <- err
			return
		}
		started <- nil
		<-stop
		_, _, _ = setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-started; err != nil {
		<-done
		return nil, err
	}
	return &Hold{done: done, release: func() {
		close(stop)
		<-done
	}}, nil
}
