package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEvent(event, slug string) HistoryEvent {
	return HistoryEvent{
		Timestamp: time.Now().UTC(),
		Event:     event,
		Slug:      slug,
		Provider:  "vllm",
	}
}

func TestAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	ev1 := makeEvent("switch_start", "model-a")
	ev2 := makeEvent("switch_ready", "model-a")
	ev3 := makeEvent("stop", "model-a")

	require.NoError(t, Append(path, ev1))
	require.NoError(t, Append(path, ev2))
	require.NoError(t, Append(path, ev3))

	events, err := Load(path, 100)
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "switch_start", events[0].Event)
	assert.Equal(t, "switch_ready", events[1].Event)
	assert.Equal(t, "stop", events[2].Event)
	assert.Equal(t, "model-a", events[0].Slug)
}

func TestLoadLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	for i := 0; i < 10; i++ {
		require.NoError(t, Append(path, makeEvent("stop", "model-a")))
	}

	events, err := Load(path, 3)
	require.NoError(t, err)
	assert.Len(t, events, 3)
}

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	// Write more than 10 bytes so threshold of 10 triggers rotation.
	content := []byte(`{"timestamp":"2026-01-01T00:00:00Z","event":"stop","slug":"x"}` + "\n")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	err := Rotate(path, 10) // threshold of 10 bytes → triggers rotation
	require.NoError(t, err)

	// Original file should be truncated.
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(0), fi.Size())

	// Compressed file should exist.
	_, err = os.Stat(path + ".1.gz")
	require.NoError(t, err)
}

func TestLoadMissing(t *testing.T) {
	events, err := Load("/nonexistent/path/history.jsonl", 20)
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestLoadFiltered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	now := time.Now().UTC()
	events := []HistoryEvent{
		{Timestamp: now, Event: "switch_start", Slug: "model-a", Provider: "vllm"},
		{Timestamp: now, Event: "switch_ready", Slug: "model-a", Provider: "vllm", ElapsedS: 14.2},
		{Timestamp: now, Event: "switch_start", Slug: "model-b", Provider: "nim"},
		{Timestamp: now, Event: "switch_ready", Slug: "model-b", Provider: "nim", ElapsedS: 20.1},
	}
	for _, ev := range events {
		require.NoError(t, Append(path, ev))
	}

	loaded, err := Load(path, 100)
	require.NoError(t, err)
	require.Len(t, loaded, 4)

	// Filter by slug manually (as the cmd layer would do).
	var filtered []HistoryEvent
	for _, ev := range loaded {
		if ev.Slug == "model-a" {
			filtered = append(filtered, ev)
		}
	}
	assert.Len(t, filtered, 2)
	assert.Equal(t, "switch_start", filtered[0].Event)
	assert.Equal(t, "switch_ready", filtered[1].Event)
}

func TestRotateNoOpWhenSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("small\n"), 0o644))

	require.NoError(t, Rotate(path, 50*1024*1024))

	// File should remain unchanged.
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Greater(t, fi.Size(), int64(0))

	_, err = os.Stat(path + ".1.gz")
	assert.True(t, os.IsNotExist(err))
}

func TestRotateMissingFile(t *testing.T) {
	// Should not error when file doesn't exist.
	require.NoError(t, Rotate("/nonexistent/path/history.jsonl", 100))
}

func TestAppendCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "history.jsonl")

	require.NoError(t, Append(path, makeEvent("stop", "model-a")))

	events, err := Load(path, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "stop", events[0].Event)
}

func TestAppendWithRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	// Write a line that's bigger than our tiny threshold (5 bytes).
	content := `{"timestamp":"2026-01-01T00:00:00Z","event":"stop","slug":"x"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	// Patch rotateThreshold by writing directly — simulate what Append does when
	// file exceeds threshold by calling Rotate explicitly with a small threshold first.
	require.NoError(t, Rotate(path, 5))

	// File truncated; compressed backup exists.
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(0), fi.Size())

	// Now append — should succeed even after rotation.
	require.NoError(t, Append(path, makeEvent("switch_start", "model-b")))
	events, err := Load(path, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "switch_start", events[0].Event)
}

func TestAppendPermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test permission failures as root")
	}
	dir := t.TempDir()
	// Make directory read-only so OpenFile fails.
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	path := filepath.Join(dir, "history.jsonl")
	err := Append(path, makeEvent("stop", "model-a"))
	assert.Error(t, err)
}

func TestLoadSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	// Mix of valid lines, invalid JSON, and blank lines.
	content := `{"timestamp":"2026-01-01T00:00:00Z","event":"stop","slug":"ok"}` + "\n" +
		"\n" + // blank line — must be skipped
		"not-valid-json\n" +
		`{"timestamp":"2026-01-02T00:00:00Z","event":"crash","slug":"also-ok"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	events, err := Load(path, 100)
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "stop", events[0].Event)
	assert.Equal(t, "crash", events[1].Event)
}
