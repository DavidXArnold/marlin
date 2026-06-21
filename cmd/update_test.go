package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/state"
)

// mockNIMProv implements provider.Provider and nimDigester so update tests can
// inject full NIM behaviour without a real container runtime.
type mockNIMProv struct {
	pullErr   error
	digest    string
	digestErr error
	switchErr error
}

func (m *mockNIMProv) PullImage(_ context.Context, _ string) error       { return m.pullErr }
func (m *mockNIMProv) GetDigest(_ context.Context, _ string) (string, error) {
	return m.digest, m.digestErr
}
func (m *mockNIMProv) Switch(_ context.Context, _ string) error              { return m.switchErr }
func (m *mockNIMProv) Stop(_ context.Context) error                           { return nil }
func (m *mockNIMProv) Status(_ context.Context) (*provider.Status, error)    { return &provider.Status{}, nil }
func (m *mockNIMProv) Logs(_ context.Context, _ io.Writer, _ bool, _ int) error { return nil }

// updateEnv sets up a temp dir with a config pointing at it, a NIM model file,
// and optionally a saved state. Returns models dir, cleanup func.
func updateEnv(t *testing.T, slug string, st *state.State) string {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	require.NoError(t, os.MkdirAll(modelsDir, 0o755))

	cfgContent := fmt.Sprintf(`[paths]
models_dir = %q
global_models_dir = %q
state_file = %q
secrets_env = %q
active_symlink = %q
`, modelsDir, modelsDir,
		filepath.Join(dir, "state.toml"),
		filepath.Join(dir, "secrets.env"),
		filepath.Join(dir, "model.env"),
	)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))

	if st != nil {
		require.NoError(t, state.Save(filepath.Join(dir, "state.toml"), st))
	}

	old := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = old })

	return modelsDir
}

func writeNIMModelForUpdate(t *testing.T, dir, slug string) {
	t.Helper()
	m := &config.ModelConfig{
		Model: config.ModelMeta{
			Type:  config.ProviderNIM,
			Image: "nvcr.io/nim/meta/llama:latest",
		},
	}
	require.NoError(t, config.SaveModel(filepath.Join(dir, slug+".toml"), m))
}

// --- tests ---

func TestUpdateNoActiveModel(t *testing.T) {
	updateEnv(t, "", nil)
	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active model")
}

func TestUpdateVLLMModelRejected(t *testing.T) {
	st := &state.State{ActiveModel: "qwen-72b", ActiveProvider: config.ProviderVLLM}
	modelsDir := updateEnv(t, "qwen-72b", st)
	m := &config.ModelConfig{Model: config.ModelMeta{Type: config.ProviderVLLM, ID: "some/model"}}
	require.NoError(t, config.SaveModel(filepath.Join(modelsDir, "qwen-72b.toml"), m))

	injectProvider(t, &mockNIMProv{})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nim providers only")
}

func TestUpdateMissingModel(t *testing.T) {
	st := &state.State{ActiveModel: "ghost", ActiveProvider: config.ProviderNIM}
	updateEnv(t, "ghost", st)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestUpdateAlreadyUpToDate(t *testing.T) {
	digest := "sha256:abcdef0123456789abcdef0123456789abcdef01"
	st := &state.State{
		ActiveModel:    "llama-nim",
		ActiveProvider: config.ProviderNIM,
		PinnedDigest:   digest,
	}
	modelsDir := updateEnv(t, "llama-nim", st)
	writeNIMModelForUpdate(t, modelsDir, "llama-nim")

	mock := &mockNIMProv{digest: digest}
	injectProvider(t, mock)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "already up to date")
}

func TestUpdateNewDigestTriggerSwitch(t *testing.T) {
	oldDigest := "sha256:aaaa0000000000000000000000000000000000000000"
	newDigest := "sha256:bbbb0000000000000000000000000000000000000000"
	st := &state.State{
		ActiveModel:    "llama-nim",
		ActiveProvider: config.ProviderNIM,
		PinnedDigest:   oldDigest,
	}
	modelsDir := updateEnv(t, "llama-nim", st)
	writeNIMModelForUpdate(t, modelsDir, "llama-nim")

	mock := &mockNIMProv{digest: newDigest}
	injectProvider(t, mock)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "update available")
	assert.Contains(t, buf.String(), "updated to")
}

func TestUpdatePullError(t *testing.T) {
	st := &state.State{ActiveModel: "llama-nim", ActiveProvider: config.ProviderNIM}
	modelsDir := updateEnv(t, "llama-nim", st)
	writeNIMModelForUpdate(t, modelsDir, "llama-nim")

	injectProvider(t, &mockNIMProv{pullErr: fmt.Errorf("network timeout")})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pulling")
}

func TestUpdateSwitchError(t *testing.T) {
	oldDigest := "sha256:aaaa"
	newDigest := "sha256:bbbb0000000000000000000000000000000000000000"
	st := &state.State{
		ActiveModel:    "llama-nim",
		ActiveProvider: config.ProviderNIM,
		PinnedDigest:   oldDigest,
	}
	modelsDir := updateEnv(t, "llama-nim", st)
	writeNIMModelForUpdate(t, modelsDir, "llama-nim")

	injectProvider(t, &mockNIMProv{digest: newDigest, switchErr: fmt.Errorf("container start failed")})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	err := runUpdate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container start failed")
}

func TestUpdateExplicitSlugArg(t *testing.T) {
	digest := "sha256:cccc0000000000000000000000000000000000000000"
	st := &state.State{ActiveModel: "", ActiveProvider: ""}
	modelsDir := updateEnv(t, "", st)
	writeNIMModelForUpdate(t, modelsDir, "llama-nim")

	mock := &mockNIMProv{digest: digest}
	injectProvider(t, mock)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	// Pass slug as arg — no active model needed.
	err := runUpdate(cmd, []string{"llama-nim"})
	require.NoError(t, err)
	// digest != stored (""), so a switch is triggered.
	assert.Contains(t, buf.String(), "restarting")
}

// --- shortDigest ---

func TestShortDigestLong(t *testing.T) {
	d := "sha256:abcdef012345678901234567890123456789"
	assert.Equal(t, "sha256:abcdef012345…", shortDigest(d))
}

func TestShortDigestShort(t *testing.T) {
	assert.Equal(t, "sha256:abc", shortDigest("sha256:abc"))
}

func TestShortDigestNoPrefix(t *testing.T) {
	assert.Equal(t, "sha256:abc", shortDigest("abc"))
}

func TestShortDigestEmpty(t *testing.T) {
	assert.Equal(t, "sha256:", shortDigest(""))
}

// keep os import used
var _ = os.Getenv
