package config

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeModelFile(t *testing.T, dir, slug, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, slug+".toml"), []byte(content), 0o644))
}

func TestResolveModelNoParent(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "qwen3-32b", `
[model]
id   = "nvidia/Qwen3-32B-NVFP4"
type = "vllm"

[serve]
gpu_memory_utilization = 0.90
`)
	m, err := ResolveModel("qwen3-32b", dir)
	require.NoError(t, err)
	assert.Equal(t, "nvidia/Qwen3-32B-NVFP4", m.Model.ID)
	assert.InDelta(t, 0.90, m.Serve.GPUMemoryUtilization, 0.001)
}

func TestResolveModelInheritsParent(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "nvfp4-base", `
[model]
id       = ""
type     = "vllm"
abstract = true

[serve]
quantization           = "nvfp4"
gpu_memory_utilization = 0.90
tool_call_parser       = "hermes"
`)
	writeModelFile(t, dir, "qwen3-32b", `
[model]
id      = "nvidia/Qwen3-32B-NVFP4"
extends = "nvfp4-base"

[serve]
max_model_len = 32768
`)
	m, err := ResolveModel("qwen3-32b", dir)
	require.NoError(t, err)
	assert.Equal(t, "nvidia/Qwen3-32B-NVFP4", m.Model.ID)
	assert.Equal(t, "nvfp4", m.Serve.Quantization)       // inherited
	assert.InDelta(t, 0.90, m.Serve.GPUMemoryUtilization, 0.001) // inherited
	assert.Equal(t, "hermes", m.Serve.ToolCallParser)    // inherited
	assert.Equal(t, 32768, m.Serve.MaxModelLen)           // child wins
	assert.Equal(t, ProviderVLLM, m.Model.Type)           // inherited
}

func TestResolveModelChildWins(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "base", `
[model]
type = "vllm"

[serve]
gpu_memory_utilization = 0.80
tool_call_parser       = "hermes"
`)
	writeModelFile(t, dir, "child", `
[model]
id      = "some/model"
extends = "base"

[serve]
gpu_memory_utilization = 0.95
tool_call_parser       = "llama3_json"
`)
	m, err := ResolveModel("child", dir)
	require.NoError(t, err)
	assert.InDelta(t, 0.95, m.Serve.GPUMemoryUtilization, 0.001)
	assert.Equal(t, "llama3_json", m.Serve.ToolCallParser)
}

func TestResolveModelThreeLevel(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "root", `
[model]
type = "vllm"

[serve]
gpu_memory_utilization = 0.80
quantization           = "fp8"
`)
	writeModelFile(t, dir, "mid", `
[model]
extends = "root"

[serve]
quantization = "awq_marlin"
`)
	writeModelFile(t, dir, "leaf", `
[model]
id      = "some/leaf"
extends = "mid"

[serve]
max_model_len = 4096
`)
	m, err := ResolveModel("leaf", dir)
	require.NoError(t, err)
	assert.Equal(t, "some/leaf", m.Model.ID)
	assert.Equal(t, "awq_marlin", m.Serve.Quantization) // mid wins over root
	assert.InDelta(t, 0.80, m.Serve.GPUMemoryUtilization, 0.001) // from root
	assert.Equal(t, 4096, m.Serve.MaxModelLen)          // from leaf
}

func TestResolveModelCircularError(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "a", `
[model]
extends = "b"
`)
	writeModelFile(t, dir, "b", `
[model]
extends = "a"
`)
	_, err := ResolveModel("a", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestResolveModelMissingParentError(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "child", `
[model]
extends = "nonexistent"
`)
	_, err := ResolveModel("child", dir)
	require.Error(t, err)
}

func TestResolveModelMissingSlugError(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveModel("ghost", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveModelAbstractPreserved(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "base", `
[model]
type     = "vllm"
abstract = true
`)
	writeModelFile(t, dir, "child", `
[model]
id      = "some/model"
extends = "base"
`)
	m, err := ResolveModel("child", dir)
	require.NoError(t, err)
	assert.False(t, m.Model.Abstract, "child should not inherit abstract flag")
}

func TestResolveModelArraysOverridden(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "base", `
[model]
type = "vllm"

[serve]
served_model_name = ["parent-alias"]
`)
	writeModelFile(t, dir, "child", `
[model]
id      = "m"
extends = "base"

[serve]
served_model_name = ["child-alias", "local"]
`)
	m, err := ResolveModel("child", dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"child-alias", "local"}, m.Serve.ServedModelName)
}

func TestResolveModelTrustRemoteCodeInherited(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "base", `
[model]
type = "vllm"
`)
	writeModelFile(t, dir, "child", `
[model]
id      = "some/model"
extends = "base"

[serve]
trust_remote_code = true
`)
	m, err := ResolveModel("child", dir)
	require.NoError(t, err)
	assert.True(t, m.Serve.TrustRemoteCode)
}

func TestResolveModelFallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()

	fakeFS := fstest.MapFS{
		"models/nvfp4-base.toml": &fstest.MapFile{
			Data: []byte(`
[model]
type     = "vllm"
abstract = true

[serve]
gpu_memory_utilization = 0.824
max_model_len          = 131072
`),
		},
	}

	old := BundledModels
	BundledModels = fakeFS
	t.Cleanup(func() { BundledModels = old })

	writeModelFile(t, dir, "my-model", `
[model]
id      = "nvidia/SomeModel-NVFP4"
extends = "nvfp4-base"

[serve]
tool_call_parser  = "hermes"
served_model_name = ["local"]
`)

	m, err := ResolveModel("my-model", dir)
	require.NoError(t, err)
	assert.Equal(t, "nvidia/SomeModel-NVFP4", m.Model.ID)
	assert.InDelta(t, 0.824, m.Serve.GPUMemoryUtilization, 0.001)
	assert.Equal(t, 131072, m.Serve.MaxModelLen)
	assert.Equal(t, "hermes", m.Serve.ToolCallParser)
}

func TestResolveModelArraysFallThrough(t *testing.T) {
	dir := t.TempDir()
	writeModelFile(t, dir, "base", `
[model]
type = "vllm"

[serve]
served_model_name = ["from-parent"]
extra_flags       = ["--disable-log-requests"]
`)
	writeModelFile(t, dir, "child", `
[model]
id      = "m"
extends = "base"
`)
	m, err := ResolveModel("child", dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"from-parent"}, m.Serve.ServedModelName)
	assert.Equal(t, []string{"--disable-log-requests"}, m.Serve.ExtraFlags)
}
