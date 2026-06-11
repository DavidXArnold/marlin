package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleModelTOML = `
[model]
id       = "Qwen/Qwen2.5-72B-Instruct-AWQ"
registry = "huggingface"
status   = "working"
notes    = "Proven on GN100"

[serve]
quantization            = "awq_marlin"
tool_call_parser        = "hermes"
served_model_name       = ["gn100", "qwen25-72b"]
gpu_memory_utilization  = 0.824
max_model_len           = 32768
extra_flags             = ["--enable-auto-tool-choice", "--safetensors-load-strategy=prefetch"]
`

func TestLoadModel(t *testing.T) {
	path := writeTempModelFile(t, "qwen25.toml", sampleModelTOML)

	m, err := LoadModel(path)
	require.NoError(t, err)

	assert.Equal(t, "Qwen/Qwen2.5-72B-Instruct-AWQ", m.Model.ID)
	assert.Equal(t, "huggingface", m.Model.Registry)
	assert.Equal(t, StatusWorking, m.Model.Status)
	assert.Equal(t, "Proven on GN100", m.Model.Notes)

	assert.Equal(t, "awq_marlin", m.Serve.Quantization)
	assert.Equal(t, "hermes", m.Serve.ToolCallParser)
	assert.Equal(t, []string{"gn100", "qwen25-72b"}, m.Serve.ServedModelName)
	assert.InDelta(t, 0.824, m.Serve.GPUMemoryUtilization, 0.001)
	assert.Equal(t, 32768, m.Serve.MaxModelLen)
	assert.Contains(t, m.Serve.ExtraFlags, "--enable-auto-tool-choice")
}

func TestLoadModelMissing(t *testing.T) {
	_, err := LoadModel("/nonexistent/model.toml")
	assert.Error(t, err)
}

func TestLoadModelInvalidTOML(t *testing.T) {
	path := writeTempModelFile(t, "bad.toml", "not valid %%")
	_, err := LoadModel(path)
	assert.Error(t, err)
}

func TestSaveAndReloadModel(t *testing.T) {
	original := &ModelConfig{
		Model: ModelMeta{
			ID:       "meta-llama/Llama-3.1-8B-Instruct",
			Registry: "huggingface",
			Status:   StatusUntested,
		},
		Serve: ServeConfig{
			ToolCallParser:       "llama3_json",
			ServedModelName:      []string{"gn100", "llama31-8b"},
			GPUMemoryUtilization: 0.90,
			MaxModelLen:          131072,
		},
	}

	path := filepath.Join(t.TempDir(), "llama.toml")
	require.NoError(t, SaveModel(path, original))

	loaded, err := LoadModel(path)
	require.NoError(t, err)

	assert.Equal(t, original.Model.ID, loaded.Model.ID)
	assert.Equal(t, original.Serve.ToolCallParser, loaded.Serve.ToolCallParser)
	assert.InDelta(t, original.Serve.GPUMemoryUtilization, loaded.Serve.GPUMemoryUtilization, 0.001)
}

func TestSaveModelCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "model.toml")
	require.NoError(t, SaveModel(path, &ModelConfig{}))
	_, err := os.Stat(path)
	assert.NoError(t, err)
}

func TestSaveModelCreateError(t *testing.T) {
	// Put a regular file where the parent dir should be — MkdirAll fails.
	base := t.TempDir()
	blocker := filepath.Join(base, "notadir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	path := filepath.Join(blocker, "model.toml")
	err := SaveModel(path, &ModelConfig{})
	assert.Error(t, err)
}

func TestListModels(t *testing.T) {
	dir := t.TempDir()

	models := map[string]string{
		"qwen25.toml": sampleModelTOML,
		"llama.toml": `
[model]
id       = "meta-llama/Llama-3.1-8B-Instruct"
registry = "huggingface"
status   = "working"

[serve]
gpu_memory_utilization = 0.90
served_model_name = ["gn100"]
`,
	}

	for name, content := range models {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
	}

	// non-toml file should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignored.env"), []byte("FOO=bar"), 0644))

	loaded, names, err := ListModels(dir)
	require.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Len(t, names, 2)
}

func TestListModelsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	loaded, names, err := ListModels(dir)
	require.NoError(t, err)
	assert.Empty(t, loaded)
	assert.Empty(t, names)
}

func TestListModelsMissingDir(t *testing.T) {
	cfgs, names, err := ListModels("/nonexistent/dir")
	assert.NoError(t, err)
	assert.Empty(t, cfgs)
	assert.Empty(t, names)
}

func TestListModelsFromDirsDedup(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "llama.toml"), []byte("[model]\nid=\"a\"\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir1, "qwen.toml"), []byte("[model]\nid=\"b\"\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "llama.toml"), []byte("[model]\nid=\"c\"\n"), 0644)) // duplicate
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "nim.toml"), []byte("[model]\nid=\"d\"\n"), 0644))

	cfgs, names, err := ListModelsFromDirs(dir1, dir2)
	require.NoError(t, err)
	assert.Len(t, names, 3) // llama (dir1 wins), qwen, nim
	assert.Contains(t, names, "llama")
	assert.Contains(t, names, "qwen")
	assert.Contains(t, names, "nim")
	// dir1's llama should win (id="a")
	for i, n := range names {
		if n == "llama" {
			assert.Equal(t, "a", cfgs[i].Model.ID)
		}
	}
}

func TestListModelsFromDirsMissingDir(t *testing.T) {
	cfgs, names, err := ListModelsFromDirs("/nonexistent", t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, cfgs)
	assert.Empty(t, names)
}

func TestFindModelPath(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "mymodel.toml"), []byte("[model]\n"), 0644))

	path, err := FindModelPath("mymodel", dir1, dir2)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir2, "mymodel.toml"), path)
}

func TestFindModelPathNotFound(t *testing.T) {
	_, err := FindModelPath("ghost", t.TempDir(), t.TempDir())
	assert.Error(t, err)
}

func TestModelConfigToBytes(t *testing.T) {
	m := &ModelConfig{
		Model: ModelMeta{Type: ProviderVLLM, ID: "meta/llama-3.1-8b", Status: StatusUntested},
		Serve: ServeConfig{GPUMemoryUtilization: 0.9},
	}
	b, err := ModelConfigToBytes(m)
	require.NoError(t, err)
	assert.Contains(t, string(b), "meta/llama-3.1-8b")
}

func writeTempModelFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
