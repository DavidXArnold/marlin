package render

import (
	"testing"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestEnvRendersModelID(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "Qwen/Qwen2.5-72B-Instruct-AWQ"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824},
	}

	out := Env(m, "")
	assert.Contains(t, out, "VLLM_MODEL=Qwen/Qwen2.5-72B-Instruct-AWQ")
}

func TestEnvRendersToolCallParser(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "Qwen/Qwen2.5-72B-Instruct-AWQ"},
		Serve: config.ServeConfig{
			ToolCallParser:       "hermes",
			GPUMemoryUtilization: 0.824,
		},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--enable-auto-tool-choice")
	assert.Contains(t, out, "--tool-call-parser hermes")
}

func TestEnvRendersServedModelName(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{
			ServedModelName:      []string{"gn100", "qwen25-72b"},
			GPUMemoryUtilization: 0.824,
		},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--served-model-name")
	assert.Contains(t, out, "gn100 qwen25-72b")
}

func TestEnvRendersQuantization(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{
			Quantization:         "awq_marlin",
			GPUMemoryUtilization: 0.824,
		},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--quantization awq_marlin")
}

func TestEnvRendersGPUMemory(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--gpu-memory-utilization 0.824")
}

func TestEnvRendersMaxModelLen(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{
			GPUMemoryUtilization: 0.824,
			MaxModelLen:          131072,
		},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--max-model-len 131072")
}

func TestEnvRendersExtraFlags(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{
			GPUMemoryUtilization: 0.824,
			ExtraFlags:           []string{"--safetensors-load-strategy=prefetch"},
		},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--safetensors-load-strategy=prefetch")
}

func TestEnvOmitsEmptyQuantization(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824},
	}

	out := Env(m, "")
	assert.NotContains(t, out, "--quantization")
}

func TestEnvNVFP4OmitsQuantizationFlag(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "nvidia/Qwen3-32B-NVFP4"},
		Serve: config.ServeConfig{
			Quantization:         "nvfp4",
			GPUMemoryUtilization: 0.90,
		},
	}

	out := Env(m, "")
	assert.NotContains(t, out, "--quantization", "nvfp4 is auto-detected; flag must not be emitted")
}

func TestEnvOmitsZeroMaxModelLen(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824, MaxModelLen: 0},
	}

	out := Env(m, "")
	assert.NotContains(t, out, "--max-model-len")
}

func TestEnvInjectsHFToken(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "meta-llama/Llama-3.1-8B-Instruct"},
		Serve: config.ServeConfig{},
	}

	out := Env(m, "hf_abc123")
	assert.Contains(t, out, "HF_TOKEN=hf_abc123")
}

func TestEnvOmitsHFTokenWhenEmpty(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "meta-llama/Llama-3.1-8B-Instruct"},
	}

	out := Env(m, "")
	assert.NotContains(t, out, "HF_TOKEN")
}

func TestEnvTrustRemoteCode(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{TrustRemoteCode: true},
	}

	out := Env(m, "")
	assert.Contains(t, out, "--trust-remote-code")
}

func TestEnvOmitsTrustRemoteCodeWhenFalse(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{TrustRemoteCode: false},
	}

	out := Env(m, "")
	assert.NotContains(t, out, "--trust-remote-code")
}

func TestEnvIncludesVLLMImage(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{
			ID:    "nvidia/Qwen3-32B-NVFP4",
			Image: "nvcr.io/nvidia/vllm:26.05.post1-py3",
		},
	}
	out := Env(m, "")
	assert.Contains(t, out, "VLLM_IMAGE=nvcr.io/nvidia/vllm:26.05.post1-py3")
}

func TestEnvOmitsVLLMImageWhenEmpty(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
	}
	out := Env(m, "")
	assert.NotContains(t, out, "VLLM_IMAGE")
}

func TestLlamaCppEnvBasic(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "llama-3b", Type: config.ProviderLlamaCpp},
		Serve: config.ServeConfig{
			GGUFPath:    "/models/llama-3.2-3b-q4_k_m.gguf",
			NGL:         99,
			ContextSize: 4096,
		},
	}

	out := LlamaCppEnv(m)
	assert.Contains(t, out, "LLAMA_MODEL=/models/llama-3.2-3b-q4_k_m.gguf")
	assert.Contains(t, out, "LLAMA_NGL=99")
	assert.Contains(t, out, "LLAMA_CONTEXT=4096")
}

func TestLlamaCppEnvDefaultNGL(t *testing.T) {
	m := &config.ModelConfig{
		Serve: config.ServeConfig{GGUFPath: "/models/test.gguf"},
	}

	out := LlamaCppEnv(m)
	assert.Contains(t, out, "LLAMA_NGL=99")
}

func TestLlamaCppEnvOmitsContextWhenZero(t *testing.T) {
	m := &config.ModelConfig{
		Serve: config.ServeConfig{GGUFPath: "/models/test.gguf", NGL: 35},
	}

	out := LlamaCppEnv(m)
	assert.NotContains(t, out, "LLAMA_CONTEXT")
}
