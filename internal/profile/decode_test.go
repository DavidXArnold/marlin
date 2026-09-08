package profile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cleanProfileTOML = `
[meta]
schema_version = 1
profile_version = "1.0.0"

[model]
type = "vllm"
id = "nvidia/Llama-3.1-8B-Instruct-NVFP4"
registry = "huggingface"
status = "untested"

[serve]
tool_call_parser = "llama3_json"
served_model_name = ["local", "llama-3.1-8b"]
gpu_memory_utilization = 0.9
`

const unrecognizedFieldsTOML = `
[meta]
schema_version = 3
profile_version = "1.2.0"

[model]
type = "vllm"
id = "some/moe-model"
registry = "huggingface"

[serve]
tool_call_parser = "llama3_json"
moe_backend = "triton"
attention_backend = "flashinfer"
`

const noMetaTableTOML = `
[model]
type = "vllm"
id = "some/legacy-model"
registry = "huggingface"

[serve]
tool_call_parser = "llama3_json"
`

func TestDecodeCleanProfileNoUnrecognizedFields(t *testing.T) {
	result, err := Decode([]byte(cleanProfileTOML))
	require.NoError(t, err)
	assert.Empty(t, result.UnrecognizedKeys)
	assert.Equal(t, "nvidia/Llama-3.1-8B-Instruct-NVFP4", result.Config.Model.ID)
	assert.Equal(t, 1, result.Config.Meta.SchemaVersion)
	assert.Equal(t, "1.0.0", result.Config.Meta.ProfileVersion)
}

func TestDecodeUnrecognizedFieldsReported(t *testing.T) {
	result, err := Decode([]byte(unrecognizedFieldsTOML))
	require.NoError(t, err)
	assert.Equal(t, []string{"[serve].attention_backend", "[serve].moe_backend"}, result.UnrecognizedKeys)

	// The Config itself must reflect only understood fields — unknown ones
	// are inherently absent from the typed struct, never partially applied.
	assert.Equal(t, "some/moe-model", result.Config.Model.ID)
	assert.Equal(t, "llama3_json", result.Config.Serve.ToolCallParser)
}

func TestDecodeNoMetaTableDefaultsToSchemaOne(t *testing.T) {
	result, err := Decode([]byte(noMetaTableTOML))
	require.NoError(t, err)
	assert.Empty(t, result.UnrecognizedKeys)
	assert.Equal(t, 1, result.Config.EffectiveSchemaVersion())
}

func TestDecodeInvalidTOML(t *testing.T) {
	_, err := Decode([]byte("not valid %% toml"))
	require.Error(t, err)
}

func TestFormatKey(t *testing.T) {
	cases := []struct {
		key  []string
		want string
	}{
		{[]string{"moe_backend"}, "moe_backend"},
		{[]string{"serve", "moe_backend"}, "[serve].moe_backend"},
	}
	for _, c := range cases {
		got := formatKey(c.key)
		assert.Equal(t, c.want, got)
	}
}
