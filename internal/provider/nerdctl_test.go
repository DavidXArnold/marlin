package provider

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dimage "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
)

// makeTestNerdctl writes a shell script that impersonates nerdctl and returns its path.
func makeTestNerdctl(t *testing.T, script string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "nerdctl")
	require.NoError(t, err)
	_, err = fmt.Fprintf(f, "#!/bin/sh\n%s\n", script)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.Chmod(f.Name(), 0o755))
	return f.Name()
}

func newTestNerdctlClient(t *testing.T, script string) *nerdctlClient {
	t.Helper()
	return &nerdctlClient{bin: makeTestNerdctl(t, script)}
}

// TestDecodeDockerAuth covers both valid and invalid base64 auth blobs.
func TestDecodeDockerAuth(t *testing.T) {
	// Valid URLEncoding
	auth := ngcRegistryAuth("my-api-key")
	user, pass, ok := decodeDockerAuth(auth)
	assert.True(t, ok)
	assert.Equal(t, "$oauthtoken", user)
	assert.Equal(t, "my-api-key", pass)

	// Invalid base64
	_, _, ok = decodeDockerAuth("not-base64!!!")
	assert.False(t, ok)

	// Valid base64 but bad JSON
	_, _, ok = decodeDockerAuth("bm90anNvbg==") // "notjson"
	assert.False(t, ok)
}

func TestNerdctlPSToSummaryRunning(t *testing.T) {
	ps := nerdctlPS{
		ID:     "abc123",
		Names:  "marlin-nim",
		Image:  "nvcr.io/nim/meta/llama:latest",
		Status: "Up 2 hours",
		Ports:  "0.0.0.0:8000->8000/tcp",
		Labels: "marlin.managed=true,marlin.mode=nim",
	}
	s := ps.toSummary()
	assert.Equal(t, "abc123", s.ID)
	assert.Equal(t, "running", s.State)
	assert.Equal(t, "/marlin-nim", s.Names[0])
	assert.Equal(t, uint16(8000), s.Ports[0].PublicPort)
	assert.Equal(t, "true", s.Labels["marlin.managed"])
	assert.Equal(t, "nim", s.Labels["marlin.mode"])
}

func TestNerdctlPSToSummaryExited(t *testing.T) {
	ps := nerdctlPS{ID: "dead", Status: "Exited (0) 5 hours ago"}
	s := ps.toSummary()
	assert.Equal(t, "exited", s.State)
}

func TestNerdctlPSToSummaryNoPort(t *testing.T) {
	ps := nerdctlPS{ID: "dead", Status: "Up 1 hour", Ports: ""}
	s := ps.toSummary()
	assert.Empty(t, s.Ports)
}

func TestContainerBinaryDockerPreferred(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"docker", "nerdctl"} {
		f, _ := os.Create(dir + "/" + name)
		_ = f.Close()
		_ = os.Chmod(dir+"/"+name, 0o755)
	}
	t.Setenv("PATH", dir)
	bin, err := ContainerBinary()
	require.NoError(t, err)
	assert.Contains(t, bin, "docker")
}

func TestContainerBinaryNerdctlFallback(t *testing.T) {
	dir := t.TempDir()
	f, _ := os.Create(dir + "/nerdctl")
	_ = f.Close()
	_ = os.Chmod(dir+"/nerdctl", 0o755)
	t.Setenv("PATH", dir)
	bin, err := ContainerBinary()
	require.NoError(t, err)
	assert.Contains(t, bin, "nerdctl")
}

func TestContainerBinaryNoneFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := ContainerBinary()
	assert.Error(t, err)
}

func TestFindNerdctl(t *testing.T) {
	dir := t.TempDir()
	f, _ := os.Create(dir + "/nerdctl")
	_ = f.Close()
	_ = os.Chmod(dir+"/nerdctl", 0o755)
	t.Setenv("PATH", dir)
	bin, err := FindNerdctl()
	require.NoError(t, err)
	assert.Contains(t, bin, "nerdctl")
}

func TestFindNerdctlNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := FindNerdctl()
	assert.Error(t, err)
}

func TestNerdctlClientImagePull(t *testing.T) {
	c := newTestNerdctlClient(t, `exit 0`)
	r, err := c.ImagePull(context.Background(), "nvcr.io/nim/meta/llama:latest", dimage.PullOptions{
		RegistryAuth: ngcRegistryAuth("test-key"),
	})
	require.NoError(t, err)
	require.NoError(t, r.Close())
}

func TestNerdctlClientImagePullError(t *testing.T) {
	c := newTestNerdctlClient(t, `echo "pull failed" >&2; exit 1`)
	_, err := c.ImagePull(context.Background(), "nvcr.io/nim/meta/llama:latest", dimage.PullOptions{})
	assert.Error(t, err)
}

func TestNerdctlClientContainerCreate(t *testing.T) {
	c := newTestNerdctlClient(t, `echo "deadbeef1234"`)
	resp, err := c.ContainerCreate(context.Background(),
		&container.Config{
			Image: "nvcr.io/nim/meta/llama:latest",
			Env:   []string{"NGC_API_KEY=test"},
			Labels: map[string]string{"marlin.managed": "true"},
		},
		&container.HostConfig{
			Binds: []string{"/cache:/opt/nim/.cache"},
			Resources: container.Resources{
				DeviceRequests: []container.DeviceRequest{
					{Driver: "nvidia", Count: -1},
				},
			},
			PortBindings: nat.PortMap{
				"8000/tcp": []nat.PortBinding{{HostPort: "8000"}},
			},
		},
		nil, nil, "marlin-nim",
	)
	require.NoError(t, err)
	assert.Equal(t, "deadbeef1234", resp.ID)
}

func TestNerdctlClientContainerStartNoOp(t *testing.T) {
	c := newTestNerdctlClient(t, `exit 1`) // would fail if called
	err := c.ContainerStart(context.Background(), "some-id", container.StartOptions{})
	assert.NoError(t, err)
}

func TestNerdctlClientContainerStop(t *testing.T) {
	timeout := 30
	c := newTestNerdctlClient(t, `exit 0`)
	err := c.ContainerStop(context.Background(), "abc123", container.StopOptions{Timeout: &timeout})
	assert.NoError(t, err)
}

func TestNerdctlClientContainerRemove(t *testing.T) {
	c := newTestNerdctlClient(t, `exit 0`)
	err := c.ContainerRemove(context.Background(), "abc123", container.RemoveOptions{Force: true})
	assert.NoError(t, err)
}

func TestNerdctlClientContainerList(t *testing.T) {
	script := `echo '{"ID":"abc123","Names":"marlin-nim","Image":"nvcr.io/nim/meta/llama:latest","Status":"Up 2 hours","Ports":"0.0.0.0:8000->8000/tcp","Labels":"marlin.managed=true"}'`
	c := newTestNerdctlClient(t, script)
	cs, err := c.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "marlin.managed=true")),
	})
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "abc123", cs[0].ID)
	assert.Equal(t, "running", cs[0].State)
}

func TestNerdctlClientContainerListEmpty(t *testing.T) {
	c := newTestNerdctlClient(t, `exit 0`)
	cs, err := c.ContainerList(context.Background(), container.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, cs)
}

func TestNerdctlClientContainerLogs(t *testing.T) {
	c := newTestNerdctlClient(t, `echo "log line one"; echo "log line two" >&2`)
	r, err := c.ContainerLogs(context.Background(), "abc123", container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	var stdout, stderr strings.Builder
	_, err = stdcopy.StdCopy(&stdout, &stderr, r)
	require.NoError(t, err)
	assert.Contains(t, stdout.String()+stderr.String(), "log line")
}

func TestNerdctlClientContainerLogsWithFollow(t *testing.T) {
	c := newTestNerdctlClient(t, `echo "line"`)
	r, err := c.ContainerLogs(context.Background(), "abc123", container.LogsOptions{
		Follow: true,
		Tail:   "10",
	})
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}

func TestNewNIMProviderNerdctl(t *testing.T) {
	dir := t.TempDir()
	f, _ := os.Create(dir + "/nerdctl")
	_ = f.Close()
	_ = os.Chmod(dir+"/nerdctl", 0o755)
	t.Setenv("PATH", dir)

	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "nerdctl"
	_, err := NewNIMProvider(cfg, "key")
	require.NoError(t, err)
}

func TestNewAdhocRunnerNerdctl(t *testing.T) {
	dir := t.TempDir()
	f, _ := os.Create(dir + "/nerdctl")
	_ = f.Close()
	_ = os.Chmod(dir+"/nerdctl", 0o755)
	t.Setenv("PATH", dir)

	cfg := config.Defaults()
	cfg.Service.ContainerRuntime = "nerdctl"
	_, err := NewAdhocRunner(cfg)
	require.NoError(t, err)
}
