package provider

import (
	"context"
	"fmt"
	"io"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/mesh"
	"github.com/DavidXArnold/marlin/internal/service"
)

// MeshProvider manages GGUF models via the mesh-llm local management API.
//
// It uses mesh-llm's management API (default localhost:3131) to load/unload
// individual models without touching the mesh-llm daemon lifecycle. The daemon
// is managed separately via "marlin mesh start/stop".
//
// Model profiles must specify serve.gguf_path pointing at a local GGUF file.
type MeshProvider struct {
	cfg       *config.Config
	svc       *service.SystemdManager
	client    meshClient
	loadModel func(slug string) (*config.ModelConfig, error)
	current   string // GGUFPath of the currently loaded model
}

// meshClient is the subset of mesh.Client used by MeshProvider, extracted for
// test injection.
type meshClient interface {
	LoadModel(ctx context.Context, modelRef string) error
	UnloadModel(ctx context.Context, modelRef string) error
	Runtime(ctx context.Context) (*mesh.RuntimeInfo, error)
}

// NewMeshProvider builds a MeshProvider searching dirs for model configs.
func NewMeshProvider(cfg *config.Config, dirs []string) *MeshProvider {
	return &MeshProvider{
		cfg:    cfg,
		svc:    service.NewSystemdManager(cfg.Mesh.SystemdUnit),
		client: mesh.NewClient(cfg.Mesh.ManagementURL),
		loadModel: func(slug string) (*config.ModelConfig, error) {
			return config.ResolveModel(slug, dirs...)
		},
	}
}

// Switch loads modelSlug into the running mesh-llm peer via the management API.
// The previous model (if any) is unloaded first.
func (m *MeshProvider) Switch(ctx context.Context, modelSlug string) error {
	mc, err := m.loadModel(modelSlug)
	if err != nil {
		return fmt.Errorf("loading model %q: %w", modelSlug, err)
	}
	if mc.Serve.GGUFPath == "" {
		return fmt.Errorf("model %q: gguf_path is required for mesh provider", modelSlug)
	}

	if m.current != "" {
		_ = m.client.UnloadModel(ctx, m.current)
	}
	if err := m.client.LoadModel(ctx, mc.Serve.GGUFPath); err != nil {
		return fmt.Errorf("loading %q into mesh-llm: %w", modelSlug, err)
	}
	m.current = mc.Serve.GGUFPath
	return nil
}

// Stop unloads the active model from mesh-llm.
// It does NOT stop the mesh-llm daemon, which may serve other models to peers.
func (m *MeshProvider) Stop(ctx context.Context) error {
	if m.current == "" {
		return nil
	}
	err := m.client.UnloadModel(ctx, m.current)
	m.current = ""
	return err
}

// Status queries mesh-llm via the management API.
func (m *MeshProvider) Status(ctx context.Context) (*Status, error) {
	info, err := m.client.Runtime(ctx)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return &Status{Running: false}, nil
	}
	s := &Status{Running: true}
	for _, model := range info.Models {
		if model.Ref == m.current {
			s.ModelID = model.Ref
			break
		}
	}
	return s, nil
}

// Logs tails the mesh-llm systemd journal.
func (m *MeshProvider) Logs(ctx context.Context, w io.Writer, follow bool, lines int) error {
	args := []string{"journalctl", "-u", m.cfg.Mesh.SystemdUnit,
		fmt.Sprintf("-n%d", lines)}
	if follow {
		args = append(args, "-f")
	}
	return runCommand(ctx, w, args[0], args[1:]...)
}
