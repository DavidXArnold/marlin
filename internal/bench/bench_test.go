package bench

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeStream(tokens []string, err error) StreamFn {
	return func(_ context.Context, _, _ string, _ int, fn func(TokenEvent) error) error {
		for _, tok := range tokens {
			if callErr := fn(TokenEvent{Content: tok}); callErr != nil {
				return callErr
			}
		}
		return err
	}
}

// fakeStreamWithUsage mirrors fakeStream but also emits a final usage-only
// event (empty Content, PromptTokens set), matching how vllm.Client.ChatStream
// surfaces the stream_options.include_usage chunk.
func fakeStreamWithUsage(tokens []string, promptToks int, err error) StreamFn {
	return func(_ context.Context, _, _ string, _ int, fn func(TokenEvent) error) error {
		for _, tok := range tokens {
			if callErr := fn(TokenEvent{Content: tok}); callErr != nil {
				return callErr
			}
		}
		if callErr := fn(TokenEvent{PromptTokens: promptToks}); callErr != nil {
			return callErr
		}
		return err
	}
}

func TestRunBasic(t *testing.T) {
	now := time.Now()
	tick := 0
	NowFunc = func() time.Time {
		t := now.Add(time.Duration(tick) * 10 * time.Millisecond)
		tick++
		return t
	}
	defer func() { NowFunc = time.Now }()

	r, err := Run(context.Background(), fakeStream([]string{"Hello", " world", "!"}, nil), "m", "p", 64)
	require.NoError(t, err)
	assert.Equal(t, 3, r.OutputToks)
	assert.Greater(t, r.TTFT, time.Duration(0))
	assert.Greater(t, r.TotalTime, r.TTFT)
	assert.Greater(t, r.DecodeToksPerSec, 0.0)
}

func TestRunStreamError(t *testing.T) {
	stream := fakeStream(nil, fmt.Errorf("connection reset"))
	_, err := Run(context.Background(), stream, "m", "p", 64)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
}

func TestRunSingleToken(t *testing.T) {
	r, err := Run(context.Background(), fakeStream([]string{"hi"}, nil), "m", "p", 64)
	require.NoError(t, err)
	assert.Equal(t, 1, r.OutputToks)
	// Single token → no decode window → DecodeToksPerSec == 0
	assert.Equal(t, 0.0, r.DecodeToksPerSec)
}

func TestRunNoTokens(t *testing.T) {
	r, err := Run(context.Background(), fakeStream(nil, nil), "m", "p", 64)
	require.NoError(t, err)
	assert.Equal(t, 0, r.OutputToks)
	assert.Equal(t, time.Duration(0), r.TTFT)
}

func TestRunWithUsageComputesPrefillThroughput(t *testing.T) {
	now := time.Now()
	tick := 0
	NowFunc = func() time.Time {
		t := now.Add(time.Duration(tick) * 10 * time.Millisecond)
		tick++
		return t
	}
	defer func() { NowFunc = time.Now }()

	r, err := Run(context.Background(), fakeStreamWithUsage([]string{"Hello", " world", "!"}, 40, nil), "m", "p", 64)
	require.NoError(t, err)
	assert.Equal(t, 3, r.OutputToks)
	assert.Equal(t, 40, r.PromptToks)
	assert.Greater(t, r.PrefillToksPerSec, 0.0)
	assert.InDelta(t, float64(r.PromptToks)/r.TTFT.Seconds(), r.PrefillToksPerSec, 0.001)
}

func TestRunNoUsageLeavesPrefillThroughputZero(t *testing.T) {
	r, err := Run(context.Background(), fakeStream([]string{"hi"}, nil), "m", "p", 64)
	require.NoError(t, err)
	assert.Equal(t, 0, r.PromptToks)
	assert.Equal(t, 0.0, r.PrefillToksPerSec)
}

func TestSummariseEmpty(t *testing.T) {
	s := Summarise(nil)
	assert.Equal(t, 0, s.Runs)
}

func TestSummariseMultiple(t *testing.T) {
	results := []*Result{
		{TTFT: 100 * time.Millisecond, DecodeToksPerSec: 50, PrefillToksPerSec: 200, OutputToks: 10, PromptToks: 20},
		{TTFT: 200 * time.Millisecond, DecodeToksPerSec: 40, PrefillToksPerSec: 300, OutputToks: 20, PromptToks: 30},
		{TTFT: 150 * time.Millisecond, DecodeToksPerSec: 60, PrefillToksPerSec: 400, OutputToks: 15, PromptToks: 40},
	}
	s := Summarise(results)
	assert.Equal(t, 3, s.Runs)
	assert.Equal(t, 150*time.Millisecond, s.AvgTTFT)
	assert.Equal(t, 100*time.Millisecond, s.MinTTFT)
	assert.Equal(t, 200*time.Millisecond, s.MaxTTFT)
	assert.InDelta(t, 50.0, s.AvgDecodeToksPerSec, 0.001)
	assert.InDelta(t, 300.0, s.AvgPrefillToksPerSec, 0.001)
	assert.Equal(t, 45, s.TotalOutputToks)
	assert.Equal(t, 90, s.TotalPromptToks)
}

func TestPrint(t *testing.T) {
	s := &Stats{
		Runs:                 2,
		AvgTTFT:              120 * time.Millisecond,
		MinTTFT:              100 * time.Millisecond,
		MaxTTFT:              140 * time.Millisecond,
		AvgDecodeToksPerSec:  55.3,
		AvgPrefillToksPerSec: 312.5,
		TotalOutputToks:      30,
		TotalPromptToks:      50,
	}
	var buf strings.Builder
	Print(&buf, s, "test-model")
	out := buf.String()
	assert.Contains(t, out, "test-model")
	assert.Contains(t, out, "55.3")
	assert.Contains(t, out, "312.5")
	assert.Contains(t, out, "30")
	assert.Contains(t, out, "50")
}
