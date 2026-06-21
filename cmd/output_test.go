package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/history"
)

// setOutputFmt sets the global outputFormat for the duration of the test.
func setOutputFmt(t *testing.T, fmt string) {
	t.Helper()
	old := outputFormat
	outputFormat = fmt
	t.Cleanup(func() { outputFormat = old })
}

// --- list ---

func TestListJSON(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b", "llama-8b")
	defer cleanup()
	setOutputFmt(t, "json")

	var buf bytes.Buffer
	require.NoError(t, runList(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, `"slug"`)
	assert.Contains(t, out, `"qwen25-72b"`)
	assert.Contains(t, out, `"llama-8b"`)
}

func TestListJSONL(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	setOutputFmt(t, "jsonl")

	var buf bytes.Buffer
	require.NoError(t, runList(cmdWithContext(&buf), nil))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 1)
	assert.Contains(t, lines[0], `"slug"`)
}

func TestListPlain(t *testing.T) {
	cleanup := tempEnv(t, "qwen25-72b")
	defer cleanup()
	setOutputFmt(t, "plain")

	var buf bytes.Buffer
	require.NoError(t, runList(cmdWithContext(&buf), nil))
	out := buf.String()
	assert.Contains(t, out, "qwen25-72b")
	assert.Contains(t, out, "\t") // tab-separated
	assert.NotContains(t, out, "SLUG") // no header
}

// --- history ---

func TestHistoryCmdJSON(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()
	setOutputFmt(t, "json")

	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: time.Now(), Event: "switch_start", Slug: "qwen3-32b"},
	})

	cmd := cmdWithContext(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, runHistory(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, `"event"`)
	assert.Contains(t, out, `"switch_start"`)
}

func TestHistoryCmdPlain(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()
	setOutputFmt(t, "plain")

	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: time.Now(), Event: "stop", Slug: "llama-8b", DurationS: 3600},
	})

	cmd := cmdWithContext(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, runHistory(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "stop")
	assert.Contains(t, out, "llama-8b")
	assert.Contains(t, out, "\t")
}

func TestHistoryCmdJSONEmpty(t *testing.T) {
	_, cleanup := historyEnv(t)
	defer cleanup()
	setOutputFmt(t, "json")

	cmd := cmdWithContext(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, runHistory(cmd, nil))

	assert.Contains(t, buf.String(), "[]")
}

func TestHistoryCmdJSONL(t *testing.T) {
	histPath, cleanup := historyEnv(t)
	defer cleanup()
	setOutputFmt(t, "jsonl")

	writeHistoryEvents(t, histPath, []history.HistoryEvent{
		{Timestamp: time.Now(), Event: "switch_ready", Slug: "m1", ElapsedS: 12.3},
		{Timestamp: time.Now(), Event: "stop", Slug: "m1", DurationS: 7200},
	})

	cmd := cmdWithContext(nil)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	cmd.Flags().Int("last", 20, "")
	cmd.Flags().String("slug", "", "")
	cmd.Flags().String("since", "", "")
	require.NoError(t, runHistory(cmd, nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 2)
}

// --- doctor ---

func TestDoctorJSON(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	setOutputFmt(t, "json")

	injectDoctorRunCmd(t, func(_ context.Context, name string, _ ...string) ([]byte, error) {
		return nil, &stubCmdNotFound{name: name}
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("fix", false, "")
	cmd.Flags().Bool("yes", false, "")
	require.NoError(t, runDoctor(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, `"results"`)
	assert.Contains(t, out, `"pass"`)
	assert.Contains(t, out, `"level"`)
}

func TestDoctorPlain(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	setOutputFmt(t, "plain")

	injectDoctorRunCmd(t, func(_ context.Context, name string, _ ...string) ([]byte, error) {
		return nil, &stubCmdNotFound{name: name}
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("fix", false, "")
	cmd.Flags().Bool("yes", false, "")
	require.NoError(t, runDoctor(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "\t") // tab-separated
	// At minimum one result line with level\tid\tdetail
	assert.True(t, strings.Contains(out, "PASS") || strings.Contains(out, "WARN") || strings.Contains(out, "FAIL"))
}

func TestDoctorJSONL(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	setOutputFmt(t, "jsonl")

	injectDoctorRunCmd(t, func(_ context.Context, name string, _ ...string) ([]byte, error) {
		return nil, &stubCmdNotFound{name: name}
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("fix", false, "")
	cmd.Flags().Bool("yes", false, "")
	require.NoError(t, runDoctor(cmd, nil))

	// Each line is a separate JSON object.
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		assert.True(t, strings.HasPrefix(line, "{"), "expected JSON object, got: %s", line)
	}
}

// --- ps ---

func TestPsJSON(t *testing.T) {
	stub := &stubAdhoc{
		listResult: []provider.AdhocInfo{
			{Slug: "llama-8b", Provider: "nim", Status: "running", Port: "8000", ID: "abc123456789"},
		},
	}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()
	setOutputFmt(t, "json")

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runPs(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, `"model"`)
	assert.Contains(t, out, `"llama-8b"`)
}

func TestPsJSONL(t *testing.T) {
	stub := &stubAdhoc{
		listResult: []provider.AdhocInfo{
			{Slug: "m1", Provider: "nim", Status: "running", Port: "8000", ID: "id1"},
			{Slug: "m2", Provider: "vllm", Status: "stopped", Port: "8001", ID: "id2"},
		},
	}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()
	setOutputFmt(t, "jsonl")

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runPs(cmd, nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 2)
}

func TestPsPlain(t *testing.T) {
	stub := &stubAdhoc{
		listResult: []provider.AdhocInfo{
			{Slug: "llama-8b", Provider: "nim", Status: "running", Port: "8000", ID: "abc123456789"},
		},
	}
	injectAdhocRunner(t, stub)
	_, cleanup := switchEnv(t)
	defer cleanup()
	setOutputFmt(t, "plain")

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	require.NoError(t, runPs(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "llama-8b")
	assert.Contains(t, out, "\t")
	assert.NotContains(t, out, "MODEL") // no header
}
