package proc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLauncherHelper(t *testing.T) {
	mode := os.Getenv("CARE_LAUNCHER_HELPER")
	if mode == "" {
		return
	}
	if mode == "child" {
		time.Sleep(2 * time.Second)
		if _, err := fmt.Fprintln(os.Stdout, "child output after launcher exit"); err != nil {
			os.Exit(93)
		}
		if _, err := fmt.Fprintln(os.Stderr, "child stderr after launcher exit"); err != nil {
			os.Exit(94)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if mode == "hang" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	dir, err := os.Getwd()
	if err != nil || dir != os.Getenv("CARE_LAUNCHER_DIR") {
		os.Exit(90)
	}
	fmt.Println("launcher stdout")
	fmt.Fprintln(os.Stderr, "launcher stderr")
	if strings.HasPrefix(mode, "descendant") {
		child := exec.Command(os.Args[0], "-test.run=^TestLauncherHelper$")
		child.Env = append(os.Environ(), "CARE_LAUNCHER_HELPER=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(91)
		}
		if err := os.WriteFile("child.pid", []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			_ = child.Process.Kill()
			os.Exit(92)
		}
	}
	if strings.HasSuffix(mode, "failure") {
		os.Exit(7)
	}
	os.Exit(0)
}

func TestRunLauncher(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "failure", "descendant-success", "descendant-failure", "hang", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Cleanup(func() {
				data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
				if os.IsNotExist(err) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				pid, err := strconv.Atoi(string(data))
				if err != nil {
					t.Error(err)
					return
				}
				child, err := os.FindProcess(pid)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() {
					if err := child.Release(); err != nil {
						t.Error(err)
					}
				}()
				if err := child.Kill(); err != nil {
					t.Error(err)
				}
			})
			var logs []string
			r := Runner{
				Dir: dir,
				Env: append(os.Environ(), "CARE_LAUNCHER_HELPER="+mode, "CARE_LAUNCHER_DIR="+dir),
				Log: func(line string) { logs = append(logs, line) },
			}
			timeout := 10 * time.Second
			if mode == "hang" {
				timeout = 100 * time.Millisecond
			}
			if mode == "cancelled" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				r.Ctx = ctx
			}
			start := time.Now()
			logPath := filepath.Join(dir, "launcher.log")
			err := r.RunLauncher(timeout, logPath, exe, "-test.run=^TestLauncherHelper$")
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("launcher waited for its descendant or ignored timeout: %s", elapsed)
			}
			switch {
			case mode == "hang":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected deadline error, got %v", err)
				}
			case mode == "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation, got %v", err)
				}
			case strings.HasSuffix(mode, "failure"):
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 7 {
					t.Fatalf("lost launcher failure: %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode != "hang" && mode != "cancelled" {
				output := strings.Join(logs, "\n")
				if !strings.Contains(output, "launcher stdout") || !strings.Contains(output, "launcher stderr") {
					t.Fatalf("missing launcher output: %s", output)
				}
				if mode == "descendant-success" {
					deadline := time.Now().Add(5 * time.Second)
					for {
						data, err := os.ReadFile(logPath)
						if err != nil {
							t.Fatal(err)
						}
						if strings.Contains(string(data), "child output after launcher exit") &&
							strings.Contains(string(data), "child stderr after launcher exit") {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("child lost its output handles after the launcher returned")
						}
						time.Sleep(20 * time.Millisecond)
					}
				}
			}
		})
	}
	t.Run("missing executable", func(t *testing.T) {
		dir := t.TempDir()
		if err := (Runner{}).RunLauncher(time.Second, filepath.Join(dir, "launcher.log"), filepath.Join(dir, "missing")); err == nil {
			t.Fatal("missing launcher reported success")
		}
	})
	t.Run("unwritable log", func(t *testing.T) {
		if err := (Runner{}).RunLauncher(time.Second, t.TempDir(), exe, "-test.run=^TestLauncherHelper$"); err == nil {
			t.Fatal("unwritable launcher log reported success")
		}
	})
}
