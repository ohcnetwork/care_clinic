package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/release"
)

func TestRunningJobIsTrackedForTheQuitPrompt(t *testing.T) {
	a := &App{
		configFile: filepath.Join(t.TempDir(), "config.json"),
		cfg:        Config{Role: roleServer},
		pins:       &release.Pins{},
	}
	done := make(chan struct{})
	if err := a.run(func() error { <-done; return nil }, false, "setup"); err != nil {
		t.Fatal(err)
	}
	if got := a.runningJob(); got != "setup" {
		t.Fatalf("running job = %q, want setup", got)
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("setup was quit without asking")
	}
	close(done)
	deadline := time.Now().Add(5 * time.Second)
	for a.runningJob() != "" {
		if time.Now().After(deadline) {
			t.Fatal("the finished job was never cleared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEveryJobOffersQuitWithItsOwnWording(t *testing.T) {
	seen := map[string]string{}
	for _, label := range []string{"setup", jobPrereq, "start", "stop", "restore", "uninstall", "app-update", "update", "backup-now", "rebuild-all", ""} {
		p := jobQuitPrompt(label)
		if p.doing == "" || p.title == "" || p.message == "" {
			t.Fatalf("no quit prompt for %q: %+v", label, p)
		}
		if other, ok := seen[p.message]; ok {
			t.Fatalf("%q reuses the quit wording of %q", label, other)
		}
		seen[p.message] = label
	}
}

func TestConfirmedQuitClosesDuringAJob(t *testing.T) {
	a := &App{
		configFile: filepath.Join(t.TempDir(), "config.json"),
		cfg:        Config{Role: roleServer},
		pins:       &release.Pins{},
	}
	done := make(chan struct{})
	defer close(done)
	if err := a.run(func() error { <-done; return nil }, false, "start"); err != nil {
		t.Fatal(err)
	}
	if !a.beforeClose(context.Background()) {
		t.Fatal("closed during a job without asking")
	}
	a.quitConfirmed.Store(true)
	if a.beforeClose(context.Background()) {
		t.Fatal("a confirmed quit was still prevented by the running job")
	}
}

func TestPrereqInstallCanBeQuitDuringSetup(t *testing.T) {
	a := &App{
		configFile: filepath.Join(t.TempDir(), "config.json"),
		cfg:        Config{Role: roleServer},
		pins:       &release.Pins{},
	}
	started, unblock := make(chan struct{}), make(chan struct{})
	go func() {
		_ = a.withLabeledJob(jobPrereq, func() error { close(started); <-unblock; return nil })
	}()
	<-started
	if got := a.runningJob(); got != jobPrereq {
		t.Fatalf("running job = %q, want %q", got, jobPrereq)
	}
	if jobQuitPrompt(jobPrereq).message == jobQuitPrompt("setup").message {
		t.Fatal("installing a requirement reuses the setup quit wording")
	}
	close(unblock)
	deadline := time.Now().Add(5 * time.Second)
	for a.runningJob() != "" {
		if time.Now().After(deadline) {
			t.Fatal("the finished job was never cleared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
