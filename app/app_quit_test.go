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

func TestOtherJobsNeverQuitAndExplainWhy(t *testing.T) {
	a := &App{}
	for _, label := range []string{"restore", "uninstall", "app-update", "update", "rebuild-all", ""} {
		if a.quitDuringJob(label) {
			t.Fatalf("quitting was allowed during %q", label)
		}
		if busyQuitMessage(label) == "" {
			t.Fatalf("no explanation for %q", label)
		}
	}
}
