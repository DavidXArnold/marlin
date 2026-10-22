package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/pkg/render"
)

// LlamaCppProvider manages llama-server via a systemd service and an env-file symlink.
//
// Layout on disk:
//
//	<models_dir>/<slug>.llamacpp.env   rendered env file for each model
//	<llamacpp_env_file>                symlink → active model's env file
//
// The systemd unit (default "marlin-llamacpp") reads the env file and runs
// llama-server with the variables it defines.
type LlamaCppProvider struct {
	cfg       *config.Config
	svc       *service.SystemdManager
	w         io.Writer
	loadModel func(slug string) (*config.ModelConfig, error)
	hfToken   string // written as HF_TOKEN so -hf can pull gated repos
}

// NewLlamaCppProvider builds a LlamaCppProvider that searches dirs for model configs.
// hfToken is written to the env file as HF_TOKEN when non-empty.
func NewLlamaCppProvider(cfg *config.Config, dirs []string, hfToken string) *LlamaCppProvider {
	return &LlamaCppProvider{
		cfg:     cfg,
		svc:     service.NewSystemdManager(cfg.Service.LlamaCppUnit),
		w:       os.Stderr,
		hfToken: hfToken,
		loadModel: func(slug string) (*config.ModelConfig, error) {
			return config.ResolveModel(slug, dirs...)
		},
	}
}

func (l *LlamaCppProvider) Switch(ctx context.Context, modelSlug string) error {
	m, err := l.loadModel(modelSlug)
	if err != nil {
		return fmt.Errorf("loading model %q: %w", modelSlug, err)
	}

	if m.Serve.GGUFPath == "" && m.Model.ID == "" {
		return fmt.Errorf("model %q needs model.id (a Hugging Face GGUF repo) or serve.gguf_path", modelSlug)
	}

	// The containerized unit needs an image; the profile's wins over the global default.
	image := ""
	if l.cfg.Service.LlamaCppMode != "binary" {
		image = m.Model.Image
		if image == "" {
			image = l.cfg.Service.LlamaCppImage
		}
	}

	envPath := filepath.Join(l.cfg.Paths.ModelsDir, modelSlug+".llamacpp.env")
	envContent := []byte(render.LlamaCppEnv(m, image, l.hfToken))

	written, err := privilege.PromptAndWriteFile(l.w, filepath.Dir(envPath), envPath, envContent)
	if err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}
	if !written {
		return fmt.Errorf("writing env file: cancelled")
	}

	symlink := l.cfg.Paths.LlamaCppEnvFile
	if symlink == "" {
		symlink = "/etc/marlin/llamacpp.env"
	}
	if err := privilege.PromptAndSymlink(l.w, envPath, symlink); err != nil {
		return fmt.Errorf("updating active symlink: %w", err)
	}

	return l.svc.Restart(ctx)
}

func (l *LlamaCppProvider) Stop(ctx context.Context) error {
	return l.svc.Stop(ctx)
}

func (l *LlamaCppProvider) Status(ctx context.Context) (*Status, error) {
	raw, err := l.svc.ActiveState(ctx)
	if err != nil {
		return nil, err
	}
	active := raw == "active" || raw == "reloading"

	s := &Status{Running: active, State: service.FriendlyState(raw)}
	if n, err := l.svc.Restarts(ctx); err == nil {
		s.Restarts = n
	}

	if active {
		symlink := l.cfg.Paths.LlamaCppEnvFile
		if symlink == "" {
			symlink = "/etc/marlin/llamacpp.env"
		}
		target, err := os.Readlink(symlink)
		if err == nil {
			base := filepath.Base(target)
			// strip ".llamacpp.env" suffix (13 chars)
			const sfx = ".llamacpp.env"
			if len(base) > len(sfx) && base[len(base)-len(sfx):] == sfx {
				s.ModelID = base[:len(base)-len(sfx)]
			}
		}
	}

	return s, nil
}

func (l *LlamaCppProvider) Logs(ctx context.Context, w io.Writer, follow bool, lines int) error {
	unit := l.cfg.Service.LlamaCppUnit
	if unit == "" {
		unit = "marlin-llamacpp"
	}
	args := []string{"journalctl", "-u", unit, fmt.Sprintf("-n%d", lines)}
	if follow {
		args = append(args, "-f")
	}
	return runCommand(ctx, w, args[0], args[1:]...)
}
