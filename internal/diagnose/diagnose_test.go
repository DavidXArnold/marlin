package diagnose

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Excerpts below are trimmed from real gn100 crash logs.

const oomLogs = `Oct 21 20:14:22 gn100-01 marlin[2756075]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436] EngineCore failed to start.
Oct 21 20:14:22 gn100-01 marlin[2756075]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436] Traceback (most recent call last):
Oct 21 20:14:22 gn100-01 marlin[2756075]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436]   File "/usr/local/lib/python3.12/dist-packages/vllm/v1/engine/core.py", line 1398, in run_engine_core
Oct 21 20:14:22 gn100-01 marlin[2756075]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436] torch.AcceleratorError: CUDA error: out of memory
Oct 21 20:14:22 gn100-01 marlin[2756075]: (EngineCore pid=279) ERROR 10-22 02:14:22 [core.py:1436] Search for ` + "`cudaErrorMemoryAllocation'" + ` in https://docs.nvidia.com/cuda/
Oct 21 20:14:24 gn100-01 marlin[2756075]: (APIServer pid=1) RuntimeError: Engine core initialization failed. See root cause above. Failed core proc(s): {}
Oct 21 20:14:27 gn100-01 systemd[1]: marlin.service: Main process exited, code=exited, status=1/FAILURE`

func TestAnalyzeOOM(t *testing.T) {
	r := Analyze(oomLogs)
	assert.Equal(t, "torch.AcceleratorError: CUDA error: out of memory", r.Cause)
	assert.Contains(t, r.Hint, "doesn't fit")
}

func TestAnalyzeFreeMemoryPrefersSpecificHint(t *testing.T) {
	logs := `(EngineCore pid=300) ERROR 09-26 19:38:03 [core.py:1436] ValueError: Free memory on device cuda:0 (113.12/119.69 GiB) on startup is less than desired GPU memory utilization (0.95, 113.71 GiB).`
	r := Analyze(logs)
	assert.Contains(t, r.Cause, "ValueError: Free memory on device")
	assert.Contains(t, r.Hint, "gpu_memory_utilization")
}

func TestAnalyzeUnknownArchitecture(t *testing.T) {
	logs := `(APIServer pid=1) pydantic_core._pydantic_core.ValidationError: 1 validation error for ModelConfig
(APIServer pid=1)   Value error, The checkpoint you are trying to load has model type ` + "`qwen4_exp`" + ` but Transformers does not recognize this architecture.`
	r := Analyze(logs)
	assert.Contains(t, r.Cause, "ValidationError")
	assert.Contains(t, r.Hint, "newer build")
}

func TestAnalyzeGated(t *testing.T) {
	r := Analyze(`(APIServer pid=1) huggingface_hub.errors.GatedRepoError: 403 Client Error.`)
	assert.Contains(t, r.Cause, "GatedRepoError")
	assert.Contains(t, r.Hint, "license")
}

func TestAnalyzeContainerNameConflict(t *testing.T) {
	r := Analyze(`marlin[1]: docker: Error response from daemon: Conflict. The container name "/marlin-vllm" is already in use by container "8efb".`)
	assert.Contains(t, r.Cause, "Error response from daemon")
	assert.Contains(t, r.Hint, "docker rm -f")
}

func TestAnalyzeSpeculativeZeroDivision(t *testing.T) {
	logs := `File "/usr/local/lib/python3.12/dist-packages/vllm/config/speculative.py", line 1487, in __post_init__
ZeroDivisionError: integer modulo by zero`
	r := Analyze(logs)
	assert.Equal(t, "ZeroDivisionError: integer modulo by zero", r.Cause)
	assert.Contains(t, r.Hint, "--speculative-config")
}

func TestAnalyzeKVCacheAndNvFp4(t *testing.T) {
	assert.Contains(t, Analyze("AssertionError: DeepseekV4 only supports fp8 kv-cache format for now, got auto").Hint, "kv-cache-dtype")
	assert.Contains(t, Analyze("NotImplementedError: No NvFp4 MoE backend supports the deployment configuration.").Hint, "NVFP4")
}

func TestAnalyzeUnrecognisedStillReportsCause(t *testing.T) {
	r := Analyze("INFO starting\nKeyError: 'weird_param'\nINFO done")
	assert.Equal(t, "KeyError: 'weird_param'", r.Cause)
	assert.Empty(t, r.Hint)
}

func TestAnalyzeNothingFound(t *testing.T) {
	assert.Equal(t, Result{}, Analyze("INFO all good\nINFO still loading"))
}

func TestAnalyzeTruncatesLongCause(t *testing.T) {
	r := Analyze("ValueError: " + strings.Repeat("x", 400))
	assert.LessOrEqual(t, len([]rune(r.Cause)), 301)
	assert.True(t, strings.HasSuffix(r.Cause, "…"))
}
