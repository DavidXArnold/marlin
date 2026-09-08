package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/profile"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

const compatProfileTOML = `[meta]
schema_version = 1
profile_version = "1.0.0"

[model]
type = "vllm"
id = "nvidia/Llama-3.1-8B-Instruct-NVFP4"
registry = "huggingface"
status = "untested"

[serve]
tool_call_parser = "llama3_json"
served_model_name = ["local", "llama-3.1-8b"]
gpu_memory_utilization = 0.9
`

const incompatProfileTOML = `[meta]
schema_version = 99
profile_version = "1.0.0"

[model]
type = "vllm"
id = "some/moe-model"
registry = "huggingface"
status = "untested"

[serve]
tool_call_parser = "llama3_json"
moe_backend = "triton"
attention_backend = "flashinfer"
`

// profileServerHits wraps a fake profile-repo server so tests can assert how
// many times a given path was requested (for cache-bypass assertions).
type profileServerHits struct {
	srv  *httptest.Server
	hits map[string]int
}

// newFakeProfileServer serves files (a map of URL path -> body) as if they
// were raw.githubusercontent.com content.
func newFakeProfileServer(t *testing.T, files map[string]string) *profileServerHits {
	t.Helper()
	h := &profileServerHits{hits: map[string]int{}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits[r.URL.Path]++
		body, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// installFakeProfileSource swaps newProfileSource to point at h for the
// duration of the test.
func installFakeProfileSource(t *testing.T, h *profileServerHits) {
	t.Helper()
	old := newProfileSource
	newProfileSource = func(cfg *config.Config) *profile.Source {
		return profile.NewSourceWithBase(cfg.Profiles.RepoOwner, cfg.Profiles.RepoName, cfg.Profiles.RepoRef, cfg.Profiles.Subdir, h.srv.URL)
	}
	t.Cleanup(func() { newProfileSource = old })
}

// buildManifestJSON constructs a manifest.json body for the given
// path -> body content, using cfg.Profiles' owner/name/ref/subdir to derive
// the URL prefix every fixture is served under.
func manifestPathPrefix(cfg *config.Config) string {
	return fmt.Sprintf("/%s/%s/%s/%s", cfg.Profiles.RepoOwner, cfg.Profiles.RepoName, cfg.Profiles.RepoRef, cfg.Profiles.Subdir)
}

// standardTestManifest returns manifest JSON plus the server's file map for
// a fixed set of fixture profiles used across most test cases below.
func standardTestManifest(t *testing.T, cfg *config.Config) (manifestJSON string, files map[string]string) {
	t.Helper()
	prefix := manifestPathPrefix(cfg)
	files = map[string]string{
		prefix + "/compat-profile/1.0.0.toml":   compatProfileTOML,
		prefix + "/incompat-profile/1.0.0.toml": incompatProfileTOML,
	}

	m := profile.Manifest{
		Profiles: map[string]profile.ManifestProfile{
			"compat-profile": {
				Versions: map[string]profile.ManifestEntryVersion{
					"1.0.0": {SchemaVersion: 1, SHA256: sha256Hex(compatProfileTOML), Path: "compat-profile/1.0.0.toml"},
				},
			},
			"incompat-profile": {
				Versions: map[string]profile.ManifestEntryVersion{
					"1.0.0": {SchemaVersion: 99, SHA256: sha256Hex(incompatProfileTOML), Path: "incompat-profile/1.0.0.toml"},
				},
			},
		},
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	files[prefix+"/manifest.json"] = string(b)
	return string(b), files
}

// --- AC1: compatible profile pulls cleanly, no schema warning ---

func TestProfilePullCompatible(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")

	require.NoError(t, runProfilePull(cmd, []string{"compat-profile"}))
	out := buf.String()
	assert.Contains(t, out, "pulled compat-profile@1.0.0")
	assert.NotContains(t, out, "Warning")

	loaded, err := config.LoadModel(filepath.Join(cfg.Paths.ModelsDir, "compat-profile.toml"))
	require.NoError(t, err)
	assert.Equal(t, "nvidia/Llama-3.1-8B-Instruct-NVFP4", loaded.Model.ID)
	assert.Equal(t, 1, loaded.Meta.SchemaVersion)
}

// --- AC2: incompatible profile, no override -> exact spec error, no file written ---

func TestProfilePullIncompatibleNoOverride(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")

	err = runProfilePull(cmd, []string{"incompat-profile"})
	require.Error(t, err)
	want := `profile "incompat-profile" requires schema_version 99, this Marlin build supports up to 1.` + "\n" +
		"Upgrade Marlin, or re-run with --allow-newer-schema to force (unsupported fields will be ignored)."
	assert.Equal(t, want, err.Error())

	_, statErr := os.Stat(filepath.Join(cfg.Paths.ModelsDir, "incompat-profile.toml"))
	assert.True(t, os.IsNotExist(statErr), "profile file must not be written when the schema gate refuses")

	// The gate must fire before the profile file is even downloaded.
	prefix := manifestPathPrefix(cfg)
	assert.Zero(t, h.hits[prefix+"/incompat-profile/1.0.0.toml"])
}

// --- AC3: same profile + --allow-newer-schema -> exact warning, only understood fields persisted ---

func TestProfilePullIncompatibleWithOverride(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, cmd.Flags().Set("allow-newer-schema", "true"))

	require.NoError(t, runProfilePull(cmd, []string{"incompat-profile"}))
	out := buf.String()
	assert.Contains(t, out,
		"Warning: profile uses fields unsupported by this Marlin build and they will be ignored: [serve].attention_backend, [serve].moe_backend")
	assert.Contains(t, out, "pulled incompat-profile@1.0.0")

	destPath := filepath.Join(cfg.Paths.ModelsDir, "incompat-profile.toml")
	loaded, err := config.LoadModel(destPath)
	require.NoError(t, err)
	assert.Equal(t, "some/moe-model", loaded.Model.ID)
	assert.Equal(t, "llama3_json", loaded.Serve.ToolCallParser)

	raw, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "moe_backend")
	assert.NotContains(t, string(raw), "attention_backend")
}

func TestProfilePullAllowNewerSchemaSilentWhenAlreadyCompatible(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, cmd.Flags().Set("allow-newer-schema", "true"))

	require.NoError(t, runProfilePull(cmd, []string{"compat-profile"}))
	assert.NotContains(t, buf.String(), "Warning")
}

// --- AC4: `profile list` visibly flags the incompatible profile ---

func TestProfileListFlagsIncompatible(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("refresh", false, "")

	require.NoError(t, runProfileList(cmd, nil))
	lines := strings.Split(buf.String(), "\n")

	var compatLine, incompatLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "compat-profile") {
			compatLine = l
		}
		if strings.HasPrefix(l, "incompat-profile") {
			incompatLine = l
		}
	}
	require.NotEmpty(t, compatLine)
	require.NotEmpty(t, incompatLine)
	assert.NotContains(t, compatLine, "requires marlin upgrade")
	assert.Contains(t, incompatLine, "requires marlin upgrade")
	assert.Contains(t, incompatLine, "needs schema 99, this build supports 1")
}

func TestProfileListJSON(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	oldFormat := outputFormat
	outputFormat = "json"
	defer func() { outputFormat = oldFormat }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, runProfileList(cmd, nil))

	var items []profileListItem
	require.NoError(t, json.Unmarshal(buf.Bytes(), &items))
	require.Len(t, items, 2)
}

// --- AC5: version pinning ---

func TestProfilePullPinnedVersion(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")

	require.NoError(t, runProfilePull(cmd, []string{"compat-profile@1.0.0"}))
	assert.Contains(t, buf.String(), "pulled compat-profile@1.0.0")
}

func TestProfilePullPinnedVersionMissing(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")

	err = runProfilePull(cmd, []string{"compat-profile@9.9.9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no version "9.9.9"`)
	assert.Contains(t, err.Error(), "1.0.0")
}

// --- AC6: checksum mismatch rejected before applying ---

func TestProfilePullChecksumMismatch(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	prefix := manifestPathPrefix(cfg)
	files := map[string]string{
		prefix + "/bad-checksum-profile/1.0.0.toml": compatProfileTOML,
	}
	m := profile.Manifest{
		Profiles: map[string]profile.ManifestProfile{
			"bad-checksum-profile": {
				Versions: map[string]profile.ManifestEntryVersion{
					"1.0.0": {SchemaVersion: 1, SHA256: "0000000000000000000000000000000000000000000000000000000000000000", Path: "bad-checksum-profile/1.0.0.toml"},
				},
			},
		},
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	files[prefix+"/manifest.json"] = string(b)

	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")

	err = runProfilePull(cmd, []string{"bad-checksum-profile"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Contains(t, err.Error(), "expected 000000")

	_, statErr := os.Stat(filepath.Join(cfg.Paths.ModelsDir, "bad-checksum-profile.toml"))
	assert.True(t, os.IsNotExist(statErr))
}

// --- profile-not-found ---

func TestProfilePullNotFound(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("allow-newer-schema", false, "")
	cmd.Flags().Bool("global", false, "")
	cmd.Flags().Bool("refresh", false, "")

	err = runProfilePull(cmd, []string{"nonexistent-profile"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"nonexistent-profile" not found`)
}

// --- manifest cache wiring at the cmd layer ---

func TestProfilePullReusesManifestCache(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)
	prefix := manifestPathPrefix(cfg)

	pull := func() {
		var buf bytes.Buffer
		cmd := cmdWithContext(&buf)
		cmd.Flags().Bool("allow-newer-schema", false, "")
		cmd.Flags().Bool("global", false, "")
		cmd.Flags().Bool("refresh", false, "")
		require.NoError(t, runProfilePull(cmd, []string{"compat-profile"}))
	}

	pull()
	assert.Equal(t, 1, h.hits[prefix+"/manifest.json"])
	pull()
	assert.Equal(t, 1, h.hits[prefix+"/manifest.json"], "second pull within TTL must reuse the cached manifest")
}

func TestProfileListRefreshBypassesCache(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)
	prefix := manifestPathPrefix(cfg)

	list := func(refresh bool) {
		var buf bytes.Buffer
		cmd := cmdWithContext(&buf)
		cmd.Flags().Bool("refresh", false, "")
		if refresh {
			require.NoError(t, cmd.Flags().Set("refresh", "true"))
		}
		require.NoError(t, runProfileList(cmd, nil))
	}

	list(false)
	assert.Equal(t, 1, h.hits[prefix+"/manifest.json"])
	list(true)
	assert.Equal(t, 2, h.hits[prefix+"/manifest.json"], "--refresh must bypass the cache")
}

// --- empty manifest ---

func TestProfileListEmpty(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	prefix := manifestPathPrefix(cfg)
	files := map[string]string{prefix + "/manifest.json": `{"profiles":{}}`}
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, runProfileList(cmd, nil))
	assert.Contains(t, buf.String(), "no profiles found")
}

// --- output format variants ---

func TestProfileListJSONL(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	oldFormat := outputFormat
	outputFormat = "jsonl"
	defer func() { outputFormat = oldFormat }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, runProfileList(cmd, nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	for _, l := range lines {
		var it profileListItem
		require.NoError(t, json.Unmarshal([]byte(l), &it))
	}
}

func TestProfileListPlain(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	oldFormat := outputFormat
	outputFormat = "plain"
	defer func() { outputFormat = oldFormat }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, runProfileList(cmd, nil))

	out := buf.String()
	assert.Contains(t, out, "compat-profile\t1.0.0\t1\tcompatible")
	assert.Contains(t, out, "incompat-profile\t1.0.0\t99\tincompatible")
}

// --- verbosity wiring ---

func TestProfileListVerboseLogging(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()
	cfg, err := globalConfig()
	require.NoError(t, err)

	_, files := standardTestManifest(t, cfg)
	h := newFakeProfileServer(t, files)
	installFakeProfileSource(t, h)

	oldV := Verbosity
	Verbosity = 1
	defer func() { Verbosity = oldV }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("refresh", false, "")
	require.NoError(t, runProfileList(cmd, nil))
	assert.Contains(t, buf.String(), "[profile] GET")
}

// --- splitProfileRef ---

func TestSplitProfileRef(t *testing.T) {
	cases := []struct {
		in, id, version string
	}{
		{"foo", "foo", ""},
		{"foo@1.0.0", "foo", "1.0.0"},
		{"foo@bar@2.0.0", "foo@bar", "2.0.0"},
	}
	for _, c := range cases {
		id, version := splitProfileRef(c.in)
		assert.Equal(t, c.id, id)
		assert.Equal(t, c.version, version)
	}
}
