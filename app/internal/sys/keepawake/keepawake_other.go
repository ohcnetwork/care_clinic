//go:build !windows

package keepawake

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

const settle = 500 * time.Millisecond

func Start() (*Hold, error) {
	cmd, err := command()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	var exitErr error
	go func() {
		exitErr = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil, helperExit(exitErr, stderr.String())
	case <-time.After(settle):
	}
	return &Hold{done: done, release: func() {
		_ = cmd.Process.Kill()
		<-done
	}}, nil
}

func helperExit(err error, stderr string) error {
	detail := strings.TrimSpace(stderr)
	if err == nil {
		err = errors.New("the sleep blocker exited straight away")
	}
	if detail != "" {
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
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
