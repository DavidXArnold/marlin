package render

import (
	"fmt"
	"strings"

	"github.com/DavidXArnold/marlin/internal/config"
)

// Env renders a ModelConfig to the .env format consumed by the vLLM systemd service.
func Env(m *config.ModelConfig) string {
	var b strings.Builder

	fmt.Fprintf(&b, "VLLM_MODEL=%s\n", m.Model.ID)

	args := buildExtraArgs(m)
	if len(args) > 0 {
		fmt.Fprintf(&b, "VLLM_EXTRA_ARGS=%s\n", strings.Join(args, " "))
	}

	return b.String()
}

// LlamaCppEnv renders a ModelConfig to the .env format consumed by the
// marlin-llamacpp systemd service (llama-server).
func LlamaCppEnv(m *config.ModelConfig) string {
	var b strings.Builder

	fmt.Fprintf(&b, "LLAMA_MODEL=%s\n", m.Serve.GGUFPath)

	ngl := m.Serve.NGL
	if ngl <= 0 {
		ngl = 99 // default: offload all layers to GPU
	}
	fmt.Fprintf(&b, "LLAMA_NGL=%d\n", ngl)

	if m.Serve.ContextSize > 0 {
		fmt.Fprintf(&b, "LLAMA_CONTEXT=%d\n", m.Serve.ContextSize)
	}

	return b.String()
}

func buildExtraArgs(m *config.ModelConfig) []string {
	var args []string

	if m.Serve.ToolCallParser != "" {
		args = append(args, "--enable-auto-tool-choice")
		args = append(args, "--tool-call-parser "+m.Serve.ToolCallParser)
	}

	if len(m.Serve.ServedModelName) > 0 {
		args = append(args, "--served-model-name "+strings.Join(m.Serve.ServedModelName, " "))
	}

	// nvfp4 is auto-detected by vLLM from the model config; no --quantization flag needed.
	if m.Serve.Quantization != "" && m.Serve.Quantization != "nvfp4" {
		args = append(args, "--quantization "+m.Serve.Quantization)
	}

	if m.Serve.GPUMemoryUtilization > 0 {
		args = append(args, fmt.Sprintf("--gpu-memory-utilization %.3f", m.Serve.GPUMemoryUtilization))
	}

	if m.Serve.MaxModelLen > 0 {
		args = append(args, fmt.Sprintf("--max-model-len %d", m.Serve.MaxModelLen))
	}

	args = append(args, m.Serve.ExtraFlags...)

	return args
}
