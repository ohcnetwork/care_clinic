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
		output := strings.Repeat("child output after launcher exit\n", 64*1024)
		if _, err := fmt.Fprint(os.Stdout, output); err != nil {
			os.Exit(93)
		}
		if _, err := fmt.Fprint(os.Stderr, output); err != nil {
			os.Exit(94)
		}
		if err := os.WriteFile("child-output.done", []byte("both output handles remain writable"), 0o600); err != nil {
			os.Exit(95)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if mode == "parent" {
		r := Runner{Env: append(os.Environ(), "CARE_LAUNCHER_HELPER=descendant-success")}
		if err := r.RunLauncher(10*time.Second, os.Args[0], "-test.run=^TestLauncherHelper$"); err != nil {
			os.Exit(96)
		}
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
	for _, mode := range []string{"success", "failure", "descendant-success", "descendant-failure", "parent", "hang", "cancelled"} {
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
			err := r.RunLauncher(timeout, exe, "-test.run=^TestLauncherHelper$")
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
				if strings.Contains(output, "launcher stdout") || strings.Contains(output, "launcher stderr") {
					t.Fatalf("launcher output was not discarded: %s", output)
				}
				if mode == "descendant-success" || mode == "parent" {
					deadline := time.Now().Add(5 * time.Second)
					for {
						data, err := os.ReadFile(filepath.Join(dir, "child-output.done"))
						if err != nil && !os.IsNotExist(err) {
							t.Fatal(err)
						}
						if string(data) == "both output handles remain writable" {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("child lost its output handles after the launcher or its parent exited")
						}
						time.Sleep(20 * time.Millisecond)
					}
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "child.pid" && entry.Name() != "child-output.done" {
					t.Errorf("unexpected output file: %s", entry.Name())
				}
			}
		})
	}
	t.Run("missing executable", func(t *testing.T) {
		dir := t.TempDir()
		if err := (Runner{}).RunLauncher(time.Second, filepath.Join(dir, "missing")); err == nil {
			t.Fatal("missing launcher reported success")
		}
	})
}
