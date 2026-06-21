package cmd

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/history"
)

func historyEnv(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()

	modelsDir := dir + "/models"
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))
	historyPath := dir + "/history.jsonl"

	cfgContent := fmt.Sprintf(`[behavior]
switch_prompt = false

[paths]
models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q
history_file = %q

[server]
alias = "gn100"
`, modelsDir,
		dir+"/state.toml",
		dir+"/secrets.env",
		dir+"/model.env",
		historyPath,
	)
	cfgPath := dir + "/config.toml"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))

	old := cfgFile
	cfgFile = cfgPath
	return historyPath, func() { cfgFile = old }
}

func writeHistoryEvents(t *testing.T, histPath string, events []history.HistoryEvent) {
	t.Helper()
	for _, ev := range events {
		require.NoError(t, history.Append(histPath, ev))
	}
}

func TestHistoryCmdEmpty(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()
	_ = histPath

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, runHistory(cmd, nil))
	assert.Contains(t, buf.String(), "no history")
}

func TestHistoryCmdTable(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()

	now := time.Now().UTC()
	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: now, Event: "switch_start", Slug: "qwen25-72b", Provider: "vllm"},
		{Timestamp: now.Add(14 * time.Second), Event: "switch_ready", Slug: "qwen25-72b", Provider: "vllm", ElapsedS: 14.2},
		{Timestamp: now.Add(4317 * time.Second), Event: "stop", Slug: "qwen25-72b", Provider: "vllm", DurationS: 4317},
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, runHistory(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "switch_start")
	assert.Contains(t, out, "switch_ready")
	assert.Contains(t, out, "stop")
	assert.Contains(t, out, "qwen25-72b")
	assert.Contains(t, out, "14.2s")
}

func TestHistoryCmdStats(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()

	now := time.Now().UTC()
	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: now, Event: "switch_start", Slug: "model-a", Provider: "vllm"},
		{Timestamp: now.Add(15 * time.Second), Event: "switch_ready", Slug: "model-a", Provider: "vllm", ElapsedS: 15.0},
		{Timestamp: now.Add(3600 * time.Second), Event: "stop", Slug: "model-a", Provider: "vllm", DurationS: 3600},
		{Timestamp: now.Add(3700 * time.Second), Event: "switch_start", Slug: "model-b", Provider: "nim"},
		{Timestamp: now.Add(3720 * time.Second), Event: "switch_ready", Slug: "model-b", Provider: "nim", ElapsedS: 20.0},
		{Timestamp: now.Add(7200 * time.Second), Event: "crash", Slug: "model-b", Provider: "nim"},
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runHistoryStats(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "total switches : 2")
	assert.Contains(t, out, "unique models  : 2")
	assert.Contains(t, out, "avg ready time")
	assert.Contains(t, out, "crashes        : 1")
}

func TestHistoryCmdSlugFilter(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()

	now := time.Now().UTC()
	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: now, Event: "switch_start", Slug: "model-a", Provider: "vllm"},
		{Timestamp: now.Add(10 * time.Second), Event: "switch_ready", Slug: "model-a", Provider: "vllm", ElapsedS: 10},
		{Timestamp: now.Add(20 * time.Second), Event: "switch_start", Slug: "model-b", Provider: "nim"},
		{Timestamp: now.Add(30 * time.Second), Event: "switch_ready", Slug: "model-b", Provider: "nim", ElapsedS: 10},
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "model-a", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, cmd.Flags().Set("slug", "model-a"))
	require.NoError(t, runHistory(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "model-a")
	assert.NotContains(t, out, "model-b")
}

func TestHistoryCmdSince(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()

	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-1 * time.Hour)

	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: old, Event: "stop", Slug: "old-model", Provider: "vllm"},
		{Timestamp: recent, Event: "stop", Slug: "new-model", Provider: "vllm"},
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "7d", "")
	require.NoError(t, cmd.Flags().Set("since", "7d"))
	require.NoError(t, runHistory(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "new-model")
	assert.NotContains(t, out, "old-model")
}

func TestAppendHistoryInjectable(t *testing.T) {
	var called bool
	old := appendHistory
	appendHistory = func(path string, ev history.HistoryEvent) error {
		called = true
		return nil
	}
	defer func() { appendHistory = old }()

	_ = appendHistory("", history.HistoryEvent{})
	assert.True(t, called)
}

func TestHistoryFilePath(t *testing.T) {
	_, cleanup := historyEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Paths.HistoryFile)
	assert.Contains(t, cfg.Paths.HistoryFile, "history.jsonl")
}

func TestFormatSeconds(t *testing.T) {
	cases := []struct {
		s    float64
		want string
	}{
		{30, "30s"},
		{90, "1m"},
		{3600, "1h"},
		{3660, "1h 1m"},
		{7384, "2h 3m"},
	}
	for _, tc := range cases {
		got := formatSeconds(tc.s)
		assert.Equal(t, tc.want, got, "formatSeconds(%v)", tc.s)
	}
}

func TestAverageEmpty(t *testing.T) {
	assert.Equal(t, 0.0, average(nil))
	assert.Equal(t, 0.0, average([]float64{}))
}

func TestParseDurationArg(t *testing.T) {
	d, err := parseDurationArg("7d")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, d)

	d, err = parseDurationArg("24h")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, d)

	_, err = parseDurationArg("bad")
	assert.Error(t, err)
}
