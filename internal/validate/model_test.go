package validate

import (
	"testing"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validModel() *config.ModelConfig {
	return &config.ModelConfig{
		Model: config.ModelMeta{
			ID:       "Qwen/Qwen2.5-72B-Instruct-AWQ",
			Registry: "huggingface",
			Status:   config.StatusWorking,
		},
		Serve: config.ServeConfig{
			Quantization:         "awq_marlin",
			ToolCallParser:       "hermes",
			ServedModelName:      []string{"gn100", "qwen25-72b"},
			GPUMemoryUtilization: 0.824,
			MaxModelLen:          32768,
		},
	}
}

func TestModelValidNoIssues(t *testing.T) {
	issues := Model(validModel(), "gn100")
	assert.Empty(t, issues)
}

func TestModelMissingID(t *testing.T) {
	m := validModel()
	m.Model.ID = ""
	issues := Model(m, "gn100")
	assert.True(t, hasError(issues, "model.id is required"))
}

func TestModelNIMMissingImage(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderNIM},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.9},
	}
	issues := Model(m, "gn100")
	assert.True(t, hasError(issues, "model.image is required"))
}

func validNIMModel() *config.ModelConfig {
	return &config.ModelConfig{
		Model: config.ModelMeta{
			Type:  config.ProviderNIM,
			Image: "nvcr.io/nim/meta/llama:latest",
		},
	}
}

func TestModelNIMValidNoIssues(t *testing.T) {
	// A minimal NIM config — no gpu_memory, served_model_name, id — should have no errors.
	issues := Model(validNIMModel(), "gn100")
	for _, iss := range issues {
		assert.NotEqual(t, LevelError, iss.Level, "unexpected error: %s", iss.Message)
	}
}

func TestModelNIMSkipsGPUMemory(t *testing.T) {
	m := validNIMModel()
	m.Serve.GPUMemoryUtilization = 0 // would error for vllm
	issues := Model(m, "gn100")
	assert.False(t, hasError(issues, "gpu_memory_utilization"), "NIM should skip GPU memory check")
}

func TestModelNIMSkipsServedModelName(t *testing.T) {
	m := validNIMModel()
	m.Serve.ServedModelName = nil // would warn for vllm
	issues := Model(m, "gn100")
	assert.False(t, hasWarn(issues, "served_model_name"), "NIM should skip served_model_name check")
}

func TestModelNIMSkipsQuantization(t *testing.T) {
	m := validNIMModel()
	m.Model.Image = "nvcr.io/nim/meta/llama-AWQ:latest"
	m.Serve.Quantization = "wrong"
	issues := Model(m, "gn100")
	assert.False(t, hasWarn(issues, "quantization"), "NIM should skip quantization check")
}

func TestModelNIMSkipsToolCallParser(t *testing.T) {
	m := validNIMModel()
	m.Serve.ToolCallParser = "openai"
	issues := Model(m, "gn100")
	assert.False(t, hasWarn(issues, "parser"), "NIM should skip tool-call parser check")
}

func TestModelMissingGPUMemory(t *testing.T) {
	m := validModel()
	m.Serve.GPUMemoryUtilization = 0
	issues := Model(m, "gn100")
	assert.True(t, hasError(issues, "gpu_memory_utilization must be set"))
}

func TestModelHighGPUMemoryWarns(t *testing.T) {
	m := validModel()
	m.Serve.GPUMemoryUtilization = 0.97
	issues := Model(m, "gn100")
	assert.True(t, hasLevel(issues, LevelWarn))
}

func TestModelMissingAliasWarns(t *testing.T) {
	m := validModel()
	m.Serve.ServedModelName = []string{"qwen25-72b"}
	issues := Model(m, "gn100")
	assert.True(t, hasWarn(issues, "gn100"))
}

func TestModelAWQWithWrongQuantWarns(t *testing.T) {
	m := validModel()
	m.Serve.Quantization = "awq"
	issues := Model(m, "gn100")
	assert.True(t, hasLevel(issues, LevelWarn))
}

func TestModelWrongParserForQwenWarns(t *testing.T) {
	m := validModel()
	m.Serve.ToolCallParser = "openai"
	issues := Model(m, "gn100")
	assert.True(t, hasWarn(issues, "hermes"))
}

func TestModelNoParserNoIssue(t *testing.T) {
	m := validModel()
	m.Serve.ToolCallParser = ""
	issues := Model(m, "gn100")
	assert.Empty(t, issues)
}

func TestIssueString(t *testing.T) {
	iss := Issue{Level: LevelError, Message: "model.id is required"}
	assert.Equal(t, "[error] model.id is required", iss.String())

	iss2 := Issue{Level: LevelWarn, Message: "gpu_memory_utilization is high"}
	assert.Equal(t, "[warn] gpu_memory_utilization is high", iss2.String())
}

func hasError(issues []Issue, substr string) bool {
	return hasLevelSubstr(issues, LevelError, substr)
}

func hasWarn(issues []Issue, substr string) bool {
	return hasLevelSubstr(issues, LevelWarn, substr)
}

func hasLevel(issues []Issue, level Level) bool {
	for _, i := range issues {
		if i.Level == level {
			return true
		}
	}
	return false
}

func hasLevelSubstr(issues []Issue, level Level, substr string) bool {
	for _, i := range issues {
		if i.Level == level {
			if substr == "" || contains(i.Message, substr) {
				return true
			}
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestModelLlamaCppHFRefValid(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderLlamaCpp, ID: "org/Model-GGUF:Q4_K_M"},
		Serve: config.ServeConfig{ServedModelName: []string{"local"}, ToolCallParser: "anything"},
	}
	assert.Empty(t, Model(m, "local"))
}

func TestModelLlamaCppGGUFPathValid(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderLlamaCpp},
		Serve: config.ServeConfig{GGUFPath: "/models/x.gguf", ServedModelName: []string{"local"}},
	}
	assert.Empty(t, Model(m, "local"))
}

func TestModelLlamaCppNoSource(t *testing.T) {
	m := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderLlamaCpp}}
	issues := Model(m, "local")
	require.NotEmpty(t, issues)
	assert.Equal(t, LevelError, issues[0].Level)
	assert.Contains(t, issues[0].Message, "gguf_path")
}

func TestModelSchemaTooNew(t *testing.T) {
	m := validModel()
	m.Meta.SchemaVersion = config.SupportedSchemaVersion + 1
	issues := Model(m, "gn100")
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[len(issues)-1].Message, "marlin upgrade")
}

func TestModelContainerValid(t *testing.T) {
	m := validModel()
	m.Container = config.ContainerConfig{
		Env:     []string{"A=x y", "B="},
		Volumes: []string{"{home}/m:{home}/m:ro"},
		Args:    []string{"--shm-size=16g"},
	}
	assert.Empty(t, Model(m, "gn100"))
}

func TestModelContainerInvalid(t *testing.T) {
	m := validModel()
	m.Container = config.ContainerConfig{
		Env:        []string{"NOEQUALS", "=v", "BAD KEY=v"},
		Volumes:    []string{"/a b:/c", "/nocolon"},
		Args:       []string{"--shm-size 16g"},
		Entrypoint: "bash -c",
	}
	var errs int
	for _, iss := range Model(m, "gn100") {
		if iss.Level == LevelError {
			errs++
		}
	}
	assert.Equal(t, 7, errs)
}

func TestModelContainerWarnings(t *testing.T) {
	nim := &config.ModelConfig{
		Model:     config.ModelMeta{Type: config.ProviderNIM, Image: "nvcr.io/nim/x:1"},
		Container: config.ContainerConfig{Args: []string{"--shm-size=1g"}},
	}
	issues := Model(nim, "gn100")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "ignored for nim")

	ll := &config.ModelConfig{
		Model:     config.ModelMeta{Type: config.ProviderLlamaCpp, ID: "org/m-GGUF"},
		Serve:     config.ServeConfig{ServedModelName: []string{"gn100"}},
		Container: config.ContainerConfig{ModelArg: "/x"},
	}
	issues = Model(ll, "gn100")
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "vLLM-only")
}
