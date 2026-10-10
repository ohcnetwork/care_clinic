package main

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
	"github.com/ohcnetwork/care_desktop/app/internal/vm"
)

// useEngine points every docker command this process starts at the CARE
// engine. On Windows the engine cannot see this computer's folders, so docker
// commands that mount them first bring the engine's copy up to date, and
// bring back whatever the containers wrote once they finish.
func useEngine() {
	vm.ApplyEnv()
	if runtime.GOOS != "windows" {
		return
	}
	proc.DockerHook = func(args []string) ([]string, func()) {
		if !vm.MountSensitive(args) {
			return args, func() {}
		}
		out, paths := vm.RewriteDockerArgs(args)
		sync := func(paths []string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := vm.Sync(ctx, paths); err != nil && appLog != nil {
				appLog.Writef("engine sync: %s", err)
			}
		}
		sync(paths)
		return out, func() { sync(nil) }
	}
}

// stopEngine runs at the end of an uninstall. With the images removed there is
// nothing left worth keeping in the engine, so its disk goes too.
func (a *App) stopEngine(removeImages bool) {
	if !vm.Supported() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := vm.Stop(ctx); err != nil {
		a.logln("Could not stop the CARE engine: " + err.Error())
		return
	}
	if !removeImages {
		return
	}
	if err := os.RemoveAll(vm.StateDir()); err != nil {
		a.logln("CARE was removed, but the CARE engine's files could not be deleted: " + err.Error())
		return
	}
	a.logln("The CARE engine and its disk were removed.")
}
