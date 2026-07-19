package provider

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/mesh"
	"github.com/DavidXArnold/marlin/internal/service"
)

// Ensure service import is used for SystemdManagerWithRunner in makeMeshProvider.
var _ = service.NewSystemdManager

// stubMeshClient implements meshClient for testing.
type stubMeshClient struct {
	loadErr    error
	unloadErr  error
	runtimeInfo *mesh.RuntimeInfo
	runtimeErr  error
	loadCalled  []string
	unloadCalled []string
}

func (s *stubMeshClient) LoadModel(_ context.Context, ref string) error {
	s.loadCalled = append(s.loadCalled, ref)
	return s.loadErr
}

func (s *stubMeshClient) UnloadModel(_ context.Context, ref string) error {
	s.unloadCalled = append(s.unloadCalled, ref)
	return s.unloadErr
}

func (s *stubMeshClient) Runtime(_ context.Context) (*mesh.RuntimeInfo, error) {
	return s.runtimeInfo, s.runtimeErr
}

func makeMeshProvider(t *testing.T, client meshClient, models map[string]*config.ModelConfig) *MeshProvider {
	t.Helper()
	cfg := &config.Config{
		Mesh: config.MeshConfig{SystemdUnit: "mesh-llm", ManagementURL: "http://localhost:3131"},
	}
	p := &MeshProvider{
		cfg:    cfg,
		svc:    service.NewSystemdManagerWithRunner("mesh-llm", func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil }),
		client: client,
		loadModel: func(slug string) (*config.ModelConfig, error) {
			if m, ok := models[slug]; ok {
				return m, nil
			}
			return nil, errors.New("model not found: " + slug)
		},
	}
	return p
}

func TestMeshProviderSwitchLoadsModel(t *testing.T) {
	stub := &stubMeshClient{}
	p := makeMeshProvider(t, stub, map[string]*config.ModelConfig{
		"qwen-8b": {
			Model: config.ModelMeta{Type: config.ProviderMesh},
			Serve: config.ServeConfig{GGUFPath: "/models/qwen-8b.gguf"},
		},
	})

	require.NoError(t, p.Switch(context.Background(), "qwen-8b"))
	assert.Equal(t, []string{"/models/qwen-8b.gguf"}, stub.loadCalled)
	assert.Equal(t, "/models/qwen-8b.gguf", p.current)
}

func TestMeshProviderSwitchUnloadsPreviousFirst(t *testing.T) {
	stub := &stubMeshClient{}
	p := makeMeshProvider(t, stub, map[string]*config.ModelConfig{
		"modelA": {Serve: config.ServeConfig{GGUFPath: "/models/a.gguf"}},
		"modelB": {Serve: config.ServeConfig{GGUFPath: "/models/b.gguf"}},
	})
	p.current = "/models/a.gguf"

	require.NoError(t, p.Switch(context.Background(), "modelB"))
	require.Len(t, stub.unloadCalled, 1)
	assert.Equal(t, "/models/a.gguf", stub.unloadCalled[0])
	assert.Equal(t, "/models/b.gguf", p.current)
}

func TestMeshProviderSwitchNoGGUFPath(t *testing.T) {
	stub := &stubMeshClient{}
	p := makeMeshProvider(t, stub, map[string]*config.ModelConfig{
		"no-gguf": {Model: config.ModelMeta{Type: config.ProviderMesh}},
	})
	err := p.Switch(context.Background(), "no-gguf")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gguf_path")
}

func TestMeshProviderSwitchModelNotFound(t *testing.T) {
	stub := &stubMeshClient{}
	p := makeMeshProvider(t, stub, nil)
	assert.Error(t, p.Switch(context.Background(), "ghost"))
}

func TestMeshProviderSwitchLoadError(t *testing.T) {
	stub := &stubMeshClient{loadErr: errors.New("mesh-llm offline")}
	p := makeMeshProvider(t, stub, map[string]*config.ModelConfig{
		"m": {Serve: config.ServeConfig{GGUFPath: "/models/m.gguf"}},
	})
	err := p.Switch(context.Background(), "m")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mesh-llm offline")
}

func TestMeshProviderStopUnloads(t *testing.T) {
	stub := &stubMeshClient{}
	p := makeMeshProvider(t, stub, nil)
	p.current = "/models/m.gguf"

	require.NoError(t, p.Stop(context.Background()))
	assert.Equal(t, []string{"/models/m.gguf"}, stub.unloadCalled)
	assert.Empty(t, p.current)
}

func TestMeshProviderStopNoop(t *testing.T) {
	stub := &stubMeshClient{}
	p := makeMeshProvider(t, stub, nil)

	require.NoError(t, p.Stop(context.Background()))
	assert.Empty(t, stub.unloadCalled)
}

func TestMeshProviderStatusRunning(t *testing.T) {
	stub := &stubMeshClient{
		runtimeInfo: &mesh.RuntimeInfo{
			Models: []mesh.ModelInfo{{Ref: "/models/m.gguf", State: "ready"}},
		},
	}
	p := makeMeshProvider(t, stub, nil)
	p.current = "/models/m.gguf"

	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, s.Running)
	assert.Equal(t, "/models/m.gguf", s.ModelID)
}

func TestMeshProviderStatusNotRunning(t *testing.T) {
	stub := &stubMeshClient{runtimeInfo: nil}
	p := makeMeshProvider(t, stub, nil)

	s, err := p.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, s.Running)
}

func TestMeshProviderStatusAPIError(t *testing.T) {
	stub := &stubMeshClient{runtimeErr: errors.New("connection refused")}
	p := makeMeshProvider(t, stub, nil)

	_, err := p.Status(context.Background())
	assert.Error(t, err)
}

func TestMeshProviderLogsCallsJournalctl(t *testing.T) {
	var gotCmd string
	var gotArgs []string
	restore := SetRunCommandForTest(func(_ context.Context, _ io.Writer, name string, args ...string) error {
		gotCmd = name
		gotArgs = args
		return nil
	})
	defer restore()

	stub := &stubMeshClient{}
	cfg := &config.Config{Mesh: config.MeshConfig{SystemdUnit: "mesh-llm"}}
	p := &MeshProvider{cfg: cfg, client: stub}

	require.NoError(t, p.Logs(context.Background(), io.Discard, false, 50))
	assert.Equal(t, "journalctl", gotCmd)
	assert.Contains(t, gotArgs, "-u")
	assert.Contains(t, gotArgs, "mesh-llm")
	assert.Contains(t, gotArgs, "-n50")
}
