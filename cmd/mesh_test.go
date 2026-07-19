package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/mesh"
)

// writeMeshCfg writes a marlin config.toml wiring mesh fields to the given
// management URL and mesh config path, then sets cfgFile for the test.
// Returns cleanup (restores cfgFile).
func writeMeshCfg(t *testing.T, managementURL, meshCfgPath string) (restore func()) {
	t.Helper()
	dir := t.TempDir()
	cfgContent := fmt.Sprintf(`
[mesh]
management_url = %q
inference_url  = "http://localhost:9337"
systemd_unit   = "mesh-llm-test"
config_path    = %q
mesh_bin       = "true"
auto_register  = false
`, managementURL, meshCfgPath)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))
	old := cfgFile
	cfgFile = cfgPath
	return func() { cfgFile = old }
}

// stubMeshSvcManager implements meshSvcManager for tests.
type stubMeshSvcMgr struct {
	active   bool
	startErr error
	stopErr  error
}

func (s *stubMeshSvcMgr) Start(_ context.Context) error            { return s.startErr }
func (s *stubMeshSvcMgr) Stop(_ context.Context) error             { return s.stopErr }
func (s *stubMeshSvcMgr) IsActive(_ context.Context) (bool, error) { return s.active, nil }

// meshAPIServer starts an httptest server that returns info on GET /api/runtime
// and 200 on POST /api/runtime/control/apply-config.
func meshAPIServer(t *testing.T, info *mesh.RuntimeInfo) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/runtime":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(info)
		case "/api/runtime/control/apply-config":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// ── mesh status ───────────────────────────────────────────────────────────────

func TestMeshStatusRunning(t *testing.T) {
	info := &mesh.RuntimeInfo{
		Peers: []mesh.PeerInfo{
			{ID: "peer1abc", Addr: "192.168.1.2", Models: []string{"qwen3-8b"}},
		},
		Models: []mesh.ModelInfo{
			{Ref: "/models/m.gguf", State: "ready"},
		},
	}
	srv := meshAPIServer(t, info)
	defer srv.Close()

	restore := writeMeshCfg(t, srv.URL, filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	origSvc := newMeshSvcManager
	newMeshSvcManager = func(_ string) meshSvcManager { return &stubMeshSvcMgr{active: true} }
	defer func() { newMeshSvcManager = origSvc }()

	var buf bytes.Buffer
	meshStatusCmd.SetOut(&buf)
	meshStatusCmd.SetErr(&buf)
	require.NoError(t, runMeshStatus(meshStatusCmd, nil))

	out := buf.String()
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "1 connected")
	assert.Contains(t, out, "peer1abc")
	assert.Contains(t, out, "1 loaded")
	assert.Contains(t, out, "/models/m.gguf")
}

func TestMeshStatusNotReachable(t *testing.T) {
	restore := writeMeshCfg(t, "http://127.0.0.1:19337", filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	origSvc := newMeshSvcManager
	newMeshSvcManager = func(_ string) meshSvcManager { return &stubMeshSvcMgr{active: false} }
	defer func() { newMeshSvcManager = origSvc }()

	var buf bytes.Buffer
	meshStatusCmd.SetOut(&buf)
	meshStatusCmd.SetErr(&buf)
	require.NoError(t, runMeshStatus(meshStatusCmd, nil))
	assert.Contains(t, buf.String(), "not reachable")
}

// ── mesh peers ────────────────────────────────────────────────────────────────

func TestMeshPeersListed(t *testing.T) {
	info := &mesh.RuntimeInfo{
		Peers: []mesh.PeerInfo{
			{ID: "aabbcc", Addr: "10.0.0.1", Models: []string{"llama3"}},
			{ID: "ddeeff", Addr: "10.0.0.2"},
		},
	}
	srv := meshAPIServer(t, info)
	defer srv.Close()

	restore := writeMeshCfg(t, srv.URL, filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	var buf bytes.Buffer
	meshPeersCmd.SetOut(&buf)
	meshPeersCmd.SetErr(&buf)
	require.NoError(t, runMeshPeers(meshPeersCmd, nil))
	out := buf.String()
	assert.Contains(t, out, "aabbcc")
	assert.Contains(t, out, "10.0.0.1")
	assert.Contains(t, out, "llama3")
}

func TestMeshPeersNoPeers(t *testing.T) {
	info := &mesh.RuntimeInfo{}
	srv := meshAPIServer(t, info)
	defer srv.Close()

	restore := writeMeshCfg(t, srv.URL, filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	var buf bytes.Buffer
	meshPeersCmd.SetOut(&buf)
	meshPeersCmd.SetErr(&buf)
	require.NoError(t, runMeshPeers(meshPeersCmd, nil))
	assert.Contains(t, buf.String(), "no peers")
}

func TestMeshPeersNotRunning(t *testing.T) {
	restore := writeMeshCfg(t, "http://127.0.0.1:19337", filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	err := runMeshPeers(meshPeersCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

// ── mesh start ────────────────────────────────────────────────────────────────

func TestMeshStartAlreadyRunning(t *testing.T) {
	restore := writeMeshCfg(t, "http://127.0.0.1:19337", filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	origSvc := newMeshSvcManager
	newMeshSvcManager = func(_ string) meshSvcManager { return &stubMeshSvcMgr{active: true} }
	defer func() { newMeshSvcManager = origSvc }()

	var buf bytes.Buffer
	meshStartCmd.SetOut(&buf)
	require.NoError(t, runMeshStart(meshStartCmd, nil))
	assert.Contains(t, buf.String(), "already running")
}

func TestMeshStartSuccess(t *testing.T) {
	restore := writeMeshCfg(t, "http://127.0.0.1:19337", filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	origSvc := newMeshSvcManager
	newMeshSvcManager = func(_ string) meshSvcManager { return &stubMeshSvcMgr{active: false} }
	defer func() { newMeshSvcManager = origSvc }()

	var buf bytes.Buffer
	meshStartCmd.SetOut(&buf)
	require.NoError(t, runMeshStart(meshStartCmd, nil))
	assert.Contains(t, buf.String(), "started")
}

func TestMeshStartWithJoinToken(t *testing.T) {
	meshCfgPath := filepath.Join(t.TempDir(), "mesh.toml")
	restore := writeMeshCfg(t, "http://127.0.0.1:19337", meshCfgPath)
	defer restore()

	origSvc := newMeshSvcManager
	newMeshSvcManager = func(_ string) meshSvcManager { return &stubMeshSvcMgr{active: false} }
	defer func() { newMeshSvcManager = origSvc }()

	require.NoError(t, meshStartCmd.Flags().Set("join", "mytoken123"))
	defer func() { _ = meshStartCmd.Flags().Set("join", "") }()

	var buf bytes.Buffer
	meshStartCmd.SetOut(&buf)
	require.NoError(t, runMeshStart(meshStartCmd, nil))

	data, err := os.ReadFile(meshCfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "mytoken123")
}

// ── mesh stop ─────────────────────────────────────────────────────────────────

func TestMeshStopSuccess(t *testing.T) {
	restore := writeMeshCfg(t, "http://127.0.0.1:19337", filepath.Join(t.TempDir(), "mesh.toml"))
	defer restore()

	origSvc := newMeshSvcManager
	newMeshSvcManager = func(_ string) meshSvcManager { return &stubMeshSvcMgr{} }
	defer func() { newMeshSvcManager = origSvc }()

	var buf bytes.Buffer
	meshStopCmd.SetOut(&buf)
	require.NoError(t, runMeshStop(meshStopCmd, nil))
	assert.Contains(t, buf.String(), "stopped")
}

// ── mesh push-config ──────────────────────────────────────────────────────────

func TestMeshPushConfigBinaryNotFound(t *testing.T) {
	meshCfgPath := filepath.Join(t.TempDir(), "mesh.toml")
	require.NoError(t, os.WriteFile(meshCfgPath, []byte("version = 1\n"), 0o644))

	dir := t.TempDir()
	cfgContent := fmt.Sprintf(`
[mesh]
management_url = "http://127.0.0.1:19337"
config_path    = %q
mesh_bin       = "mesh-llm-does-not-exist-xyzzy"
`, meshCfgPath)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))
	old := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = old }()

	err := runMeshPushConfig(meshPushConfigCmd, []string{"sometoken"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMeshPushConfigCallsCLI(t *testing.T) {
	meshCfgPath := filepath.Join(t.TempDir(), "mesh.toml")
	require.NoError(t, os.WriteFile(meshCfgPath, []byte("version = 1\n"), 0o644))

	// Use "true" as a fake binary that exists on PATH.
	dir := t.TempDir()
	cfgContent := fmt.Sprintf(`
[mesh]
management_url = "http://127.0.0.1:19337"
config_path    = %q
mesh_bin       = "true"
`, meshCfgPath)
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgContent), 0o644))
	old := cfgFile
	cfgFile = cfgPath
	defer func() { cfgFile = old }()

	origExec := meshExecFunc
	var calls [][]string
	meshExecFunc = func(_ context.Context, bin string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{bin}, args...))
		if len(args) >= 2 && args[1] == "get-config" {
			return []byte(`{"revision":3}`), nil
		}
		return []byte{}, nil
	}
	defer func() { meshExecFunc = origExec }()

	var buf bytes.Buffer
	meshPushConfigCmd.SetOut(&buf)
	require.NoError(t, runMeshPushConfig(meshPushConfigCmd, []string{"tok-abc"}))

	require.Len(t, calls, 2)
	assert.Contains(t, calls[0], "get-config")
	assert.Contains(t, calls[1], "apply-config")
	assert.Contains(t, calls[1], "--expected-revision")
	assert.Contains(t, calls[1], "3")
	assert.Contains(t, calls[1], "tok-abc")
	assert.Contains(t, buf.String(), "revision 3")
}

// ── meshAutoRegisterFunc ──────────────────────────────────────────────────────

func TestMeshAutoRegisterWritesConfig(t *testing.T) {
	meshCfgPath := filepath.Join(t.TempDir(), "mesh.toml")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Mesh.ConfigPath = meshCfgPath
	cfg.Mesh.ManagementURL = srv.URL
	cfg.Mesh.AutoRegister = true
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 8000

	var errBuf bytes.Buffer
	meshStatusCmd.SetErr(&errBuf)
	meshAutoRegisterFunc(meshStatusCmd, cfg, config.ProviderVLLM)

	data, err := os.ReadFile(meshCfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "openai-endpoint")
	assert.Contains(t, string(data), "localhost:8000")
}

func TestMeshAutoRegisterSkipsWhenDisabled(t *testing.T) {
	meshCfgPath := filepath.Join(t.TempDir(), "mesh.toml")
	cfg := config.Defaults()
	cfg.Mesh.ConfigPath = meshCfgPath
	cfg.Mesh.AutoRegister = false

	meshAutoRegisterFunc(meshStatusCmd, cfg, config.ProviderVLLM)

	_, err := os.ReadFile(meshCfgPath)
	assert.True(t, os.IsNotExist(err), "config file should not be created when disabled")
}

func TestMeshAutoRegisterSkipsForMeshProvider(t *testing.T) {
	meshCfgPath := filepath.Join(t.TempDir(), "mesh.toml")
	cfg := config.Defaults()
	cfg.Mesh.ConfigPath = meshCfgPath
	cfg.Mesh.AutoRegister = true

	meshAutoRegisterFunc(meshStatusCmd, cfg, config.ProviderMesh)

	_, err := os.ReadFile(meshCfgPath)
	assert.True(t, os.IsNotExist(err), "mesh provider should not self-register")
}

// ── meshStatusSection (marlin status integration) ─────────────────────────────

func TestMeshStatusSectionRunning(t *testing.T) {
	info := &mesh.RuntimeInfo{
		Peers: []mesh.PeerInfo{{ID: "p1"}, {ID: "p2"}},
	}
	srv := meshAPIServer(t, info)
	defer srv.Close()

	cfg := config.Defaults()
	cfg.Mesh.ManagementURL = srv.URL
	cfg.Mesh.InferenceURL = "http://localhost:9337"

	section := meshStatusSection(context.Background(), cfg)
	assert.Contains(t, section, "2 peer(s)")
	assert.Contains(t, section, "localhost:9337")
}

func TestMeshStatusSectionNotRunning(t *testing.T) {
	cfg := config.Defaults()
	cfg.Mesh.ManagementURL = "http://127.0.0.1:19337"

	section := meshStatusSection(context.Background(), cfg)
	assert.Empty(t, section)
}

// ── joinStrings helper ────────────────────────────────────────────────────────

func TestJoinStrings(t *testing.T) {
	assert.Equal(t, "", joinStrings(nil, ", "))
	assert.Equal(t, "a", joinStrings([]string{"a"}, ", "))
	assert.Equal(t, "a, b, c", joinStrings([]string{"a", "b", "c"}, ", "))
}
