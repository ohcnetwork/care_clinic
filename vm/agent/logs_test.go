package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalStreamAndProcessErrors(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '{\"__CURSOR\":\"test\",\"MESSAGE\":\"ready\"}'\nprintf '%s\\n' 'journal failed' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	server := httptest.NewServer(newAPI("missing").routes())
	defer server.Close()
	resp, err := http.Get(server.URL + "/v1/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"__CURSOR":"test"`) ||
		!strings.Contains(string(body), "collector_error") || !strings.Contains(string(body), "journal failed") {
		t.Fatalf("missing streamed logs/error: %s %v", body, err)
	}
}

func TestJournalRejectsInvalidCursor(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/logs?cursor=%0A", nil)
	newAPI("missing").routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor accepted: %d", recorder.Code)
	}
}
