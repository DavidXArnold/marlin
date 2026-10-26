package render

import (
	"fmt"
	"path/filepath"
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
	case config.ProviderLlamaCpp:
		inspectLlamaCpp(&b, m, cfg, slug, hfToken)
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

	// In container mode, show VLLM_IMAGE even when the model profile omits
	// model.image — fall back to the global default image for display.
	mDisplay := m
	if cfg.Service.VLLMMode != "binary" && m.Model.Image == "" && cfg.Service.VLLMImage != "" {
		m2 := *m
		m2.Model.Image = cfg.Service.VLLMImage
		mDisplay = &m2
	}

	mDisplay, launch := inspectContainer(b, mDisplay, cfg, slug, cfg.Service.VLLMMode, "vllm")

	writef("\n=== env file (%s/%s.env) ===\n", cfg.Paths.ModelsDir, slug)
	writef("%s", Env(mDisplay, ""))
	writef("%s", LaunchEnv("VLLM", launch))
	if hfToken != "" {
		writef("HF_TOKEN=*** (from secrets)\n")
	}

	writef("\n=== systemd ExecStart ===\n")
	if cfg.Service.VLLMMode != "binary" {
		containerBin, _ := ResolveContainerBin(cfg)
		writef("%s\n", execStartLine(SystemdUnitContainerized(cfg, containerBin, ResolveHFCache(cfg))))
	} else {
		vllmBin, _ := ResolveVLLMBin(cfg.Service.VLLMBin)
		writef("%s\n", execStartLine(SystemdUnit(cfg, vllmBin)))
	}
}

// execStartLine returns the ExecStart command of a rendered unit.
func execStartLine(unit string) string {
	for _, l := range strings.Split(unit, "\n") {
		if strings.HasPrefix(l, "ExecStart=") {
			return strings.TrimPrefix(l, "ExecStart=")
		}
	}
	return ""
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

func inspectLlamaCpp(b *strings.Builder, m *config.ModelConfig, cfg *config.Config, slug, hfToken string) {
	writef := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }

	image := ""
	if cfg.Service.LlamaCppMode != "binary" {
		image = m.Model.Image
		if image == "" {
			image = cfg.Service.LlamaCppImage
		}
	}

	m, launch := inspectContainer(b, m, cfg, slug, cfg.Service.LlamaCppMode, LlamaCppServerBin)

	writef("\n=== env file (%s/%s.llamacpp.env) ===\n", cfg.Paths.ModelsDir, slug)
	writef("%s", LlamaCppEnv(m, image, "", launch))
	if hfToken != "" {
		writef("HF_TOKEN=*** (from secrets)\n")
	}

	writef("\n=== systemd unit (%s) ===\n", LlamaCppUnitPath(cfg))
	if cfg.Service.LlamaCppMode == "binary" {
		bin, _ := ResolveLlamaCppBin(cfg.Service.LlamaCppBin)
		writef("%s", SystemdUnitLlamaCpp(cfg, bin))
		return
	}
	containerBin, _ := ResolveContainerBin(cfg)
	writef("%s", SystemdUnitLlamaCppContainerized(cfg, containerBin, ResolveHFCache(cfg)))
}

// inspectContainer expands placeholders and prints what the profile's
// [container] table renders to. It returns the expanded model (or m unchanged
// when expansion fails, after printing why) and the container launch.
func inspectContainer(b *strings.Builder, m *config.ModelConfig, cfg *config.Config, slug, mode, defaultEntrypoint string) (*config.ModelConfig, ContainerLaunch) {
	writef := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }

	expanded, err := ExpandModel(m, NewExpandContext(cfg))
	if err != nil {
		writef("\nwarning: %v\n", err)
	}
	if expanded.Container.IsZero() {
		return expanded, ContainerLaunch{}
	}
	if mode == "binary" {
		writef("\nnote: [container] is ignored in binary mode\n")
		return expanded, ContainerLaunch{}
	}

	envFile, hookFile := "", ""
	if len(expanded.Container.Env) > 0 {
		envFile = filepath.Join(cfg.Paths.ModelsDir, slug+".container.env")
		writef("\n=== container env file (%s) ===\n%s", envFile, ContainerEnvFile(expanded.Container.Env))
	}
	if len(expanded.Container.PreServe) > 0 {
		hookFile = filepath.Join(cfg.Paths.ModelsDir, slug+".pre-serve.sh")
		writef("\n=== pre_serve script (%s → %s) ===\n%s", hookFile, PreServeMount, PreServeScript(expanded.Container.PreServe))
	}
	return expanded, BuildContainerLaunch(expanded.Container, defaultEntrypoint, envFile, hookFile)
}
