package vm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuestLogsRouteAndReplayCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") != "previous" {
			t.Error("replay cursor missing")
		}
		_, _ = w.Write([]byte(`{"__CURSOR":"one","__REALTIME_TIMESTAMP":"123","MESSAGE":"docker ready","_SYSTEMD_UNIT":"docker.service","SECRET":"not exported"}
{"__CURSOR":"two","MESSAGE":"request handled","CONTAINER_NAME":"care-backend-1"}
`))
	}))
	defer server.Close()
	h := &host{g: &guest{http: &http.Client{Transport: rewriteTransport{server.URL}}}}
	root := t.TempDir()
	cursor := "previous"
	if err := h.streamLogs(context.Background(), root, &cursor); err == nil {
		t.Fatal("ended stream must report interruption")
	}
	if cursor != "two" {
		t.Fatalf("cursor: %s", cursor)
	}
	for _, name := range []string{"guest-system.log", "containers/care-backend-1.log"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || len(data) == 0 || strings.Contains(string(data), "SECRET") {
			t.Fatalf("invalid captured log %s: %s %v", name, data, err)
		}
	}
	if got := containerLogName("../../backend"); strings.ContainsAny(got, "./\\") {
		t.Fatalf("unsafe name %s", got)
	}
}

type rewriteTransport struct{ server string }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.URL.Scheme = "http"
	copy.URL.Host = strings.TrimPrefix(r.server, "http://")
	return http.DefaultTransport.RoundTrip(copy)
}
