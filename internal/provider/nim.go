package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
)

const nimContainerName = "marlin-nim"

// ngcRegistryAuth encodes NGC credentials into the base64 JSON string that the
// Docker SDK expects for nvcr.io authentication.
// Docker requires username="$oauthtoken" (literal) and password=NGC API key.
func ngcRegistryAuth(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	type creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	b, _ := json.Marshal(creds{Username: "$oauthtoken", Password: apiKey})
	return base64.URLEncoding.EncodeToString(b)
}

// dockerClient is the subset of the Docker SDK used by NIMProvider.
// Defined as an interface so tests can inject a stub.
type dockerClient interface {
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerCreate(ctx context.Context, cfg *container.Config, hostCfg *container.HostConfig,
		netCfg *network.NetworkingConfig, platform *ocispec.Platform, name string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	ImagePull(ctx context.Context, refStr string, options dimage.PullOptions) (io.ReadCloser, error)
}

// NIMProvider manages NVIDIA NIM inference containers via the Docker SDK.
//
// NOTE: NGC_API_KEY source (secrets.env) is a temporary arrangement — flagged
// for review to support proper secrets management (e.g. credential helpers).
//
// NOTE: Port availability during NIM switch causes a service gap while the new
// container downloads weights and compiles TRT engines (can be minutes).
// Future enhancement: front port 8000 with a reverse proxy for zero-downtime switching.
type NIMProvider struct {
	cfg          *config.Config
	ngcKey       string
	docker       dockerClient
	loadModel    func(slug string) (*config.ModelConfig, error)
	w            io.Writer                    // for privilege prompts; defaults to os.Stderr
	prepareCache func(io.Writer, string) error // injectable for tests
}

func NewNIMProvider(cfg *config.Config, ngcKey string) (*NIMProvider, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}

	switch cfg.Service.ContainerRuntime {
	case "nerdctl":
		nc, err := newNerdctlClient()
		if err != nil {
			return nil, fmt.Errorf("connecting to container runtime: %w", err)
		}
		return newNIMProviderWithClient(cfg, ngcKey, nc), nil
	case "podman":
		socket := cfg.Service.ContainerSocket
		if socket == "" {
			socket = defaultPodmanSocket()
		}
		opts = append(opts, client.WithHost("unix://"+socket))
	default: // "docker" or ""
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
	return newNIMProviderWithClient(cfg, ngcKey, cli), nil
}

// defaultPodmanSocket returns the most likely podman API socket path.
// Checks rootful first, then rootless (per-user).
func defaultPodmanSocket() string {
	if _, err := os.Stat("/run/podman/podman.sock"); err == nil {
		return "/run/podman/podman.sock"
	}
	return fmt.Sprintf("/run/user/%d/podman/podman.sock", os.Getuid())
}

func newNIMProviderWithClient(cfg *config.Config, ngcKey string, docker dockerClient) *NIMProvider {
	return &NIMProvider{
		cfg:          cfg,
		ngcKey:       ngcKey,
		docker:       docker,
		w:            os.Stderr,
		prepareCache: privilege.PromptAndPrepareNIMCache,
		loadModel: func(slug string) (*config.ModelConfig, error) {
			return config.LoadModel(filepath.Join(cfg.Paths.ModelsDir, slug+".toml"))
		},
	}
}

func (n *NIMProvider) Switch(ctx context.Context, modelSlug string) error {
	m, err := n.loadModel(modelSlug)
	if err != nil {
		return fmt.Errorf("loading model %q: %w", modelSlug, err)
	}
	if m.Model.Image == "" {
		return fmt.Errorf("model %q has no image set (required for nim provider)", modelSlug)
	}

	// Pull image, streaming progress output.
	// NOTE: downtime begins here — zero-downtime proxy is a future enhancement.
	reader, err := n.docker.ImagePull(ctx, m.Model.Image, dimage.PullOptions{
		RegistryAuth: ngcRegistryAuth(n.ngcKey),
	})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", m.Model.Image, err)
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		_ = reader.Close()
		return fmt.Errorf("reading image pull response: %w", err)
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("closing image pull response: %w", err)
	}

	// Stop and remove any existing marlin-nim container.
	if err := n.stopExisting(ctx); err != nil {
		return err
	}

	if err := n.prepareCache(n.w, n.cfg.Paths.NIMCache); err != nil {
		return fmt.Errorf("preparing NIM cache dir %s: %w", n.cfg.Paths.NIMCache, err)
	}

	portSet := nat.PortSet{"8000/tcp": struct{}{}}
	portBindings := nat.PortMap{"8000/tcp": []nat.PortBinding{{HostPort: "8000"}}}

	env := append([]string{"NGC_API_KEY=" + n.ngcKey}, m.Serve.ExtraEnv...)
	binds := append([]string{n.cfg.Paths.NIMCache + ":/opt/nim/.cache"}, m.Serve.ExtraVolumes...)

	resp, err := n.docker.ContainerCreate(ctx,
		&container.Config{
			Image:        m.Model.Image,
			ExposedPorts: portSet,
			Env:          env,
		},
		&container.HostConfig{
			PortBindings: portBindings,
			Binds:        binds,
			Resources: container.Resources{
				DeviceRequests: []container.DeviceRequest{
					{Driver: "nvidia", Count: -1, Capabilities: [][]string{{"gpu"}}},
				},
			},
		},
		nil, nil, nimContainerName,
	)
	if err != nil {
		return fmt.Errorf("creating NIM container: %w", err)
	}

	if err := n.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting NIM container %s: %w", resp.ID, err)
	}

	return nil
}

func (n *NIMProvider) Stop(ctx context.Context) error {
	return n.stopExisting(ctx)
}

func (n *NIMProvider) Status(ctx context.Context) (*Status, error) {
	containers, err := n.docker.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", nimContainerName)),
	})
	if err != nil {
		return nil, fmt.Errorf("listing NIM containers: %w", err)
	}

	if len(containers) == 0 {
		return &Status{Running: false, ContainerState: "not found"}, nil
	}

	c := containers[0]
	return &Status{
		Running:        c.State == "running",
		ContainerID:    c.ID,
		ModelID:        imageToModelID(c.Image),
		ContainerState: c.State,
	}, nil
}

func (n *NIMProvider) Logs(ctx context.Context, w io.Writer, follow bool, lines int) error {
	containers, err := n.docker.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", nimContainerName)),
	})
	if err != nil {
		return fmt.Errorf("listing NIM containers: %w", err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("no NIM container running")
	}

	reader, err := n.docker.ContainerLogs(ctx, containers[0].ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       strconv.Itoa(lines),
	})
	if err != nil {
		return fmt.Errorf("fetching NIM logs: %w", err)
	}
	defer func() { _ = reader.Close() }()

	_, err = stdcopy.StdCopy(w, w, reader)
	return err
}

func (n *NIMProvider) stopExisting(ctx context.Context) error {
	containers, err := n.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", nimContainerName)),
	})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	for _, c := range containers {
		timeout := 30
		if err := n.docker.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			return fmt.Errorf("stopping container %s: %w", c.ID, err)
		}
		if err := n.docker.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("removing container %s: %w", c.ID, err)
		}
	}

	return nil
}

// imageToModelID extracts a short model name from a NIM image reference.
// e.g. "nvcr.io/nim/meta/llama-3.1-8b-instruct:latest" → "llama-3.1-8b-instruct"
func imageToModelID(image string) string {
	base := image
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == ':' {
			base = base[:i]
			break
		}
	}
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			return base[i+1:]
		}
	}
	return base
}
