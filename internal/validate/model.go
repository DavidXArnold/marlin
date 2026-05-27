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
	return issues
}

func checkModelID(m *config.ModelConfig) []Issue {
	if m.Model.ID == "" {
		return []Issue{{Level: LevelError, Message: "model.id is required"}}
	}
	return nil
}

func checkGPUMemory(m *config.ModelConfig) []Issue {
	if m.Serve.GPUMemoryUtilization <= 0 {
		return []Issue{{Level: LevelError, Message: "serve.gpu_memory_utilization must be set"}}
	}
	if m.Serve.GPUMemoryUtilization > 0.95 {
		return []Issue{{Level: LevelWarn, Message: fmt.Sprintf("serve.gpu_memory_utilization %.3f is very high (>0.95)", m.Serve.GPUMemoryUtilization)}}
	}
	return nil
}

func checkServedModelName(m *config.ModelConfig, requiredAlias string) []Issue {
	for _, name := range m.Serve.ServedModelName {
		if name == requiredAlias {
			return nil
		}
	}
	return []Issue{{Level: LevelWarn, Message: fmt.Sprintf("serve.served_model_name should include %q alias for client compatibility", requiredAlias)}}
}

// knownQuantizations maps model ID substrings to their expected quantization flag.
var knownQuantizations = map[string]string{
	"-AWQ":  "awq_marlin",
	"-GPTQ": "gptq",
	"-FP8":  "fp8",
}

func checkQuantization(m *config.ModelConfig) []Issue {
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
	"Qwen": "hermes",
	"Llama": "llama3_json",
	"gpt-oss": "openai",
}

func checkToolCallParser(m *config.ModelConfig) []Issue {
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
