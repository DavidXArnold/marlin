package render

import (
	"strings"
	"testing"

	"github.com/DavidXArnold/marlin/internal/config"
)

func baseConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8000
	cfg.Paths.ModelsDir = "/etc/marlin/models"
	cfg.Paths.NIMCache = "/opt/nim/cache"
	return cfg
}

func TestInspectVLLMIdentity(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "meta-llama/Llama-3-8b"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"
	m.Serve.GPUMemoryUtilization = 0.9

	out := Inspect(m, baseConfig(), "test-slug", "")

	if !strings.Contains(out, "meta-llama/Llama-3-8b") {
		t.Errorf("expected model id in output, got:\n%s", out)
	}
	if !strings.Contains(out, "vllm") {
		t.Errorf("expected type 'vllm' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "=== serve flags ===") {
		t.Errorf("expected serve flags section, got:\n%s", out)
	}
	if !strings.Contains(out, "0.900") {
		t.Errorf("expected gpu_memory_utilization, got:\n%s", out)
	}
}

func TestInspectVLLMEnvSection(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "mistral/Mistral-7B"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"

	out := Inspect(m, baseConfig(), "test-slug", "")

	if !strings.Contains(out, "=== env file") {
		t.Errorf("expected env file section, got:\n%s", out)
	}
	if !strings.Contains(out, "=== systemd ExecStart ===") {
		t.Errorf("expected systemd ExecStart section, got:\n%s", out)
	}
	if !strings.Contains(out, "vllm serve") {
		t.Errorf("expected vllm serve command, got:\n%s", out)
	}
}

func TestInspectVLLMServeFlags(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "test/model"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "active"
	m.Serve.GPUMemoryUtilization = 0.85
	m.Serve.ServedModelName = []string{"alias1", "alias2"}
	m.Serve.Quantization = "awq"
	m.Serve.MaxModelLen = 4096
	m.Serve.ExtraFlags = []string{"--disable-log-requests"}

	out := Inspect(m, baseConfig(), "test-slug", "")

	for _, want := range []string{"0.850", "alias1, alias2", "awq", "4096", "--disable-log-requests"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestInspectVLLMHFTokenPlaceholder(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "meta-llama/Llama-3.1-8B-Instruct"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"

	// With a token: placeholder shown.
	out := Inspect(m, baseConfig(), "llama-3.1-8b", "hf_secret")
	if !strings.Contains(out, "HF_TOKEN=*** (from secrets)") {
		t.Errorf("expected HF_TOKEN placeholder, got:\n%s", out)
	}

	// Without a token: secrets placeholder must not appear (but ExecStart env passthrough is fine).
	out = Inspect(m, baseConfig(), "llama-3.1-8b", "")
	if strings.Contains(out, "HF_TOKEN=*** (from secrets)") {
		t.Errorf("expected no HF_TOKEN secrets placeholder, got:\n%s", out)
	}
}

func TestInspectVLLMSlugInEnvPath(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "test/model"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"

	out := Inspect(m, baseConfig(), "my-actual-slug", "")
	if !strings.Contains(out, "my-actual-slug.env") {
		t.Errorf("expected slug in env file path, got:\n%s", out)
	}
	if strings.Contains(out, "/slug.env") {
		t.Errorf("expected literal 'slug' to be replaced, got:\n%s", out)
	}
}

func TestInspectVLLMTrustRemoteCode(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "some/model"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"
	m.Serve.TrustRemoteCode = true

	out := Inspect(m, baseConfig(), "some-model", "")
	if !strings.Contains(out, "trust_remote_code") {
		t.Errorf("expected trust_remote_code in output, got:\n%s", out)
	}
}

func TestInspectVLLMContainerizedExecStart(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "nvidia/Qwen3-32B-NVFP4"
	m.Model.Image = "nvcr.io/nvidia/vllm:26.05.post1-py3"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"

	cfg := baseConfig()
	// Default VLLMMode is "container" (empty string falls through to container mode).
	out := Inspect(m, cfg, "qwen3-32b-nvfp4", "")
	if !strings.Contains(out, "docker run") && !strings.Contains(out, "podman run") && !strings.Contains(out, "nerdctl run") {
		t.Errorf("expected container run command in ExecStart, got:\n%s", out)
	}
	if !strings.Contains(out, "${VLLM_IMAGE}") {
		t.Errorf("expected VLLM_IMAGE variable in ExecStart, got:\n%s", out)
	}
}

func TestInspectVLLMBinaryModeExecStart(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "Qwen/Qwen2.5-72B"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"

	cfg := baseConfig()
	cfg.Service.VLLMMode = "binary"
	out := Inspect(m, cfg, "qwen25-72b", "")
	if !strings.Contains(out, "vllm serve") {
		t.Errorf("expected vllm serve in binary ExecStart, got:\n%s", out)
	}
	if strings.Contains(out, "docker run") {
		t.Errorf("expected no docker run in binary mode, got:\n%s", out)
	}
}

// When VLLMMode is "container" and the model has no explicit image, inspect should
// show the global VLLMImage in the env section and the containerized ExecStart.
func TestInspectVLLMContainerModeNoModelImage(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "nvidia/GLM-5.2-NVFP4"
	m.Model.Type = config.ProviderVLLM
	m.Model.Status = "untested"
	// no m.Model.Image set

	cfg := baseConfig() // VLLMMode = "container" by default
	cfg.Service.VLLMImage = "nvcr.io/nvidia/vllm:26.05.post1-py3"

	out := Inspect(m, cfg, "glm-5.2-nvfp4", "")

	// Env section must include the global image so VLLM_IMAGE is set in the unit.
	if !strings.Contains(out, "VLLM_IMAGE=nvcr.io/nvidia/vllm:26.05.post1-py3") {
		t.Errorf("expected VLLM_IMAGE in env section, got:\n%s", out)
	}
	// ExecStart must be the containerized form.
	if strings.Contains(out, "exec vllm serve") {
		t.Errorf("expected containerized ExecStart, got binary form:\n%s", out)
	}
	if !strings.Contains(out, "${VLLM_IMAGE}") {
		t.Errorf("expected VLLM_IMAGE variable in containerized ExecStart, got:\n%s", out)
	}
}

func TestInspectNIMIdentity(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.ID = "nim/llama3-8b"
	m.Model.Image = "nvcr.io/nim/meta/llama3-8b-instruct:1.0.0"
	m.Model.Type = config.ProviderNIM
	m.Model.Status = "untested"

	out := Inspect(m, baseConfig(), "test-slug", "")

	if !strings.Contains(out, "nim") {
		t.Errorf("expected type 'nim' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "nvcr.io/nim/meta/llama3-8b-instruct:1.0.0") {
		t.Errorf("expected image in output, got:\n%s", out)
	}
	if !strings.Contains(out, "=== container config ===") {
		t.Errorf("expected container config section, got:\n%s", out)
	}
	if !strings.Contains(out, "=== equivalent docker run ===") {
		t.Errorf("expected docker run section, got:\n%s", out)
	}
}

func TestInspectNIMVolumesAndEnv(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.Image = "nvcr.io/nim/test:latest"
	m.Model.Type = config.ProviderNIM
	m.Model.Status = "untested"
	m.Serve.ExtraVolumes = []string{"/data/models:/models"}
	m.Serve.ExtraEnv = []string{"EXTRA_VAR=val"}

	out := Inspect(m, baseConfig(), "test-slug", "")

	if !strings.Contains(out, "/data/models:/models") {
		t.Errorf("expected extra volume, got:\n%s", out)
	}
	if !strings.Contains(out, "EXTRA_VAR=val") {
		t.Errorf("expected extra env, got:\n%s", out)
	}
	if !strings.Contains(out, "NGC_API_KEY") {
		t.Errorf("expected NGC_API_KEY placeholder, got:\n%s", out)
	}
}

func TestInspectNIMDockerRunPort(t *testing.T) {
	m := &config.ModelConfig{}
	m.Model.Image = "nvcr.io/nim/test:latest"
	m.Model.Type = config.ProviderNIM
	m.Model.Status = "untested"

	cfg := baseConfig()
	cfg.Server.Port = 9000

	out := Inspect(m, cfg, "test-slug", "")

	if !strings.Contains(out, "9000:8000") {
		t.Errorf("expected port mapping 9000:8000, got:\n%s", out)
	}
}
