package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	dimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// nerdctlClient implements dockerClient using exec.Command("nerdctl", ...) calls.
// It satisfies the same interface so NIMProvider and AdhocRunner work unchanged.
type nerdctlClient struct {
	bin string // absolute path to the nerdctl binary
}

// FindNerdctl returns the path to nerdctl or an error if not found.
func FindNerdctl() (string, error) {
	bin, err := exec.LookPath("nerdctl")
	if err != nil {
		return "", fmt.Errorf("nerdctl not found in PATH")
	}
	return bin, nil
}

func newNerdctlClient() (*nerdctlClient, error) {
	bin, err := FindNerdctl()
	if err != nil {
		return nil, err
	}
	return &nerdctlClient{bin: bin}, nil
}

// run executes a nerdctl command and returns combined output.
func (c *nerdctlClient) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("nerdctl %s: %w — %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// ImagePull pulls an image, decoding NGC registry auth from the base64 creds blob.
func (c *nerdctlClient) ImagePull(ctx context.Context, ref string, opts dimage.PullOptions) (io.ReadCloser, error) {
	args := []string{"pull"}
	if opts.RegistryAuth != "" {
		if user, pass, ok := decodeDockerAuth(opts.RegistryAuth); ok && pass != "" {
			args = append(args, "--username", user, "--password", pass)
		}
	}
	args = append(args, ref)

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

// ContainerCreate maps to nerdctl run -d (create + immediate start).
// ContainerStart is therefore a no-op.
func (c *nerdctlClient) ContainerCreate(ctx context.Context, cfg *container.Config, hostCfg *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
	args := []string{"run", "-d", "--name", name}

	for _, req := range hostCfg.DeviceRequests {
		if req.Driver == "nvidia" {
			args = append(args, "--gpus", "all")
		}
	}

	for port, bindings := range hostCfg.PortBindings {
		for _, b := range bindings {
			args = append(args, "-p", b.HostPort+":"+port.Port())
		}
	}

	for _, bind := range hostCfg.Binds {
		args = append(args, "-v", bind)
	}

	for _, e := range cfg.Env {
		args = append(args, "-e", e)
	}

	for k, v := range cfg.Labels {
		args = append(args, "--label", k+"="+v)
	}

	args = append(args, cfg.Image)

	out, err := c.run(ctx, args...)
	if err != nil {
		return container.CreateResponse{}, err
	}
	return container.CreateResponse{ID: strings.TrimSpace(string(out))}, nil
}

// ContainerStart is a no-op: nerdctl run -d in ContainerCreate already started it.
func (c *nerdctlClient) ContainerStart(_ context.Context, _ string, _ container.StartOptions) error {
	return nil
}

func (c *nerdctlClient) ContainerStop(ctx context.Context, id string, opts container.StopOptions) error {
	args := []string{"stop"}
	if opts.Timeout != nil {
		args = append(args, "--time", strconv.Itoa(*opts.Timeout))
	}
	args = append(args, id)
	_, err := c.run(ctx, args...)
	return err
}

func (c *nerdctlClient) ContainerRemove(ctx context.Context, id string, opts container.RemoveOptions) error {
	args := []string{"rm"}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, id)
	_, err := c.run(ctx, args...)
	return err
}

// nerdctlPS is the JSON shape returned by nerdctl ps --format '{{json .}}'.
type nerdctlPS struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
	Labels string `json:"Labels"`
}

func (c *nerdctlClient) ContainerList(ctx context.Context, opts container.ListOptions) ([]container.Summary, error) {
	args := []string{"ps", "--format", "{{json .}}"}
	if opts.All {
		args = append(args, "--all")
	}
	for _, v := range opts.Filters.Get("label") {
		args = append(args, "--filter", "label="+v)
	}
	for _, v := range opts.Filters.Get("name") {
		args = append(args, "--filter", "name="+v)
	}

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var result []container.Summary
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ps nerdctlPS
		if err := json.Unmarshal(line, &ps); err != nil {
			continue
		}
		result = append(result, ps.toSummary())
	}
	return result, nil
}

func (ps nerdctlPS) toSummary() container.Summary {
	state := "exited"
	if strings.HasPrefix(ps.Status, "Up ") {
		state = "running"
	}

	labels := map[string]string{}
	for _, kv := range strings.Split(ps.Labels, ",") {
		if i := strings.IndexByte(kv, '='); i > 0 {
			labels[kv[:i]] = kv[i+1:]
		}
	}

	var ports []container.Port
	// Parse "0.0.0.0:8000->8000/tcp" format
	for _, entry := range strings.Split(ps.Ports, ", ") {
		if arrow := strings.Index(entry, "->"); arrow > 0 {
			hostPart := entry[:arrow]
			if colon := strings.LastIndex(hostPart, ":"); colon >= 0 {
				if pub, err := strconv.ParseUint(hostPart[colon+1:], 10, 16); err == nil {
					ports = append(ports, container.Port{PublicPort: uint16(pub)})
				}
			}
		}
	}

	return container.Summary{
		ID:     ps.ID,
		Names:  []string{"/" + ps.Names},
		Image:  ps.Image,
		State:  state,
		Labels: labels,
		Ports:  ports,
	}
}

// ContainerLogs streams logs from nerdctl. The output is wrapped in Docker's
// 8-byte multiplexed framing so callers using stdcopy.StdCopy work correctly.
func (c *nerdctlClient) ContainerLogs(ctx context.Context, id string, opts container.LogsOptions) (io.ReadCloser, error) {
	args := []string{"logs"}
	if opts.Follow {
		args = append(args, "--follow")
	}
	if opts.Tail != "" && opts.Tail != "all" {
		args = append(args, "--tail", opts.Tail)
	}
	args = append(args, id)

	pr, pw := io.Pipe()
	go func() {
		cmd := exec.CommandContext(ctx, c.bin, args...)
		stdoutPipe, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}

		var mu sync.Mutex
		done := make(chan struct{}, 2)
		mux := func(streamType byte, r io.Reader) {
			defer func() { done <- struct{}{} }()
			buf := make([]byte, 32*1024)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					hdr := [8]byte{streamType, 0, 0, 0,
						byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
					mu.Lock()
					_, _ = pw.Write(hdr[:])
					_, _ = pw.Write(buf[:n])
					mu.Unlock()
				}
				if err != nil {
					return
				}
			}
		}
		go mux(1, stdoutPipe)
		go mux(2, stderrPipe)
		<-done
		<-done
		_ = cmd.Wait()
		_ = pw.Close()
	}()

	return pr, nil
}

// decodeDockerAuth unpacks a base64-encoded Docker auth JSON blob.
func decodeDockerAuth(encoded string) (username, password string, ok bool) {
	b, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", "", false
		}
	}
	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		return "", "", false
	}
	return creds.Username, creds.Password, true
}

// ContainerBinary returns the path to docker or nerdctl, preferring docker.
// This is used by configure.go for login operations.
func ContainerBinary() (string, error) {
	if bin, err := exec.LookPath("docker"); err == nil {
		return bin, nil
	}
	if bin, err := exec.LookPath("nerdctl"); err == nil {
		return bin, nil
	}
	return "", fmt.Errorf("no container runtime found — install docker or nerdctl")
}

