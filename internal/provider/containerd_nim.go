package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DavidXArnold/marlin/internal/config"
)

// ContainerdNIMProvider runs NIM containers via nerdctl, the Docker-compatible
// CLI for containerd. It mirrors NIMProvider's behaviour without the Docker SDK.
//
// Prerequisites on the host:
//   - containerd with the NVIDIA container runtime configured
//   - nerdctl installed and on PATH
type ContainerdNIMProvider struct {
	cfg    *config.Config
	ngcKey string
	// cmdOutput runs a nerdctl sub-command and returns its combined output.
	// Replaceable in tests without a real container runtime.
	cmdOutput func(ctx context.Context, args ...string) ([]byte, error)
	// loginFunc authenticates with the container registry.
	// Replaceable in tests to avoid a real nerdctl login call.
	loginFunc func(ctx context.Context, registry, key string) error
	loadModel func(slug string) (*config.ModelConfig, error)
}

func NewContainerdNIMProvider(cfg *config.Config, ngcKey string) (*ContainerdNIMProvider, error) {
	if _, err := exec.LookPath("nerdctl"); err != nil {
		return nil, fmt.Errorf("nerdctl not found on PATH — install nerdctl for containerd support: %w", err)
	}
	return newContainerdNIMProviderWithRunner(cfg, ngcKey, func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "nerdctl", args...).CombinedOutput()
	}), nil
}

func newContainerdNIMProviderWithRunner(cfg *config.Config, ngcKey string, runner func(context.Context, ...string) ([]byte, error)) *ContainerdNIMProvider {
	return &ContainerdNIMProvider{
		cfg:       cfg,
		ngcKey:    ngcKey,
		cmdOutput: runner,
		loginFunc: nerdctlLogin,
		loadModel: func(slug string) (*config.ModelConfig, error) {
			return config.LoadModel(filepath.Join(cfg.Paths.ModelsDir, slug+".toml"))
		},
	}
}

// nerdctlLogin authenticates with a container registry using nerdctl login via stdin.
func nerdctlLogin(ctx context.Context, registry, key string) error {
	// "$oauthtoken" is the literal NGC username string, not a shell variable.
	cmd := exec.CommandContext(ctx, "nerdctl", "login", registry, "-u", "$oauthtoken", "--password-stdin")
	cmd.Stdin = strings.NewReader(key)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nerdctl login %s: %w\n%s", registry, err, out)
	}
	return nil
}

func (p *ContainerdNIMProvider) Switch(ctx context.Context, modelSlug string) error {
	m, err := p.loadModel(modelSlug)
	if err != nil {
		return fmt.Errorf("loading model %q: %w", modelSlug, err)
	}
	if m.Model.Image == "" {
		return fmt.Errorf("model %q has no image set (required for nim provider)", modelSlug)
	}

	// Authenticate with NGC registry so nerdctl can pull from nvcr.io.
	if p.ngcKey != "" {
		if err := p.loginFunc(ctx, "nvcr.io", p.ngcKey); err != nil {
			return err
		}
	}

	if out, err := p.cmdOutput(ctx, "pull", m.Model.Image); err != nil {
		return fmt.Errorf("pulling image %s: %w\n%s", m.Model.Image, err, out)
	}

	// Tear down any existing marlin-nim container.
	if err := p.stopExisting(ctx); err != nil {
		return err
	}

	args := []string{
		"run", "-d",
		"--name", nimContainerName,
		"--gpus", "all",
		"-p", "8000:8000",
		"-e", "NGC_API_KEY=" + p.ngcKey,
		"-v", p.cfg.Paths.NIMCache + ":/opt/nim/.cache",
		m.Model.Image,
	}
	if out, err := p.cmdOutput(ctx, args...); err != nil {
		return fmt.Errorf("starting NIM container: %w\n%s", err, out)
	}
	return nil
}

func (p *ContainerdNIMProvider) Stop(ctx context.Context) error {
	return p.stopExisting(ctx)
}

func (p *ContainerdNIMProvider) Status(ctx context.Context) (*Status, error) {
	// Use inspect to get full container state. Returns error when container absent.
	out, err := p.cmdOutput(ctx, "inspect", nimContainerName)
	if err != nil {
		return &Status{Running: false}, nil
	}

	var containers []struct {
		ID    string `json:"Id"`
		Image string `json:"Image"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
	}
	if jsonErr := json.Unmarshal(out, &containers); jsonErr != nil || len(containers) == 0 {
		return &Status{Running: false}, nil
	}

	c := containers[0]
	return &Status{
		Running:     c.State.Status == "running",
		ContainerID: c.ID,
		ModelID:     imageToModelID(c.Image),
	}, nil
}

func (p *ContainerdNIMProvider) Logs(ctx context.Context, w io.Writer, follow bool, lines int) error {
	args := []string{"logs", "-n", fmt.Sprintf("%d", lines)}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, nimContainerName)
	return runCommand(ctx, w, "nerdctl", args...)
}

func (p *ContainerdNIMProvider) stopExisting(ctx context.Context) error {
	// Ignore errors — container may not exist.
	p.cmdOutput(ctx, "stop", nimContainerName)     //nolint:errcheck
	p.cmdOutput(ctx, "rm", "-f", nimContainerName) //nolint:errcheck
	return nil
}
