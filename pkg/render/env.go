package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DavidXArnold/marlin/internal/config"
)

// Env renders a ModelConfig to the .env format consumed by the vLLM systemd service.
// hfToken is written as HF_TOKEN when non-empty, so vLLM can pull gated HF models.
func Env(m *config.ModelConfig, hfToken string) string {
	var b strings.Builder

	model := m.Model.ID
	if m.Container.ModelArg != "" {
		model = m.Container.ModelArg
	}
	fmt.Fprintf(&b, "VLLM_MODEL=%s\n", model)

	if m.Model.Image != "" {
		fmt.Fprintf(&b, "VLLM_IMAGE=%s\n", m.Model.Image)
	}

	if hfToken != "" {
		fmt.Fprintf(&b, "HF_TOKEN=%s\n", hfToken)
	}

	args := buildExtraArgs(m)
	if len(args) > 0 {
		fmt.Fprintf(&b, "VLLM_EXTRA_ARGS=%s\n", strings.Join(args, " "))
	}

	return b.String()
}

// llamaCppAllLayers is the -ngl value used when a profile leaves ngl unset;
// llama-server clamps it to the model's layer count, so it means "all".
const llamaCppAllLayers = 999

// LlamaCppServerBin is llama-server's path in the official images, exec'd by a
// pre_serve hook when the profile sets no container.entrypoint.
const LlamaCppServerBin = "/app/llama-server"

// LlamaCppEnv renders a ModelConfig to the .env format consumed by the
// marlin-llamacpp systemd service (llama-server).
//
// The model source is serve.gguf_path when set (a local file, passed as -m),
// otherwise model.id as a Hugging Face "<repo>[:<quant>]" reference (passed as
// -hf, so llama-server downloads it into the HF cache on first start).
// image is written as LLAMA_IMAGE for the containerized unit and may be empty
// in binary mode. hfToken is written as HF_TOKEN when non-empty.
//
// launch carries the profile's [container] table (see BuildContainerLaunch).
func LlamaCppEnv(m *config.ModelConfig, image, hfToken string, launch ContainerLaunch) string {
	var b strings.Builder

	if image != "" {
		fmt.Fprintf(&b, "LLAMA_IMAGE=%s\n", image)
	}
	if hfToken != "" {
		fmt.Fprintf(&b, "HF_TOKEN=%s\n", hfToken)
	}

	var mounts []string
	if m.Serve.GGUFPath != "" {
		fmt.Fprintf(&b, "LLAMA_MODEL_ARGS=-m %s\n", m.Serve.GGUFPath)
		// A local file is only visible inside the container if its directory is mounted.
		dir := filepath.Dir(m.Serve.GGUFPath)
		mounts = []string{"-v", dir + ":" + dir + ":ro"}
	} else {
		fmt.Fprintf(&b, "LLAMA_MODEL_ARGS=-hf %s\n", m.Model.ID)
	}
	b.WriteString(LaunchEnv("LLAMA", launch, mounts...))

	ngl := m.Serve.NGL
	if ngl <= 0 {
		ngl = llamaCppAllLayers
	}
	args := []string{fmt.Sprintf("-ngl %d", ngl)}
	if len(m.Serve.ServedModelName) > 0 {
		args = append(args, "--alias "+strings.Join(m.Serve.ServedModelName, ","))
	}
	if m.Serve.ContextSize > 0 {
		args = append(args, fmt.Sprintf("-c %d", m.Serve.ContextSize))
	}
	args = append(args, m.Serve.ExtraFlags...)
	fmt.Fprintf(&b, "LLAMA_EXTRA_ARGS=%s\n", strings.Join(args, " "))

	// Legacy variables, kept for hand-written units that predate the args above.
	if m.Serve.GGUFPath != "" {
		fmt.Fprintf(&b, "LLAMA_MODEL=%s\n", m.Serve.GGUFPath)
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

	if m.Serve.TrustRemoteCode {
		args = append(args, "--trust-remote-code")
	}

	args = append(args, m.Serve.ExtraFlags...)

	return args
}
