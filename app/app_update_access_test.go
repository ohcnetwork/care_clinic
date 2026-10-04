package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClinicAppUpdateDoesNotRequireServerSetup(t *testing.T) {
	for _, role := range []string{"", roleClient, roleServer} {
		t.Run("role="+role, func(t *testing.T) {
			a := roleApp(t)
			a.cfg = Config{Role: role}
			a.pins.AppVersion = "1.2.3"
			before := a.loadConfig()
			var requests atomic.Int32
			original := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != releasesAPI {
					return nil, fmt.Errorf("unexpected update request: %s", r.URL)
				}
				requests.Add(1)
				// No newer version: exercise the asynchronous failure path without replacing the app.
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.2.3","assets":[
						{"name":"CARE.dmg","browser_download_url":"https://example.invalid/CARE.dmg"},
						{"name":"CARE-setup.exe","browser_download_url":"https://example.invalid/CARE-setup.exe"}
					]}`)),
					Header: make(http.Header),
				}, nil
			})}
			t.Cleanup(func() { http.DefaultClient = original })

			if err := a.InstallAppUpdate(); err != nil {
				t.Fatalf("update rejected before choosing/installing a server: %v", err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for !a.jobMu.TryLock() {
				if time.Now().After(deadline) {
					t.Fatal("update job did not finish and release its lock")
				}
				time.Sleep(time.Millisecond)
			}
			a.jobMu.Unlock()
			if requests.Load() != 1 {
				t.Fatal("update did not reach the release check")
			}
			if a.loadConfig() != before || a.activeJob.Load() != "" {
				t.Fatal("desktop update changed clinic setup or left a busy job")
			}
		})
	}
}

func TestClinicAppUpdateStillHonorsJobAndClosingGuards(t *testing.T) {
	a := roleApp(t)
	a.jobMu.Lock()
	err := a.InstallAppUpdate()
	a.jobMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("update bypassed active-job guard: %v", err)
	}
	a.closing = true
	if err := a.InstallAppUpdate(); err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("update bypassed closing guard: %v", err)
	}
}
