package bench

import (
	"context"
	"fmt"
	"io"
	"time"
)

// TokenEvent carries a single streamed token.
type TokenEvent struct {
	Content      string
	FinishReason string
}

// StreamFn is the signature Run accepts. Adapters bridge to the concrete client.
type StreamFn func(ctx context.Context, model, prompt string, maxTokens int, fn func(TokenEvent) error) error

// Result holds benchmark measurements for a single run.
type Result struct {
	TTFT             time.Duration // time to first token
	TotalTime        time.Duration // wall time from send to final token
	OutputToks       int           // number of output tokens received
	DecodeToksPerSec float64       // (OutputToks-1) / (TotalTime - TTFT)
}

// NowFunc is injectable for tests.
var NowFunc = time.Now

// Run executes one benchmark iteration. stream must call fn for each SSE chunk.
func Run(ctx context.Context, stream StreamFn, model, prompt string, maxTokens int) (*Result, error) {
	start := NowFunc()
	var ttft time.Duration
	var last time.Time
	firstToken := true
	toks := 0

	err := stream(ctx, model, prompt, maxTokens, func(tok TokenEvent) error {
		now := NowFunc()
		if firstToken && tok.Content != "" {
			ttft = now.Sub(start)
			firstToken = false
		}
		if tok.Content != "" {
			toks++
		}
		last = now
		return nil
	})
	if err != nil {
		return nil, err
	}

	if last.IsZero() {
		last = NowFunc()
	}
	total := last.Sub(start)

	r := &Result{
		TTFT:       ttft,
		TotalTime:  total,
		OutputToks: toks,
	}
	decodeTime := total - ttft
	if toks > 1 && decodeTime > 0 {
		r.DecodeToksPerSec = float64(toks-1) / decodeTime.Seconds()
	}
	return r, nil
}

// Stats summarises multiple Results.
type Stats struct {
	Runs                int
	AvgTTFT             time.Duration
	MinTTFT             time.Duration
	MaxTTFT             time.Duration
	AvgDecodeToksPerSec float64
	TotalOutputToks     int
}

// Summarise computes aggregate statistics over results.
func Summarise(results []*Result) *Stats {
	if len(results) == 0 {
		return &Stats{}
	}
	s := &Stats{
		Runs:    len(results),
		MinTTFT: results[0].TTFT,
		MaxTTFT: results[0].TTFT,
	}
	var sumTTFT time.Duration
	var sumDecode float64
	for _, r := range results {
		sumTTFT += r.TTFT
		sumDecode += r.DecodeToksPerSec
		s.TotalOutputToks += r.OutputToks
		if r.TTFT < s.MinTTFT {
			s.MinTTFT = r.TTFT
		}
		if r.TTFT > s.MaxTTFT {
			s.MaxTTFT = r.TTFT
		}
	}
	s.AvgTTFT = sumTTFT / time.Duration(len(results))
	s.AvgDecodeToksPerSec = sumDecode / float64(len(results))
	return s
}

// Print writes a human-readable summary of stats to w.
func Print(w io.Writer, s *Stats, model string) {
	_, _ = fmt.Fprintf(w, "model            : %s\n", model)
	_, _ = fmt.Fprintf(w, "runs             : %d\n", s.Runs)
	_, _ = fmt.Fprintf(w, "TTFT avg/min/max : %s / %s / %s\n",
		s.AvgTTFT.Round(time.Millisecond),
		s.MinTTFT.Round(time.Millisecond),
		s.MaxTTFT.Round(time.Millisecond))
	_, _ = fmt.Fprintf(w, "decode tok/s     : %.1f\n", s.AvgDecodeToksPerSec)
	_, _ = fmt.Fprintf(w, "total output toks: %d\n", s.TotalOutputToks)
}
