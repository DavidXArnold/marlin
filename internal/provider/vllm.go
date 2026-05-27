package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/pkg/render"
)

// VLLMProvider manages vLLM via a systemd service and an env-file symlink.
//
// Layout on disk:
//
//	/etc/marlin/models/<slug>.env   rendered env file for each model
//	/etc/marlin/model.env           symlink → active model's .env file
//
// On switch: write the new .env, atomically replace the symlink, restart the
// systemd unit.  The old .env is left in place so the previous model can be
// switched back without regenerating it.
type VLLMProvider struct {
	cfg     *config.Config
	svc     *service.SystemdManager
	loadModel func(slug string) (*config.ModelConfig, error)
}

func NewVLLMProvider(cfg *config.Config) *VLLMProvider {
	return &VLLMProvider{
		cfg: cfg,
		svc: service.NewSystemdManager(cfg.Service.SystemdUnit),
		loadModel: func(slug string) (*config.ModelConfig, error) {
			path := filepath.Join(cfg.Paths.ModelsDir, slug+".toml")
			return config.LoadModel(path)
		},
	}
}

func (v *VLLMProvider) Switch(ctx context.Context, modelSlug string) error {
	m, err := v.loadModel(modelSlug)
	if err != nil {
		return fmt.Errorf("loading model %q: %w", modelSlug, err)
	}

	envPath := filepath.Join(v.cfg.Paths.ModelsDir, modelSlug+".env")
	if err := writeEnvFile(envPath, render.Env(m)); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}

	if err := atomicSymlink(envPath, v.cfg.Paths.ActiveSymlink); err != nil {
		return fmt.Errorf("updating active symlink: %w", err)
	}

	return v.svc.Restart(ctx)
}

func (v *VLLMProvider) Stop(ctx context.Context) error {
	return v.svc.Stop(ctx)
}

func (v *VLLMProvider) Status(ctx context.Context) (*Status, error) {
	active, err := v.svc.IsActive(ctx)
	if err != nil {
		return nil, err
	}

	s := &Status{Running: active}

	if active {
		target, err := os.Readlink(v.cfg.Paths.ActiveSymlink)
		if err == nil {
			// Derive model ID from symlink target filename (strip dir + .env)
			base := filepath.Base(target)
			if len(base) > 4 {
				s.ModelID = base[:len(base)-4]
			}
		}
	}

	return s, nil
}

func (v *VLLMProvider) Logs(ctx context.Context, w io.Writer, follow bool, lines int) error {
	args := []string{"journalctl", "-u", v.cfg.Service.SystemdUnit,
		fmt.Sprintf("-n%d", lines)}
	if follow {
		args = append(args, "-f")
	}
	return runCommand(ctx, w, args[0], args[1:]...)
}

// writeEnvFile writes content to path, creating parent directories as needed.
func writeEnvFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// atomicSymlink replaces linkPath so it points at target, atomically via a
// temp symlink + rename to avoid a window where the link doesn't exist.
func atomicSymlink(target, linkPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return err
	}

	tmp := linkPath + ".tmp"
	// Remove stale temp if present
	_ = os.Remove(tmp)

	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("creating temp symlink: %w", err)
	}

	if err := os.Rename(tmp, linkPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming symlink: %w", err)
	}

	return nil
}
