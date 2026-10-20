//go:build !windows

package keepawake

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func Start() (*Hold, error) {
	cmd, err := command()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return &Hold{done: done, release: func() {
		_ = cmd.Process.Kill()
		<-done
	}}, nil
}

func command() (*exec.Cmd, error) {
	pid := strconv.Itoa(os.Getpid())
	switch runtime.GOOS {
	case "darwin":
		return proc.Command("/usr/bin/caffeinate", "-i", "-s", "-w", pid), nil
	case "linux":
		if _, err := exec.LookPath("systemd-inhibit"); err != nil {
			return nil, errors.New("systemd-inhibit is not available, so sleep can't be blocked on this computer")
		}
		return proc.Command("systemd-inhibit", "--what=idle:sleep", "--who=CARE Clinic",
			"--why=The clinic is running", "--mode=block", "tail", "--pid="+pid, "-f", "/dev/null"), nil
	}
	return nil, errors.New("keeping the computer awake is not supported on " + runtime.GOOS)
}
