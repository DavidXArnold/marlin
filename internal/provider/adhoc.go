package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/secrets"
	"github.com/DavidXArnold/marlin/internal/ui"
)

const (
	labelManaged  = "marlin.managed"
	labelMode     = "marlin.mode"
	labelModel    = "marlin.model"
	labelProvider = "marlin.provider"
)

// AdhocInfo describes a marlin-managed ad-hoc container.
type AdhocInfo struct {
	Slug     string
	Provider string
	Status   string
	Port     string
	ID       string
}

// AdhocRunner manages ephemeral model containers launched via marlin run.
// Containers are labelled so they can be listed and cleaned up independently
// of the managed (switched) provider.
type AdhocRunner struct {
	cfg          *config.Config
	docker       dockerClient
	loadModel    func(slug string) (*config.ModelConfig, error)
	w            io.Writer                    // for privilege prompts; defaults to os.Stderr
	prepareCache func(io.Writer, string) error // injectable for tests
	refreshPerms func(string) error            // injectable for tests
}

func NewAdhocRunner(cfg *config.Config) (*AdhocRunner, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	switch cfg.Service.ContainerRuntime {
	case "nerdctl":
		nc, err := newNerdctlClient()
		if err != nil {
			return nil, fmt.Errorf("connecting to container runtime: %w", err)
		}
		return newAdhocRunnerWithClient(cfg, nc), nil
	case "podman":
		socket := cfg.Service.ContainerSocket
		if socket == "" {
			socket = defaultPodmanSocket()
		}
		opts = append(opts, client.WithHost("unix://"+socket))
	default:
		if cfg.Service.ContainerSocket != "" {
			opts = append(opts, client.WithHost("unix://"+cfg.Service.ContainerSocket))
		} else {
			opts = append(opts, client.FromEnv)
		}
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to container runtime: %w", err)
	}
	return newAdhocRunnerWithClient(cfg, dockerClientWrapper{cli}), nil
}

func newAdhocRunnerWithClient(cfg *config.Config, docker dockerClient) *AdhocRunner {
	return &AdhocRunner{
		cfg:          cfg,
		docker:       docker,
		w:            os.Stderr,
		prepareCache: privilege.PromptAndPrepareNIMCache,
		refreshPerms: privilege.RefreshNIMCachePerms,
		loadModel: func(slug string) (*config.ModelConfig, error) {
			return config.LoadModel(filepath.Join(cfg.Paths.ModelsDir, slug+".toml"))
		},
	}
}

// Start pulls the image and starts a labelled ad-hoc container. Returns the container ID.
func (a *AdhocRunner) Start(ctx context.Context, slug string) (string, error) {
	return a.startWithProgress(ctx, slug, io.Discard)
}

// startWithProgress is the internal start that shows pull progress to progressW.
func (a *AdhocRunner) startWithProgress(ctx context.Context, slug string, progressW io.Writer) (string, error) {
	m, err := a.loadModel(slug)
	if err != nil {
		return "", fmt.Errorf("loading model %q: %w", slug, err)
	}

	image, _, containerCfg, hostCfg, err := a.buildContainerConfig(slug, m)
	if err != nil {
		return "", err
	}

	pullOpts := dimage.PullOptions{}
	if m.Model.Type == config.ProviderNIM {
		sec, _ := secrets.Load(a.cfg.Paths.SecretsEnv)
		pullOpts.RegistryAuth = ngcRegistryAuth(sec["NGC_API_KEY"])
	}
	reader, err := a.docker.ImagePull(ctx, image, pullOpts)
	if err != nil {
		return "", fmt.Errorf("pulling image %s: %w", image, err)
	}
	ui.StreamPull(reader, progressW, ui.IsWriterTTY(progressW))
	if err := reader.Close(); err != nil {
		return "", fmt.Errorf("closing image pull response: %w", err)
	}

	containerName := "marlin-adhoc-" + slug
	resp, err := a.docker.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, containerName)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}

	if err := a.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("starting container: %w", err)
	}

	return resp.ID, nil
}

// RunForeground starts a container, streams pull progress and then container logs
// to w until ctx is cancelled, then stops and removes the container.
func (a *AdhocRunner) RunForeground(ctx context.Context, slug string, w io.Writer) error {
	id, err := a.startWithProgress(ctx, slug, w)
	if err != nil {
		return err
	}

	defer func() {
		bg := context.Background()
		timeout := 10
		_ = a.docker.ContainerStop(bg, id, container.StopOptions{Timeout: &timeout})
		_ = a.docker.ContainerRemove(bg, id, container.RemoveOptions{Force: true})
	}()

	reader, err := a.docker.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return fmt.Errorf("fetching container logs: %w", err)
	}
	defer func() { _ = reader.Close() }()

	_, _ = stdcopy.StdCopy(w, w, reader)
	return nil
}

// List returns all marlin-managed ad-hoc containers (running or stopped).
func (a *AdhocRunner) List(ctx context.Context) ([]AdhocInfo, error) {
	containers, err := a.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelManaged+"=true")),
	})
	if err != nil {
		return nil, fmt.Errorf("listing ad-hoc containers: %w", err)
	}

	result := make([]AdhocInfo, 0, len(containers))
	for _, c := range containers {
		port := ""
		for _, p := range c.Ports {
			if p.PublicPort != 0 {
				port = fmt.Sprintf("%d", p.PublicPort)
				break
			}
		}
		result = append(result, AdhocInfo{
			Slug:     c.Labels[labelModel],
			Provider: c.Labels[labelProvider],
			Status:   c.State,
			Port:     port,
			ID:       c.ID,
		})
	}
	return result, nil
}

// Stop stops and removes all ad-hoc containers matching slug.
func (a *AdhocRunner) Stop(ctx context.Context, slug string) error {
	containers, err := a.docker.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", labelManaged+"=true"),
			filters.Arg("label", labelModel+"="+slug),
		),
	})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("no ad-hoc container found for %q", slug)
	}
	return a.stopAll(ctx, containers)
}

// DetectUnmanaged returns running inference containers not managed by marlin.
func (a *AdhocRunner) DetectUnmanaged(ctx context.Context) ([]UnmanagedContainer, error) {
	return DetectUnmanaged(ctx, a.docker)
}

// LogsFor streams logs for the named adhoc container (matched by model slug).
// If multiple containers share the slug, the most recently created one wins.
// Stopped containers are included so callers can inspect last-run output.
func (a *AdhocRunner) LogsFor(ctx context.Context, slug string, w io.Writer, follow bool, lines int) error {
	containers, err := a.docker.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", labelManaged+"=true"),
			filters.Arg("label", labelModel+"="+slug),
		),
	})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("no adhoc container found for %q", slug)
	}
	id := containers[0].ID

	tail := "all"
	if lines > 0 {
		tail = fmt.Sprintf("%d", lines)
	}
	reader, err := a.docker.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
	})
	if err != nil {
		return fmt.Errorf("fetching container logs: %w", err)
	}
	defer func() { _ = reader.Close() }()
	_, _ = stdcopy.StdCopy(w, w, reader)
	return nil
}

// StopAll stops and removes every marlin-managed ad-hoc container.
func (a *AdhocRunner) StopAll(ctx context.Context) error {
	containers, err := a.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelManaged+"=true")),
	})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	return a.stopAll(ctx, containers)
}

func (a *AdhocRunner) stopAll(ctx context.Context, containers []container.Summary) error {
	for _, c := range containers {
		timeout := 10
		if err := a.docker.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			return fmt.Errorf("stopping container %s: %w", c.ID[:12], err)
		}
		if err := a.docker.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("removing container %s: %w", c.ID[:12], err)
		}
	}
	return nil
}

func (a *AdhocRunner) buildContainerConfig(slug string, m *config.ModelConfig) (
	image, providerName string,
	containerCfg *container.Config,
	hostCfg *container.HostConfig,
	err error,
) {
	portSet := nat.PortSet{"8000/tcp": struct{}{}}
	portBindings := nat.PortMap{"8000/tcp": []nat.PortBinding{{HostPort: "8000"}}}

	labels := map[string]string{
		labelManaged:  "true",
		labelMode:     "adhoc",
		labelModel:    slug,
		labelProvider: string(m.Model.Type),
	}

	gpuReq := []container.DeviceRequest{
		{Driver: "nvidia", Count: -1, Capabilities: [][]string{{"gpu"}}},
	}

	switch m.Model.Type {
	case config.ProviderNIM:
		image = m.Model.Image
		providerName = "nim"
		if image == "" {
			return "", "", nil, nil, fmt.Errorf("model %q has no image set (required for nim provider)", slug)
		}
		labels[labelProvider] = "nim"

		if err := a.prepareCache(a.w, a.cfg.Paths.NIMCache); err != nil {
			return "", "", nil, nil, fmt.Errorf("preparing NIM cache dir %s: %w", a.cfg.Paths.NIMCache, err)
		}
		if err := a.refreshPerms(a.cfg.Paths.NIMCache); err != nil {
			_, _ = fmt.Fprintf(a.w, "warning: could not refresh NIM cache permissions: %v\n", err)
		}

		sec, _ := secrets.Load(a.cfg.Paths.SecretsEnv)
		env := append([]string{"NGC_API_KEY=" + sec["NGC_API_KEY"]}, m.Serve.ExtraEnv...)
		binds := append([]string{a.cfg.Paths.NIMCache + ":/opt/nim/.cache"}, m.Serve.ExtraVolumes...)
		containerCfg = &container.Config{
			Image:        image,
			ExposedPorts: portSet,
			Labels:       labels,
			Env:          env,
		}
		hostCfg = &container.HostConfig{
			PortBindings: portBindings,
			Binds:        binds,
			Resources:    container.Resources{DeviceRequests: gpuReq},
		}

	default: // vllm
		image = a.cfg.Service.VLLMImage
		providerName = "vllm"
		labels[labelProvider] = "vllm"

		cmd := []string{"--model", m.Model.ID}
		for _, name := range m.Serve.ServedModelName {
			cmd = append(cmd, "--served-model-name", name)
		}
		if m.Serve.GPUMemoryUtilization > 0 {
			cmd = append(cmd, "--gpu-memory-utilization",
				fmt.Sprintf("%.2f", m.Serve.GPUMemoryUtilization))
		}
		if m.Serve.Quantization != "" {
			cmd = append(cmd, "--quantization", m.Serve.Quantization)
		}
		if m.Serve.MaxModelLen > 0 {
			cmd = append(cmd, "--max-model-len", fmt.Sprintf("%d", m.Serve.MaxModelLen))
		}
		cmd = append(cmd, m.Serve.ExtraFlags...)

		containerCfg = &container.Config{
			Image:        image,
			ExposedPorts: portSet,
			Labels:       labels,
			Cmd:          cmd,
		}
		hostCfg = &container.HostConfig{
			PortBindings: portBindings,
			Resources:    container.Resources{DeviceRequests: gpuReq},
		}
	}

	return image, providerName, containerCfg, hostCfg, nil
}
