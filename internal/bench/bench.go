package bench

import (
	"context"
	"fmt"
	"io"
	"time"
)

// TokenEvent carries a single streamed token. Reasoning holds thinking-phase
// text from reasoning models (streamed separately from Content). PromptTokens
// is >0 only on the final usage-carrying event (see vllm.StreamChunk).
type TokenEvent struct {
	Content      string
	Reasoning    string
	FinishReason string
	PromptTokens int
}

// StreamFn is the signature Run accepts. Adapters bridge to the concrete client.
type StreamFn func(ctx context.Context, model, prompt string, maxTokens int, fn func(TokenEvent) error) error

// Result holds benchmark measurements for a single run.
//
// TTFT is time to the first generated token of either kind — reasoning or
// content — so it measures prefill regardless of how long a model thinks.
// TimeToContent is time to the first visible content token; for non-reasoning
// models it equals TTFT. OutputToks counts reasoning and content tokens alike,
// since both cost the same decode step on the GPU.
type Result struct {
	TTFT              time.Duration // time to first generated token (reasoning or content)
	TimeToContent     time.Duration // time to first visible content token; 0 if none arrived
	TotalTime         time.Duration // wall time from send to final token
	OutputToks        int           // reasoning + content tokens received
	ReasoningToks     int           // reasoning tokens received
	PromptToks        int           // number of prompt tokens, from the server's reported usage
	DecodeToksPerSec  float64       // (OutputToks-1) / (TotalTime - TTFT)
	PrefillToksPerSec float64       // PromptToks / TTFT — approximates prompt-processing throughput
}

// NowFunc is injectable for tests.
var NowFunc = time.Now

// Run executes one benchmark iteration. stream must call fn for each SSE chunk.
func Run(ctx context.Context, stream StreamFn, model, prompt string, maxTokens int) (*Result, error) {
	start := NowFunc()
	var ttft, timeToContent time.Duration
	var last time.Time
	sawToken, sawContent := false, false
	toks, reasoningToks, promptToks := 0, 0, 0

	err := stream(ctx, model, prompt, maxTokens, func(tok TokenEvent) error {
		now := NowFunc()
		generated := tok.Content != "" || tok.Reasoning != ""
		if generated && !sawToken {
			ttft = now.Sub(start)
			sawToken = true
		}
		if tok.Content != "" && !sawContent {
			timeToContent = now.Sub(start)
			sawContent = true
		}
		if generated {
			toks++
		}
		if tok.Reasoning != "" {
			reasoningToks++
		}
		if tok.PromptTokens > 0 {
			promptToks = tok.PromptTokens
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
		TTFT:          ttft,
		TimeToContent: timeToContent,
		TotalTime:     total,
		OutputToks:    toks,
		ReasoningToks: reasoningToks,
		PromptToks:    promptToks,
	}
	decodeTime := total - ttft
	if toks > 1 && decodeTime > 0 {
		r.DecodeToksPerSec = float64(toks-1) / decodeTime.Seconds()
	}
	if promptToks > 0 && ttft > 0 {
		r.PrefillToksPerSec = float64(promptToks) / ttft.Seconds()
	}
	return r, nil
}

// Stats summarises multiple Results.
type Stats struct {
	Runs                 int
	AvgTTFT              time.Duration
	MinTTFT              time.Duration
	MaxTTFT              time.Duration
	AvgDecodeToksPerSec  float64
	AvgPrefillToksPerSec float64
	AvgTimeToContent     time.Duration // averaged over runs that produced visible content
	ContentRuns          int           // runs that produced any visible content
	TotalOutputToks      int
	TotalReasoningToks   int
	TotalPromptToks      int
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
	var sumTTFT, sumContent time.Duration
	var sumDecode float64
	var sumPrefill float64
	for _, r := range results {
		sumTTFT += r.TTFT
		sumDecode += r.DecodeToksPerSec
		sumPrefill += r.PrefillToksPerSec
		s.TotalOutputToks += r.OutputToks
		s.TotalReasoningToks += r.ReasoningToks
		s.TotalPromptToks += r.PromptToks
		if r.TimeToContent > 0 {
			sumContent += r.TimeToContent
			s.ContentRuns++
		}
		if r.TTFT < s.MinTTFT {
			s.MinTTFT = r.TTFT
		}
		if r.TTFT > s.MaxTTFT {
			s.MaxTTFT = r.TTFT
		}
	}
	s.AvgTTFT = sumTTFT / time.Duration(len(results))
	s.AvgDecodeToksPerSec = sumDecode / float64(len(results))
	s.AvgPrefillToksPerSec = sumPrefill / float64(len(results))
	if s.ContentRuns > 0 {
		s.AvgTimeToContent = sumContent / time.Duration(s.ContentRuns)
	}
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
	_, _ = fmt.Fprintf(w, "prefill tok/s    : %.1f\n", s.AvgPrefillToksPerSec)
	_, _ = fmt.Fprintf(w, "decode tok/s     : %.1f\n", s.AvgDecodeToksPerSec)
	_, _ = fmt.Fprintf(w, "total prompt toks: %d\n", s.TotalPromptToks)
	_, _ = fmt.Fprintf(w, "total output toks: %d\n", s.TotalOutputToks)
	if s.TotalReasoningToks == 0 {
		return
	}
	// Reasoning model: show the thinking phase separately so decode tok/s
	// (which includes reasoning tokens) isn't mistaken for answer speed alone.
	_, _ = fmt.Fprintf(w, "  reasoning toks : %d\n", s.TotalReasoningToks)
	_, _ = fmt.Fprintf(w, "  content toks   : %d\n", s.TotalOutputToks-s.TotalReasoningToks)
	if s.ContentRuns > 0 {
		_, _ = fmt.Fprintf(w, "time to content  : %s avg (%d/%d runs reached content)\n",
			s.AvgTimeToContent.Round(time.Millisecond), s.ContentRuns, s.Runs)
	} else {
		_, _ = fmt.Fprintf(w, "time to content  : n/a (no run got past reasoning — raise --max-tokens)\n")
	}
}
