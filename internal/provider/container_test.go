package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

func containerModel(pt config.ProviderType) *config.ModelConfig {
	return &config.ModelConfig{
		Meta:  config.ProfileMeta{SchemaVersion: 2},
		Model: config.ModelMeta{Type: pt, ID: "org/model"},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.5, ExtraFlags: []string{"--chat-template", "{home}/t.jinja"}},
		Container: config.ContainerConfig{
			Env:      []string{"FOO=bar baz"},
			Volumes:  []string{"{home}/models:{home}/models:ro"},
			Args:     []string{"--shm-size=16g"},
			PreServe: []string{"/mods/run.sh"},
		},
	}
}

func TestVLLMSwitchAppliesContainerTable(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	t.Setenv("SUDO_USER", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := containerModel(config.ProviderVLLM)
	m.Container.ModelArg = "/workspace/model"
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "fast.toml"), m))

	require.NoError(t, p.Switch(context.Background(), "fast"))

	dir := p.cfg.Paths.ModelsDir
	env, err := os.ReadFile(filepath.Join(dir, "fast.env"))
	require.NoError(t, err)
	s := string(env)
	assert.Contains(t, s, "VLLM_MODEL=/workspace/model\n")
	assert.Contains(t, s, "--chat-template "+home+"/t.jinja")
	assert.Contains(t, s, "VLLM_CONTAINER_ARGS=--env-file "+filepath.Join(dir, "fast.container.env")+
		" -v "+home+"/models:"+home+"/models:ro --shm-size=16g -v "+filepath.Join(dir, "fast.pre-serve.sh")+":/marlin/pre-serve.sh:ro\n")
	assert.Contains(t, s, "VLLM_ENTRYPOINT=/bin/bash\n")
	assert.Contains(t, s, "VLLM_PRE_ARGS=/marlin/pre-serve.sh vllm\n")

	ctrEnv, err := os.ReadFile(filepath.Join(dir, "fast.container.env"))
	require.NoError(t, err)
	assert.Equal(t, "FOO=bar baz\n", string(ctrEnv))
	hook, err := os.ReadFile(filepath.Join(dir, "fast.pre-serve.sh"))
	require.NoError(t, err)
	assert.Contains(t, string(hook), "/mods/run.sh\nexec \"$@\"")
}

func TestVLLMSwitchBinaryModeIgnoresContainerTable(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	p.cfg.Service.VLLMMode = "binary"
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "fast.toml"), containerModel(config.ProviderVLLM)))

	require.NoError(t, p.Switch(context.Background(), "fast"))

	env, err := os.ReadFile(filepath.Join(p.cfg.Paths.ModelsDir, "fast.env"))
	require.NoError(t, err)
	assert.NotContains(t, string(env), "VLLM_CONTAINER_ARGS")
	_, err = os.Stat(filepath.Join(p.cfg.Paths.ModelsDir, "fast.container.env"))
	assert.True(t, os.IsNotExist(err))
}

func TestVLLMSwitchUnresolvedPlaceholderFails(t *testing.T) {
	p, _ := testVLLMProvider(t, successRunner)
	p.cfg.Paths.HFCache = t.TempDir()
	m := containerModel(config.ProviderVLLM)
	m.Container.ModelArg = "{hf_snapshot:org/absent}"
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "fast.toml"), m))

	err := p.Switch(context.Background(), "fast")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hf download org/absent")
	_, statErr := os.Stat(filepath.Join(p.cfg.Paths.ModelsDir, "fast.env"))
	assert.True(t, os.IsNotExist(statErr), "nothing is written when expansion fails")
}

func TestLlamaCppSwitchAppliesContainerTable(t *testing.T) {
	p, dir := testLlamaCppProvider(t, successRunner)
	m := containerModel(config.ProviderLlamaCpp)
	m.Serve.GGUFPath = filepath.Join(dir, "w", "m.gguf")
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "g.toml"), m))

	require.NoError(t, p.Switch(context.Background(), "g"))

	env, err := os.ReadFile(filepath.Join(p.cfg.Paths.ModelsDir, "g.llamacpp.env"))
	require.NoError(t, err)
	s := string(env)
	gdir := filepath.Join(dir, "w")
	// The gguf mount comes first, then the profile's own container args.
	assert.Contains(t, s, "LLAMA_CONTAINER_ARGS=-v "+gdir+":"+gdir+":ro --env-file ")
	assert.Contains(t, s, "LLAMA_ENTRYPOINT=/bin/bash\n")
	assert.Contains(t, s, "LLAMA_PRE_ARGS=/marlin/pre-serve.sh /app/llama-server\n")
}

func TestWriteContainerFilesNone(t *testing.T) {
	envFile, hook, err := writeContainerFiles(nil, t.TempDir(), "x", config.ContainerConfig{Args: []string{"--shm-size=1g"}})
	require.NoError(t, err)
	assert.Empty(t, envFile)
	assert.Empty(t, hook)
}
