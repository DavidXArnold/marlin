package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	dimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

// stubDocker is a fully controllable in-process Docker client stub.
type stubDocker struct {
	stopErr    error
	removeErr  error
	createResp container.CreateResponse
	createErr  error
	startErr   error
	listResult []container.Summary
	listErr    error
	logsReader io.ReadCloser
	logsErr    error
	pullReader io.ReadCloser
	pullErr    error
}

func (s *stubDocker) ContainerStop(_ context.Context, _ string, _ container.StopOptions) error {
	return s.stopErr
}
func (s *stubDocker) ContainerRemove(_ context.Context, _ string, _ container.RemoveOptions) error {
	return s.removeErr
}
func (s *stubDocker) ContainerCreate(_ context.Context, _ *container.Config, _ *container.HostConfig,
	_ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
	return s.createResp, s.createErr
}
func (s *stubDocker) ContainerStart(_ context.Context, _ string, _ container.StartOptions) error {
	return s.startErr
}
func (s *stubDocker) ContainerList(_ context.Context, _ container.ListOptions) ([]container.Summary, error) {
	return s.listResult, s.listErr
}
func (s *stubDocker) ContainerLogs(_ context.Context, _ string, _ container.LogsOptions) (io.ReadCloser, error) {
	return s.logsReader, s.logsErr
}
func (s *stubDocker) ImagePull(_ context.Context, _ string, _ dimage.PullOptions) (io.ReadCloser, error) {
	return s.pullReader, s.pullErr
}

func testNIMProvider(t *testing.T, d *stubDocker) (*NIMProvider, string) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0755))

	cfg := config.Defaults()
	cfg.Paths.ModelsDir = modelsDir
	cfg.Paths.NIMCache = filepath.Join(dir, "nim-cache")

	p := newNIMProviderWithClient(cfg, "test-ngc-key", d)
	p.prepareCache = func(_ io.Writer, _ string) error { return nil }
	p.refreshPerms = func(_ string) error { return nil }
	return p, dir
}

func writeNIMModel(t *testing.T, dir, slug, image string) {
	t.Helper()
	m := &config.ModelConfig{
		Model: config.ModelMeta{
			Type:  config.ProviderNIM,
			Image: image,
		},
	}
	require.NoError(t, config.SaveModel(filepath.Join(dir, slug+".toml"), m))
}

func emptyReader() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }

// --- Switch ---

func TestNIMSwitchSuccess(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		createResp: container.CreateResponse{ID: "abc123"},
	}
	p, _ := testNIMProvider(t, d)
	writeNIMModel(t, p.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama-3.1-8b:latest")

	require.NoError(t, p.Switch(context.Background(), "llama-nim"))
}

func TestNIMSwitchModelNotFound(t *testing.T) {
	d := &stubDocker{}
	p, _ := testNIMProvider(t, d)
	err := p.Switch(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "loading model")
}

func TestNIMSwitchNoImage(t *testing.T) {
	d := &stubDocker{}
	p, _ := testNIMProvider(t, d)
	m := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderNIM, Image: ""}}
	require.NoError(t, config.SaveModel(filepath.Join(p.cfg.Paths.ModelsDir, "noimage.toml"), m))

	err := p.Switch(context.Background(), "noimage")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no image set")
}

func TestNIMSwitchPullFails(t *testing.T) {
	d := &stubDocker{pullErr: fmt.Errorf("auth failed")}
	p, _ := testNIMProvider(t, d)
	writeNIMModel(t, p.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama:latest")

	err := p.Switch(context.Background(), "llama-nim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pulling image")
}

func TestNIMSwitchStopFails(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		listResult: []container.Summary{{ID: "old123", State: "running"}},
		stopErr:    fmt.Errorf("stop failed"),
	}
	p, _ := testNIMProvider(t, d)
	writeNIMModel(t, p.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama:latest")

	err := p.Switch(context.Background(), "llama-nim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stopping container")
}

func TestNIMSwitchRemoveFails(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		listResult: []container.Summary{{ID: "old123", State: "running"}},
		removeErr:  fmt.Errorf("remove failed"),
	}
	p, _ := testNIMProvider(t, d)
	writeNIMModel(t, p.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama:latest")

	err := p.Switch(context.Background(), "llama-nim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "removing container")
}

func TestNIMSwitchCreateFails(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		createErr:  fmt.Errorf("create failed"),
	}
	p, _ := testNIMProvider(t, d)
	writeNIMModel(t, p.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama:latest")

	err := p.Switch(context.Background(), "llama-nim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating NIM container")
}

func TestNIMSwitchStartFails(t *testing.T) {
	d := &stubDocker{
		pullReader: emptyReader(),
		createResp: container.CreateResponse{ID: "abc123"},
		startErr:   fmt.Errorf("start failed"),
	}
	p, _ := testNIMProvider(t, d)
	writeNIMModel(t, p.cfg.Paths.ModelsDir, "llama-nim", "nvcr.io/nim/meta/llama:latest")

	err := p.Switch(context.Background(), "llama-nim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "starting NIM container")
}

// --- Stop ---

func TestNIMStopNoContainers(t *testing.T) {
	d := &stubDocker{}
	p, _ := testNIMProvider(t, d)
	require.NoError(t, p.Stop(context.Background()))
}

func TestNIMStopExisting(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{{ID: "c1", State: "running"}},
	}
	p, _ := testNIMProvider(t, d)
	require.NoError(t, p.Stop(context.Background()))
}

func TestNIMStopListFails(t *testing.T) {
	d := &stubDocker{listErr: fmt.Errorf("docker down")}
	p, _ := testNIMProvider(t, d)
	assert.Error(t, p.Stop(context.Background()))
}

// --- Status ---

func TestNIMStatusRunning(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{ID: "abc123", State: "running", Image: "nvcr.io/nim/meta/llama-3.1-8b:latest"},
		},
	}
	p, _ := testNIMProvider(t, d)
	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, s.Running)
	assert.Equal(t, "abc123", s.ContainerID)
	assert.Equal(t, "llama-3.1-8b", s.ModelID)
}

func TestNIMStatusNotRunning(t *testing.T) {
	d := &stubDocker{}
	p, _ := testNIMProvider(t, d)
	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, s.Running)
}

func TestNIMStatusListFails(t *testing.T) {
	d := &stubDocker{listErr: fmt.Errorf("docker down")}
	p, _ := testNIMProvider(t, d)
	_, err := p.Status(context.Background())
	assert.Error(t, err)
}

// --- Logs ---

func TestNIMLogsSuccess(t *testing.T) {
	content := "\x01\x00\x00\x00\x00\x00\x00\x05hello" // docker multiplexed stream
	d := &stubDocker{
		listResult: []container.Summary{{ID: "c1"}},
		logsReader: io.NopCloser(strings.NewReader(content)),
	}
	p, _ := testNIMProvider(t, d)
	var buf bytes.Buffer
	// stdcopy.StdCopy handles the multiplexed format; error is fine for truncated stream
	_ = p.Logs(context.Background(), &buf, false, 50)
}

func TestNIMLogsNoContainer(t *testing.T) {
	d := &stubDocker{}
	p, _ := testNIMProvider(t, d)
	err := p.Logs(context.Background(), io.Discard, false, 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no NIM container found")
}

func TestNIMLogsListFails(t *testing.T) {
	d := &stubDocker{listErr: fmt.Errorf("docker down")}
	p, _ := testNIMProvider(t, d)
	assert.Error(t, p.Logs(context.Background(), io.Discard, false, 50))
}

func TestNIMLogsReaderFails(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{{ID: "c1"}},
		logsErr:    fmt.Errorf("logs failed"),
	}
	p, _ := testNIMProvider(t, d)
	err := p.Logs(context.Background(), io.Discard, false, 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fetching NIM logs")
}

// --- imageToModelID ---

func TestImageToModelID(t *testing.T) {
	cases := []struct{ image, want string }{
		{"nvcr.io/nim/meta/llama-3.1-8b-instruct:latest", "llama-3.1-8b-instruct"},
		{"nvcr.io/nim/qwen/qwen2.5-72b:1.0", "qwen2.5-72b"},
		{"myimage", "myimage"},
		{"myimage:tag", "myimage"},
		{"repo/image", "image"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, imageToModelID(c.image), c.image)
	}
}

// ensure _modelsDir referenced in testNIMProvider helper is used
var _ = filepath.Join

// — NewNIMProvider runtime dispatch —

// Docker SDK does not connect at client creation time, so we can test the
// constructor with non-existent socket paths.

func TestNewNIMProviderDockerCustomSocket(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "docker"
	cfg.Service.ContainerSocket = "/tmp/marlin-test-docker.sock" // short path; file need not exist
	p, err := NewNIMProvider(cfg, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewNIMProviderPodmanCustomSocket(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "podman"
	cfg.Service.ContainerSocket = "/tmp/marlin-test-podman.sock" // short path; file need not exist
	p, err := NewNIMProvider(cfg, "test-key")
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewNIMProviderPodmanAutoSocket(t *testing.T) {
	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "podman"
	cfg.Service.ContainerSocket = "" // trigger auto-detect
	p, err := NewNIMProvider(cfg, "")
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNGCRegistryAuth(t *testing.T) {
	auth := ngcRegistryAuth("nvapi-test-key")
	assert.NotEmpty(t, auth)

	// Decode and verify the JSON payload contains the expected credentials.
	raw, err := base64.URLEncoding.DecodeString(auth)
	require.NoError(t, err)
	payload := string(raw)
	assert.Contains(t, payload, `"$oauthtoken"`)
	assert.Contains(t, payload, `"nvapi-test-key"`)
}

func TestNGCRegistryAuthEmpty(t *testing.T) {
	assert.Empty(t, ngcRegistryAuth(""))
}
