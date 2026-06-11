package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

func testAdhocRunner(t *testing.T, d *stubDocker) (*AdhocRunner, string) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0755))

	cfg := config.Defaults()
	cfg.Paths.ModelsDir = modelsDir
	cfg.Paths.NIMCache = filepath.Join(dir, "nim-cache")

	a := newAdhocRunnerWithClient(cfg, d)
	a.prepareCache = func(_ io.Writer, _ string) error { return nil }
	a.refreshPerms = func(_ string) error { return nil }
	return a, dir
}

func writeAdhocVLLMModel(t *testing.T, dir, slug, modelID string) {
	t.Helper()
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderVLLM, ID: modelID},
		Serve: config.ServeConfig{GPUMemoryUtilization: 0.9},
	}
	require.NoError(t, config.SaveModel(filepath.Join(dir, slug+".toml"), m))
}

func writeAdhocNIMModel(t *testing.T, dir, slug, image string) {
	t.Helper()
	m := &config.ModelConfig{
		Model: config.ModelMeta{Type: config.ProviderNIM, Image: image},
	}
	require.NoError(t, config.SaveModel(filepath.Join(dir, slug+".toml"), m))
}

// --- Start ---

func TestAdhocStartVLLMSuccess(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		createResp: container.CreateResponse{ID: "aabbccdd"},
	}
	runner, dir := testAdhocRunner(t, d)
	writeAdhocVLLMModel(t, runner.cfg.Paths.ModelsDir, "llama-8b", "meta-llama/Llama-3-8B")

	id, err := runner.Start(context.Background(), "llama-8b")
	require.NoError(t, err)
	assert.Equal(t, "aabbccdd", id)
	_ = dir
}

func TestAdhocStartNIMSuccess(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		createResp: container.CreateResponse{ID: "nim999"},
	}
	runner, _ := testAdhocRunner(t, d)
	writeAdhocNIMModel(t, runner.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama:latest")

	id, err := runner.Start(context.Background(), "llama-nim")
	require.NoError(t, err)
	assert.Equal(t, "nim999", id)
}

func TestAdhocStartModelNotFound(t *testing.T) {
	d := &stubDocker{}
	runner, _ := testAdhocRunner(t, d)

	_, err := runner.Start(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading model")
}

func TestAdhocStartNIMNoImage(t *testing.T) {
	d := &stubDocker{}
	runner, _ := testAdhocRunner(t, d)
	m := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderNIM, Image: ""}}
	require.NoError(t, config.SaveModel(filepath.Join(runner.cfg.Paths.ModelsDir, "noimage.toml"), m))

	_, err := runner.Start(context.Background(), "noimage")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no image set")
}

func TestAdhocStartPullFails(t *testing.T) {
	d := &stubDocker{pullErr: fmt.Errorf("auth failed")}
	runner, _ := testAdhocRunner(t, d)
	writeAdhocVLLMModel(t, runner.cfg.Paths.ModelsDir, "llama-8b", "meta-llama/Llama-3-8B")

	_, err := runner.Start(context.Background(), "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pulling image")
}

func TestAdhocStartCreateFails(t *testing.T) {
	d := &stubDocker{pullReader: emptyReader(), createErr: fmt.Errorf("no space")}
	runner, _ := testAdhocRunner(t, d)
	writeAdhocVLLMModel(t, runner.cfg.Paths.ModelsDir, "llama-8b", "meta-llama/Llama-3-8B")

	_, err := runner.Start(context.Background(), "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating container")
}

func TestAdhocStartStartFails(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		createResp: container.CreateResponse{ID: "abc"},
		startErr:   fmt.Errorf("start failed"),
	}
	runner, _ := testAdhocRunner(t, d)
	writeAdhocVLLMModel(t, runner.cfg.Paths.ModelsDir, "llama-8b", "meta-llama/Llama-3-8B")

	_, err := runner.Start(context.Background(), "llama-8b")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "starting container")
}

func TestAdhocDetectUnmanaged(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{ID: "abc123", Image: "vllm/vllm-openai:latest", Names: []string{"/outside"}},
		},
	}
	runner, _ := testAdhocRunner(t, d)

	got, err := runner.DetectUnmanaged(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "abc123", got[0].ID)
}

// --- RunForeground ---

func TestAdhocRunForeground(t *testing.T) {
	content := "\x01\x00\x00\x00\x00\x00\x00\x05hello"
	d := &stubDocker{
		pullReader: emptyReader(),
		createResp: container.CreateResponse{ID: "fg001"},
		logsReader: io.NopCloser(strings.NewReader(content)),
	}
	runner, _ := testAdhocRunner(t, d)
	writeAdhocVLLMModel(t, runner.cfg.Paths.ModelsDir, "llama-8b", "meta-llama/Llama-3-8B")

	_ = runner.RunForeground(context.Background(), "llama-8b", io.Discard)
}

// --- List ---

func TestAdhocListEmpty(t *testing.T) {
	d := &stubDocker{}
	runner, _ := testAdhocRunner(t, d)

	items, err := runner.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestAdhocListContainers(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{
				ID:     "abc123",
				State:  "running",
				Labels: map[string]string{labelManaged: "true", labelModel: "llama-8b", labelProvider: "vllm"},
			},
		},
	}
	runner, _ := testAdhocRunner(t, d)

	items, err := runner.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "llama-8b", items[0].Slug)
	assert.Equal(t, "vllm", items[0].Provider)
	assert.Equal(t, "running", items[0].Status)
}

func TestAdhocListFails(t *testing.T) {
	d := &stubDocker{listErr: fmt.Errorf("docker down")}
	runner, _ := testAdhocRunner(t, d)

	_, err := runner.List(context.Background())
	assert.Error(t, err)
}

// --- Stop ---

func TestAdhocStopSuccess(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{ID: "abc123", Labels: map[string]string{labelManaged: "true", labelModel: "llama-8b"}},
		},
	}
	runner, _ := testAdhocRunner(t, d)

	require.NoError(t, runner.Stop(context.Background(), "llama-8b"))
}

func TestAdhocStopNotFound(t *testing.T) {
	d := &stubDocker{}
	runner, _ := testAdhocRunner(t, d)

	err := runner.Stop(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no ad-hoc container found")
}

func TestAdhocStopAllSuccess(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{ID: "c1", Labels: map[string]string{labelManaged: "true"}},
			{ID: "c2", Labels: map[string]string{labelManaged: "true"}},
		},
	}
	runner, _ := testAdhocRunner(t, d)

	require.NoError(t, runner.StopAll(context.Background()))
}

// --- NewAdhocRunner runtime ---

func TestNewAdhocRunnerCustomSocket(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "docker"
	cfg.Service.ContainerSocket = "/tmp/marlin-test-docker.sock"

	r, err := NewAdhocRunner(cfg)
	require.NoError(t, err)
	assert.NotNil(t, r)
}
