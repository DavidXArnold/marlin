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

	out := Env(m)
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

	out := Env(m)
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

	out := Env(m)
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

	out := Env(m)
	assert.Contains(t, out, "--quantization awq_marlin")
}

func TestEnvRendersGPUMemory(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824},
	}

	out := Env(m)
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

	out := Env(m)
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

	out := Env(m)
	assert.Contains(t, out, "--safetensors-load-strategy=prefetch")
}

func TestEnvOmitsEmptyQuantization(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824},
	}

	out := Env(m)
	assert.NotContains(t, out, "--quantization")
}

func TestEnvOmitsZeroMaxModelLen(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{ID: "some/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.824, MaxModelLen: 0},
	}

	out := Env(m)
	assert.NotContains(t, out, "--max-model-len")
}
