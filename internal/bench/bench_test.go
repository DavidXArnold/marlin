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

func TestSummariseEmpty(t *testing.T) {
	s := Summarise(nil)
	assert.Equal(t, 0, s.Runs)
}

func TestSummariseMultiple(t *testing.T) {
	results := []*Result{
		{TTFT: 100 * time.Millisecond, DecodeToksPerSec: 50, OutputToks: 10},
		{TTFT: 200 * time.Millisecond, DecodeToksPerSec: 40, OutputToks: 20},
		{TTFT: 150 * time.Millisecond, DecodeToksPerSec: 60, OutputToks: 15},
	}
	s := Summarise(results)
	assert.Equal(t, 3, s.Runs)
	assert.Equal(t, 150*time.Millisecond, s.AvgTTFT)
	assert.Equal(t, 100*time.Millisecond, s.MinTTFT)
	assert.Equal(t, 200*time.Millisecond, s.MaxTTFT)
	assert.InDelta(t, 50.0, s.AvgDecodeToksPerSec, 0.001)
	assert.Equal(t, 45, s.TotalOutputToks)
}

func TestPrint(t *testing.T) {
	s := &Stats{
		Runs:                2,
		AvgTTFT:             120 * time.Millisecond,
		MinTTFT:             100 * time.Millisecond,
		MaxTTFT:             140 * time.Millisecond,
		AvgDecodeToksPerSec: 55.3,
		TotalOutputToks:     30,
	}
	var buf strings.Builder
	Print(&buf, s, "test-model")
	out := buf.String()
	assert.Contains(t, out, "test-model")
	assert.Contains(t, out, "55.3")
	assert.Contains(t, out, "30")
}
