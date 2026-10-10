package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

func (a *api) logs(w http.ResponseWriter, r *http.Request) {
	args := []string{"--follow", "--output=json", "--no-pager"}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 4096 || strings.ContainsAny(cursor, "\r\n") {
		http.Error(w, "invalid journal cursor", http.StatusBadRequest)
		return
	}
	if cursor == "" {
		args = append(args, "--boot=0", "--lines=all")
	} else {
		args = append(args, "--after-cursor="+cursor)
	}
	cmd := exec.CommandContext(r.Context(), "journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Pipe stderr too: process failures must reach the collector, not disappear.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, ok := w.(http.Flusher)
	if !ok {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return
	}
	flusher.Flush()
	errCh := make(chan string, 1)
	go func() {
		var text strings.Builder
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if text.Len() < 8192 {
				text.WriteString(scanner.Text() + "\n")
			}
		}
		errCh <- text.String()
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(w, scanner.Text()); err != nil {
			_ = cmd.Process.Kill()
			break
		}
		flusher.Flush()
	}
	if scanner.Err() != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	detail := <-errCh
	if r.Context().Err() == nil && (waitErr != nil || scanner.Err() != nil) {
		_ = json.NewEncoder(w).Encode(map[string]string{"collector_error": fmt.Sprintf("journal stream ended: %v; %v; %s", waitErr, scanner.Err(), detail)})
		flusher.Flush()
	}
}
