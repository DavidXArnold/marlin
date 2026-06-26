package render

import (
	"fmt"
	"strings"

	"github.com/DavidXArnold/marlin/internal/config"
)

// Inspect returns a human-readable summary of the runtime configuration that
// will be used when the model is started. For vLLM this includes the env file
// content and the systemd ExecStart line. For NIM it includes the container
// image, ports, volumes, and environment.
//
// slug is the model's config file stem (used in file path display).
// hfToken is shown as a placeholder when non-empty; the actual token is not printed.
func Inspect(m *config.ModelConfig, cfg *config.Config, slug, hfToken string) string {
	var b strings.Builder

	writef := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
	}

	// Model identity.
	writef("=== model ===\n")
	if m.Model.ID != "" {
		writef("id     : %s\n", m.Model.ID)
	}
	if m.Model.Image != "" {
		writef("image  : %s\n", m.Model.Image)
	}
	writef("type   : %s\n", m.Model.Type)
	writef("status : %s\n", m.Model.Status)

	switch m.Model.Type {
	case config.ProviderVLLM:
		inspectVLLM(&b, m, cfg, slug, hfToken)
	case config.ProviderNIM:
		inspectNIM(&b, m, cfg)
	}

	return b.String()
}

func inspectVLLM(b *strings.Builder, m *config.ModelConfig, cfg *config.Config, slug, hfToken string) {
	writef := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }

	writef("\n=== serve flags ===\n")
	if m.Serve.GPUMemoryUtilization > 0 {
		writef("gpu_memory_utilization: %.3f\n", m.Serve.GPUMemoryUtilization)
	}
	if len(m.Serve.ServedModelName) > 0 {
		writef("served_model_name     : %s\n", strings.Join(m.Serve.ServedModelName, ", "))
	}
	if m.Serve.Quantization != "" {
		if m.Serve.Quantization == "nvfp4" {
			writef("quantization          : nvfp4 (auto-detected, no --quantization flag)\n")
		} else {
			writef("quantization          : %s\n", m.Serve.Quantization)
		}
	}
	if m.Serve.MaxModelLen > 0 {
		writef("max_model_len         : %d\n", m.Serve.MaxModelLen)
	}
	if m.Serve.TrustRemoteCode {
		writef("trust_remote_code     : true\n")
	}
	if len(m.Serve.ExtraFlags) > 0 {
		writef("extra_flags           : %s\n", strings.Join(m.Serve.ExtraFlags, " "))
	}

	writef("\n=== env file (%s/%s.env) ===\n", cfg.Paths.ModelsDir, slug)
	writef("%s", Env(m, ""))
	if hfToken != "" {
		writef("HF_TOKEN=*** (from secrets)\n")
	}

	writef("\n=== systemd ExecStart ===\n")
	if cfg.Service.VLLMMode != "binary" && m.Model.Image != "" {
		containerBin, _ := ResolveContainerBin(cfg)
		writef("/bin/bash -c 'exec %s run --rm --gpus all --ipc host --network host --name marlin-vllm --label marlin.managed=true -e \"HF_TOKEN=${HF_TOKEN:-}\" \"${VLLM_IMAGE}\" vllm serve \"${VLLM_MODEL}\" --host 0.0.0.0 --port %d ${VLLM_EXTRA_ARGS:-}'\n",
			containerBin, cfg.Server.Port)
	} else {
		vllmBin, _ := ResolveVLLMBin(cfg.Service.VLLMBin)
		writef("/bin/bash -c 'exec %s serve \"$VLLM_MODEL\" --host %s --port %d ${VLLM_EXTRA_ARGS:-}'\n",
			vllmBin, cfg.Server.Host, cfg.Server.Port)
	}
}

func inspectNIM(b *strings.Builder, m *config.ModelConfig, cfg *config.Config) {
	writef := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }

	writef("\n=== container config ===\n")
	writef("image  : %s\n", m.Model.Image)
	writef("port   : %d:8000\n", cfg.Server.Port)
	writef("volume : %s:/opt/nim/.cache\n", cfg.Paths.NIMCache)
	for _, v := range m.Serve.ExtraVolumes {
		writef("volume : %s\n", v)
	}
	writef("env    : NGC_API_KEY=*** (from secrets)\n")
	for _, e := range m.Serve.ExtraEnv {
		writef("env    : %s\n", e)
	}

	writef("\n=== equivalent docker run ===\n")
	writef("docker run --rm --gpus all \\\n")
	writef("  -p %d:8000 \\\n", cfg.Server.Port)
	writef("  -v %s:/opt/nim/.cache \\\n", cfg.Paths.NIMCache)
	for _, v := range m.Serve.ExtraVolumes {
		writef("  -v %s \\\n", v)
	}
	writef("  -e NGC_API_KEY=$NGC_API_KEY \\\n")
	for _, e := range m.Serve.ExtraEnv {
		writef("  -e %s \\\n", e)
	}
	writef("  --name marlin-nim \\\n")
	writef("  %s\n", m.Model.Image)
}
