package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

// fakeHFCache lays out a Hugging Face cache with repo's main ref pointing at rev.
func fakeHFCache(t *testing.T, repo, rev string) string {
	t.Helper()
	cache := t.TempDir()
	repoDir := filepath.Join(cache, "hub", "models--"+strings.ReplaceAll(repo, "/", "--"))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "refs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "snapshots", rev), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "refs", "main"), []byte(rev+"\n"), 0o644))
	return cache
}

func TestExpandPlaceholders(t *testing.T) {
	cache := fakeHFCache(t, "org/model", "abc123")
	x := ExpandContext{Home: "/home/u", HFCache: cache}

	got, err := Expand("{home}/models:{home}/models:ro", x)
	require.NoError(t, err)
	assert.Equal(t, "/home/u/models:/home/u/models:ro", got)

	got, err = Expand("{hf_cache}/hub", x)
	require.NoError(t, err)
	assert.Equal(t, cache+"/hub", got)

	got, err = Expand("VLLM_PLE_MMAP_DIR={hf_snapshot:org/model}/ple-table", x)
	require.NoError(t, err)
	assert.Equal(t, "VLLM_PLE_MMAP_DIR="+filepath.Join(cache, "hub", "models--org--model", "snapshots", "abc123")+"/ple-table", got)
}

func TestExpandLeavesJSONAlone(t *testing.T) {
	in := `{"method":"mtp","num_speculative_tokens":3}`
	got, err := Expand(in, ExpandContext{Home: "/h"})
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestExpandMissingSnapshot(t *testing.T) {
	_, err := Expand("{hf_snapshot:org/absent}", ExpandContext{HFCache: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hf download org/absent")
}

func TestExpandSnapshotDirMissing(t *testing.T) {
	cache := fakeHFCache(t, "org/model", "abc123")
	require.NoError(t, os.RemoveAll(filepath.Join(cache, "hub", "models--org--model", "snapshots", "abc123")))
	_, err := Expand("{hf_snapshot:org/model}", ExpandContext{HFCache: cache})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is missing")
}

func TestExpandModel(t *testing.T) {
	m := &config.ModelConfig{
		Serve: config.ServeConfig{ExtraFlags: []string{"--chat-template", "{home}/t.jinja"}},
		Container: config.ContainerConfig{
			Env:      []string{"A={home}"},
			Volumes:  []string{"{home}:{home}:ro"},
			Args:     []string{"--shm-size=16g"},
			PreServe: []string{"{home}/run.sh"},
			ModelArg: "{home}/model",
		},
	}
	out, err := ExpandModel(m, ExpandContext{Home: "/h"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--chat-template", "/h/t.jinja"}, out.Serve.ExtraFlags)
	assert.Equal(t, []string{"A=/h"}, out.Container.Env)
	assert.Equal(t, []string{"/h:/h:ro"}, out.Container.Volumes)
	assert.Equal(t, []string{"/h/run.sh"}, out.Container.PreServe)
	assert.Equal(t, "/h/model", out.Container.ModelArg)
	// The input is not mutated.
	assert.Equal(t, "{home}/model", m.Container.ModelArg)
	assert.Equal(t, "{home}/t.jinja", m.Serve.ExtraFlags[1])
}

func TestExpandModelError(t *testing.T) {
	m := &config.ModelConfig{Container: config.ContainerConfig{ModelArg: "{hf_snapshot:x/y}", Volumes: []string{"{home}:/h"}}}
	out, err := ExpandModel(m, ExpandContext{Home: "/home/u", HFCache: t.TempDir()})
	require.Error(t, err)
	// Resolvable placeholders are still expanded for previews.
	assert.Equal(t, []string{"/home/u:/h"}, out.Container.Volumes)
}

func TestNewExpandContextUsesSudoUser(t *testing.T) {
	orig := lookupUserHomeFunc
	t.Cleanup(func() { lookupUserHomeFunc = orig })
	lookupUserHomeFunc = func(string) (string, error) { return "/home/alice", nil }
	t.Setenv("SUDO_USER", "alice")
	cfg := config.Defaults()
	cfg.Paths.HFCache = ""
	x := NewExpandContext(cfg)
	assert.Equal(t, "/home/alice", x.Home)
	assert.Equal(t, "/home/alice/.cache/huggingface", x.HFCache)
}

func TestContainerEnvFileAndPreServeScript(t *testing.T) {
	assert.Equal(t, "A=1\nB=x y\n", ContainerEnvFile([]string{"A=1", "B=x y"}))

	s := PreServeScript([]string{"/mods/a.sh", "echo ok"})
	assert.Contains(t, s, "set -euo pipefail\n/mods/a.sh\necho ok\nexec \"$@\"\n")
}

func TestBuildContainerLaunch(t *testing.T) {
	c := config.ContainerConfig{Volumes: []string{"/a:/a:ro"}, Args: []string{"--shm-size=16g"}}

	l := BuildContainerLaunch(c, "vllm", "/m/x.container.env", "")
	assert.Equal(t, []string{"--env-file", "/m/x.container.env", "-v", "/a:/a:ro", "--shm-size=16g"}, l.Args)
	assert.Empty(t, l.Entrypoint)
	assert.Empty(t, l.PreArgs)

	// pre_serve swaps the entrypoint for bash running the hook, which then execs the server.
	l = BuildContainerLaunch(c, "vllm", "", "/m/x.pre-serve.sh")
	assert.Contains(t, l.Args, "/m/x.pre-serve.sh:"+PreServeMount+":ro")
	assert.Equal(t, "/bin/bash", l.Entrypoint)
	assert.Equal(t, []string{PreServeMount, "vllm"}, l.PreArgs)

	c.Entrypoint = "/opt/venv/bin/vllm"
	l = BuildContainerLaunch(c, "vllm", "", "/m/x.pre-serve.sh")
	assert.Equal(t, []string{PreServeMount, "/opt/venv/bin/vllm"}, l.PreArgs)

	l = BuildContainerLaunch(c, "vllm", "", "")
	assert.Equal(t, "/opt/venv/bin/vllm", l.Entrypoint)
}

func TestLaunchEnv(t *testing.T) {
	assert.Empty(t, LaunchEnv("VLLM", ContainerLaunch{}))

	out := LaunchEnv("LLAMA", ContainerLaunch{Args: []string{"-v", "/a:/a"}, Entrypoint: "/bin/bash", PreArgs: []string{"/marlin/pre-serve.sh", "/app/llama-server"}}, "-v", "/g:/g:ro")
	assert.Contains(t, out, "LLAMA_CONTAINER_ARGS=-v /g:/g:ro -v /a:/a\n")
	assert.Contains(t, out, "LLAMA_ENTRYPOINT=/bin/bash\n")
	assert.Contains(t, out, "LLAMA_PRE_ARGS=/marlin/pre-serve.sh /app/llama-server\n")
}

func TestEnvModelArgOverridesID(t *testing.T) {
	m := &config.ModelConfig{
		Model:     config.ModelMeta{ID: "org/model"},
		Container: config.ContainerConfig{ModelArg: "/workspace/model"},
	}
	assert.Contains(t, Env(m, ""), "VLLM_MODEL=/workspace/model\n")
}

func TestInspectShowsContainerTable(t *testing.T) {
	cfg := config.Defaults()
	cfg.Paths.ModelsDir = "/etc/marlin/models"
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderVLLM, ID: "org/model"},
		Container: config.ContainerConfig{
			Env:      []string{"FOO=bar baz"},
			PreServe: []string{"/mods/run.sh"},
		},
	}
	out := Inspect(m, cfg, "fast", "")
	assert.Contains(t, out, "=== container env file (/etc/marlin/models/fast.container.env) ===\nFOO=bar baz\n")
	assert.Contains(t, out, "/mods/run.sh\nexec \"$@\"")
	assert.Contains(t, out, "VLLM_CONTAINER_ARGS=--env-file /etc/marlin/models/fast.container.env -v /etc/marlin/models/fast.pre-serve.sh:/marlin/pre-serve.sh:ro")
	assert.Contains(t, out, "VLLM_ENTRYPOINT=/bin/bash")
	assert.Contains(t, out, "VLLM_PRE_ARGS=/marlin/pre-serve.sh vllm")
}

func TestInspectContainerBinaryModeAndErrors(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.VLLMMode = "binary"
	m := &config.ModelConfig{
		Model:     config.ModelMeta{Type: config.ProviderVLLM, ID: "org/model"},
		Container: config.ContainerConfig{Args: []string{"--shm-size=1g"}},
	}
	assert.Contains(t, Inspect(m, cfg, "s", ""), "[container] is ignored in binary mode")

	cfg.Service.VLLMMode = "container"
	cfg.Paths.HFCache = t.TempDir()
	m.Container.ModelArg = "{hf_snapshot:org/absent}"
	assert.Contains(t, Inspect(m, cfg, "s", ""), "warning: {hf_snapshot:org/absent}")
}
