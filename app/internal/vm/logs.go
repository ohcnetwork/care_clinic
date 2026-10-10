package vm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/applog"
)

// The VM writes serial output to a pipe rather than a disk descriptor; the
// reader rotates the host file and keeps draining even when the disk fails.
func consoleOutput() (*os.File, error) {
	stream, err := applog.NewStream(ConsoleLog())
	if err != nil {
		log.Printf("console logging unavailable; VM will continue without serial capture: %v", err)
		return os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	go func() {
		defer func() { _ = r.Close() }()
		queue := make(chan []byte, 128)
		go func() {
			lastError := time.Time{}
			for chunk := range queue {
				if _, err := stream.Write(chunk); err != nil && time.Since(lastError) > time.Minute {
					log.Printf("console log lost messages: %v", err)
					lastError = time.Now()
				}
			}
		}()
		defer close(queue)
		buf := make([]byte, 32<<10)
		dropped := 0
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				if dropped > 0 {
					chunk = append([]byte(fmt.Sprintf("\n[collector] console log gap: %d chunks dropped while disk writer queue was full\n", dropped)), chunk...)
				}
				select {
				case queue <- chunk:
					dropped = 0
				default:
					dropped++
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("console log pipe: %v", err)
				}
				return
			}
		}
	}()
	return w, nil
}

type journalRecord struct {
	Cursor    string          `json:"__CURSOR"`
	Timestamp string          `json:"__REALTIME_TIMESTAMP"`
	Message   json.RawMessage `json:"MESSAGE"`
	Container string          `json:"CONTAINER_NAME"`
	Unit      string          `json:"_SYSTEMD_UNIT"`
	Error     string          `json:"collector_error"`
}

var errJournalCursor = errors.New("guest journal replay failed")

func containerLogName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			b.WriteRune(c)
		}
		if b.Len() >= 120 {
			break
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func (h *host) collectLogs(ctx context.Context) {
	root := applog.DefaultLogDir()
	collector, err := applog.NewStream(filepath.Join(root, "collector.log"))
	if err != nil {
		h.logf("log collector unavailable: %v", err)
		return
	}
	report := func(format string, args ...any) {
		text := time.Now().UTC().Format(time.RFC3339Nano) + " " + fmt.Sprintf(format, args...) + "\n"
		if _, err := collector.Write([]byte(text)); err != nil {
			h.logf("log collector lost diagnostics: %v", err)
		}
	}
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			if err := applog.Maintain(root, time.Now()); err != nil {
				report("log retention failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	cursor := ""
	for ctx.Err() == nil {
		report("connecting guest journal; logs may contain sensitive information")
		err := h.streamLogs(ctx, root, &cursor)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			report("renewing guest log stream from last saved cursor")
			continue
		}
		h.setLogs(LogsInterrupted)
		report("guest logging interrupted; a gap may exist: %v", err)
		// Invalid/vacuumed cursors cannot be replayed. Explicitly record the gap
		// before resuming from the guest's recent history.
		if errors.Is(err, errJournalCursor) {
			cursor = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (h *host) streamLogs(ctx context.Context, root string, cursor *string) error {
	// Renew periodically so a hung guest cannot leave collection silently stuck.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/v1/logs?cursor="+url.QueryEscape(*cursor), nil)
	if err != nil {
		return err
	}
	resp, err := h.g.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("guest log stream: %s", resp.Status)
	}
	h.setLogs(LogsCollecting)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	for scanner.Scan() {
		var record journalRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode guest log: %w", err)
		}
		if record.Error != "" {
			return fmt.Errorf("%w: %s", errJournalCursor, record.Error)
		}
		name := filepath.Join(root, "guest-system.log")
		if record.Container != "" {
			name = filepath.Join(root, "containers", containerLogName(record.Container)+".log")
		}
		stream, err := applog.NewStream(name)
		if err != nil {
			return err
		}
		// Preserve JSON message values (including binary arrays); do not export
		// the full journal record, which can contain process environment fields.
		line, err := json.Marshal(map[string]any{"timestamp_us": record.Timestamp, "unit": record.Unit, "message": record.Message})
		if err != nil {
			return err
		}
		if _, err := stream.Write(append(line, '\n')); err != nil {
			return err
		}
		*cursor = record.Cursor
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}
