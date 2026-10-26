package validate

import (
	"fmt"
	"strings"

	"github.com/DavidXArnold/marlin/internal/config"
)

// Issue represents a single validation finding.
type Issue struct {
	Level   Level
	Message string
}

type Level string

const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
)

func (i Issue) String() string {
	return fmt.Sprintf("[%s] %s", i.Level, i.Message)
}

// Model runs all validation checks against a model config and returns any issues found.
// serverAlias is the alias the running server answers to (from global config server.alias).
func Model(m *config.ModelConfig, serverAlias string) []Issue {
	var issues []Issue
	issues = append(issues, checkModelID(m)...)
	issues = append(issues, checkGPUMemory(m)...)
	issues = append(issues, checkServedModelName(m, serverAlias)...)
	issues = append(issues, checkQuantization(m)...)
	issues = append(issues, checkToolCallParser(m)...)
	issues = append(issues, checkSchemaVersion(m)...)
	issues = append(issues, checkContainer(m)...)
	return issues
}

func checkSchemaVersion(m *config.ModelConfig) []Issue {
	if v := m.EffectiveSchemaVersion(); v > config.SupportedSchemaVersion {
		return []Issue{{Level: LevelError, Message: fmt.Sprintf(
			"profile needs schema_version %d but this marlin supports up to %d — run 'marlin upgrade'", v, config.SupportedSchemaVersion)}}
	}
	return nil
}

// checkContainer validates the [container] table. Volumes and args reach
// docker through a word-split variable, so they must not contain whitespace;
// env goes through --env-file and may.
func checkContainer(m *config.ModelConfig) []Issue {
	c := m.Container
	if c.IsZero() {
		return nil
	}
	if m.Model.Type == config.ProviderNIM || m.Model.Type == config.ProviderMesh {
		return []Issue{{Level: LevelWarn, Message: fmt.Sprintf("[container] is ignored for %s models (use serve.extra_env / serve.extra_volumes for NIM)", m.Model.Type)}}
	}
	var issues []Issue
	for _, e := range c.Env {
		if k, _, ok := strings.Cut(e, "="); !ok || k == "" || strings.ContainsAny(k, " \t") {
			issues = append(issues, Issue{Level: LevelError, Message: fmt.Sprintf("container.env entry %q must be KEY=VALUE", e)})
		}
	}
	for _, v := range c.Volumes {
		if strings.ContainsAny(v, " \t") {
			issues = append(issues, Issue{Level: LevelError, Message: fmt.Sprintf("container.volumes entry %q must not contain whitespace", v)})
		} else if !strings.Contains(v, ":") {
			issues = append(issues, Issue{Level: LevelError, Message: fmt.Sprintf("container.volumes entry %q must be /host:/container[:opts]", v)})
		}
	}
	for _, a := range c.Args {
		if strings.ContainsAny(a, " \t") {
			issues = append(issues, Issue{Level: LevelError, Message: fmt.Sprintf("container.args entry %q must not contain whitespace (write --flag=value)", a)})
		}
	}
	if strings.ContainsAny(c.Entrypoint, " \t") {
		issues = append(issues, Issue{Level: LevelError, Message: "container.entrypoint must be a single executable path"})
	}
	if c.ModelArg != "" && m.Model.Type == config.ProviderLlamaCpp {
		issues = append(issues, Issue{Level: LevelWarn, Message: "container.model_arg is vLLM-only; use serve.gguf_path for llama.cpp"})
	}
	return issues
}

func checkModelID(m *config.ModelConfig) []Issue {
	if m.Model.Type == config.ProviderNIM {
		if m.Model.Image == "" {
			return []Issue{{Level: LevelError, Message: "model.image is required for NIM models"}}
		}
		return nil
	}
	if m.Model.Type == config.ProviderLlamaCpp {
		if m.Model.ID == "" && m.Serve.GGUFPath == "" {
			return []Issue{{Level: LevelError, Message: "model.id (a Hugging Face GGUF repo) or serve.gguf_path is required for llama.cpp models"}}
		}
		return nil
	}
	if m.Model.ID == "" {
		return []Issue{{Level: LevelError, Message: "model.id is required"}}
	}
	return nil
}

func checkGPUMemory(m *config.ModelConfig) []Issue {
	// NIM containers manage GPU memory internally and llama.cpp sizes by layers
	// offloaded (ngl) — this field is not applicable to either.
	if m.Model.Type == config.ProviderNIM || m.Model.Type == config.ProviderLlamaCpp {
		return nil
	}
	if m.Serve.GPUMemoryUtilization <= 0 {
		return []Issue{{Level: LevelError, Message: "serve.gpu_memory_utilization must be set"}}
	}
	if m.Serve.GPUMemoryUtilization > 0.95 {
		return []Issue{{Level: LevelWarn, Message: fmt.Sprintf("serve.gpu_memory_utilization %.3f is very high (>0.95)", m.Serve.GPUMemoryUtilization)}}
	}
	return nil
}

func checkServedModelName(m *config.ModelConfig, requiredAlias string) []Issue {
	// NIM exposes its own model names; served_model_name is not used.
	if m.Model.Type == config.ProviderNIM {
		return nil
	}
	for _, name := range m.Serve.ServedModelName {
		if name == requiredAlias {
			return nil
		}
	}
	return []Issue{{Level: LevelWarn, Message: fmt.Sprintf("serve.served_model_name should include %q alias for client compatibility", requiredAlias)}}
}

// knownQuantizations maps model ID substrings to their expected quantization flag.
var knownQuantizations = map[string]string{
	"-AWQ":   "awq_marlin",
	"-GPTQ":  "gptq",
	"-FP8":   "fp8",
	"-NVFP4": "nvfp4",
}

func checkQuantization(m *config.ModelConfig) []Issue {
	// NIM handles quantization internally; model.id is not set for NIM.
	if m.Model.Type == config.ProviderNIM {
		return nil
	}
	idUpper := strings.ToUpper(m.Model.ID)
	for suffix, expected := range knownQuantizations {
		if strings.Contains(idUpper, suffix) {
			if m.Serve.Quantization != "" && m.Serve.Quantization != expected {
				return []Issue{{
					Level:   LevelWarn,
					Message: fmt.Sprintf("model ID suggests %s quantization but serve.quantization is %q", expected, m.Serve.Quantization),
				}}
			}
		}
	}
	return nil
}

// parserByFamily maps model family name fragments to their expected tool-call parser.
var parserByFamily = map[string]string{
	"Qwen":    "hermes",
	"Llama":   "llama3_json",
	"gpt-oss": "openai",
}

func checkToolCallParser(m *config.ModelConfig) []Issue {
	// NIM exposes its own tool-call handling; llama.cpp parses tool calls from
	// the chat template (--jinja) and has no parser flag.
	if m.Model.Type == config.ProviderNIM || m.Model.Type == config.ProviderLlamaCpp {
		return nil
	}
	if m.Serve.ToolCallParser == "" {
		return nil
	}
	for family, expected := range parserByFamily {
		if strings.Contains(m.Model.ID, family) && m.Serve.ToolCallParser != expected {
			return []Issue{{
				Level:   LevelWarn,
				Message: fmt.Sprintf("model family %q typically uses %q parser, got %q", family, expected, m.Serve.ToolCallParser),
			}}
		}
	}
	return nil
}
