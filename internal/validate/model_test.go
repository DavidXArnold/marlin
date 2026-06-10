package validate

import (
	"testing"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/stretchr/testify/assert"
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

func TestModelNIMWithImageNoIDRequired(t *testing.T) {
	m := &config.ModelConfig{
		Model: config.ModelMeta{
			Type:  config.ProviderNIM,
			Image: "nvcr.io/nim/meta/llama:latest",
		},
		Serve: config.ServeConfig{
			GPUMemoryUtilization: 0.9,
			ServedModelName:      []string{"gn100"},
		},
	}
	issues := Model(m, "gn100")
	// No error — image satisfies the identity check for NIM
	for _, iss := range issues {
		assert.NotEqual(t, LevelError, iss.Level, "unexpected error: %s", iss.Message)
	}
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
