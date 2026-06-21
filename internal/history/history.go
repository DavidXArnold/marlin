// Package history manages the append-only JSONL event log for marlin.
package history

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// HistoryEvent is one record written to the JSONL history file.
type HistoryEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Event       string    `json:"event"`                  // "switch_start", "switch_ready", "stop", "crash"
	Slug        string    `json:"slug,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	FromSlug    string    `json:"from_slug,omitempty"`
	ElapsedS    float64   `json:"elapsed_s,omitempty"`
	DurationS   float64   `json:"active_duration_s,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	ExitCode    int       `json:"exit_code,omitempty"`
	LastLogLine string    `json:"last_log_line,omitempty"`
}

const rotateThreshold int64 = 50 * 1024 * 1024 // 50 MiB

// Append writes ev as a JSON line to the JSONL file at path, creating the
// file and any parent directories if they do not exist. It also rotates the
// file when it exceeds 50 MiB.
func Append(path string, ev HistoryEvent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Rotate before appending so we don't keep growing a single large file.
	if err := Rotate(path, rotateThreshold); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// Load reads the last limit lines from the JSONL file at path. If the file
// does not exist an empty slice is returned without error. Lines that cannot
// be decoded are silently skipped.
func Load(path string, limit int) ([]HistoryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Take the tail.
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	events := make([]HistoryEvent, 0, len(lines))
	for _, l := range lines {
		var ev HistoryEvent
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, ev)
	}
	return events, nil
}

// Rotate compresses path to path+".1.gz" and truncates the original when the
// file exceeds maxBytes. Does nothing if the file is smaller or does not exist.
func Rotate(path string, maxBytes int64) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Size() <= maxBytes {
		return nil
	}

	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(path + ".1.gz")
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(dst)
	if _, err := io.Copy(gz, src); err != nil {
		_ = gz.Close()
		_ = dst.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}

	// Truncate the original.
	return os.Truncate(path, 0)
}
