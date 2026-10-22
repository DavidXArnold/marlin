// Package diagnose extracts a startup failure's root cause from inference
// server logs and maps known failure signatures to an actionable hint.
package diagnose

import (
	"regexp"
	"strings"
)

// Result is the outcome of scanning a failed start's logs.
type Result struct {
	Cause string // the most relevant error line, de-noised; "" if none found
	Hint  string // suggested fix for a recognised failure; "" if unrecognised
}

type signature struct {
	match func(logs string) bool
	hint  string
}

func contains(subs ...string) func(string) bool {
	return func(logs string) bool {
		for _, s := range subs {
			if !strings.Contains(logs, s) {
				return false
			}
		}
		return true
	}
}

func containsAny(subs ...string) func(string) bool {
	return func(logs string) bool {
		for _, s := range subs {
			if strings.Contains(logs, s) {
				return true
			}
		}
		return false
	}
}

// Order matters: more specific signatures come before generic ones.
var signatures = []signature{
	{contains("Free memory on device", "less than desired GPU memory utilization"),
		"other processes hold GPU memory — lower serve.gpu_memory_utilization (e.g. 0.85) or free memory first"},
	{containsAny("CUDA error: out of memory", "CUDA out of memory", "OutOfMemoryError", "cudaErrorMemoryAllocation"),
		"model doesn't fit in GPU memory — lower serve.max_model_len, add --kv-cache-dtype fp8 / --language-model-only, or use a smaller quantization"},
	{contains("Transformers does not recognize this architecture"),
		"this vLLM image is too old for the model's architecture — set [model] image to a newer build (e.g. vllm/vllm-openai:nightly)"},
	{containsAny("GatedRepoError", "Cannot access gated repo"),
		"gated model — accept its license on huggingface.co with the account behind HF_TOKEN, or use an ungated mirror"},
	{contains("only supports fp8 kv-cache"),
		`add "--kv-cache-dtype", "fp8" to serve.extra_flags`},
	{contains("No NvFp4 MoE backend supports"),
		"no NVFP4 MoE kernel for this GPU in this vLLM build — try a newer image or a different quantization"},
	{contains("ZeroDivisionError", "speculative"),
		"speculative decoding config is incompatible with this checkpoint/vLLM build — remove --speculative-config"},
	{contains("is already in use by container"),
		"a stale container holds the name — remove it with 'docker rm -f <name>' (marlin-vllm / marlin-llamacpp)"},

	// llama.cpp (llama-server)
	{contains("unknown model architecture: '"),
		"this llama.cpp build doesn't know the model's architecture — set [model] image (or service.llamacpp_image) to a newer llama.cpp image"},
	{containsAny("cudaMalloc failed: out of memory", "failed to allocate buffer for kv cache", "unable to allocate CUDA"),
		"model doesn't fit in GPU memory — use a smaller quant in model.id, lower serve.context_size, or lower serve.ngl to keep some layers on the CPU"},
	{containsAny("failed to download model from HuggingFace", "download failed:"),
		"the Hugging Face download failed — check model.id is '<org>/<repo>:<quant>' with an existing quant, and that HF_TOKEN can access the repo"},
	{contains("couldn't bind HTTP server socket"),
		"server.port is already in use — stop the other inference server (another marlin service or a hand-run container) first"},
}

// Lines that carry no diagnostic value on their own.
var noise = regexp.MustCompile(`^\s*(\^+|~+|File "|Traceback|During handling|The above exception|raise |return |self\.|For further information|For more detailed|Search for )`)

// journald / vLLM prefixes to strip so the cause reads cleanly, e.g.
// "Oct 21 20:14:22 gn100-01 marlin[123]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436] ".
var prefixes = []*regexp.Regexp{
	regexp.MustCompile(`^[A-Z][a-z]{2} +\d+ [\d:]+ \S+ \S+\[\d+\]: `),
	regexp.MustCompile(`^\([A-Za-z]+ pid=\d+\) `),
	regexp.MustCompile(`^(ERROR|WARNING|INFO) [\d-]+ [\d:]+ \[[^\]]+\] `),
}

var errorLine = regexp.MustCompile(`([A-Za-z]+(Error|Exception)\b|Error response from daemon|error:|out of memory|error loading model|failed to allocate|download failed|couldn't bind)`)

func clean(line string) string {
	for _, p := range prefixes {
		line = p.ReplaceAllString(line, "")
	}
	return strings.TrimSpace(line)
}

// Analyze scans logs (oldest first) and returns the root cause and a hint.
// It prefers the first error line of the last failure, skipping wrapper
// errors like "Engine core initialization failed" that only point elsewhere.
func Analyze(logs string) Result {
	var r Result
	for _, s := range signatures {
		if s.match(logs) {
			r.Hint = s.hint
			break
		}
	}

	lines := strings.Split(logs, "\n")
	candidates := make([]string, 0, len(lines))
	for _, raw := range lines {
		l := clean(raw)
		if l == "" || noise.MatchString(l) || !errorLine.MatchString(l) {
			continue
		}
		if strings.Contains(l, "See root cause above") || strings.Contains(l, "EngineCore failed to start") {
			continue
		}
		candidates = append(candidates, l)
	}
	if len(candidates) == 0 {
		return r
	}
	// The last distinct error is usually the most specific of the final crash.
	r.Cause = candidates[len(candidates)-1]
	if len(r.Cause) > 300 {
		r.Cause = r.Cause[:300] + "…"
	}
	return r
}
