package mesh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── PatchOpenAIEndpoint ───────────────────────────────────────────────────────

func TestPatchOpenAIEndpointCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mesh-llm", "config.toml")

	changed, err := PatchOpenAIEndpoint(path, "http://localhost:8000/v1")
	require.NoError(t, err)
	assert.True(t, changed)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "[[plugin]]")
	assert.Contains(t, content, `name = "openai-endpoint"`)
	assert.Contains(t, content, `url = "http://localhost:8000/v1"`)
	assert.Contains(t, content, "optional = true")
	assert.Contains(t, content, "lazy_start = true")
	assert.Contains(t, content, "version = 1")
}

func TestPatchOpenAIEndpointNoOpWhenCorrect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	_, err := PatchOpenAIEndpoint(path, "http://localhost:8000/v1")
	require.NoError(t, err)

	original, _ := os.ReadFile(path)

	changed, err := PatchOpenAIEndpoint(path, "http://localhost:8000/v1")
	require.NoError(t, err)
	assert.False(t, changed)

	after, _ := os.ReadFile(path)
	assert.Equal(t, string(original), string(after))
}

func TestPatchOpenAIEndpointUpdatesURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	_, err := PatchOpenAIEndpoint(path, "http://localhost:8000/v1")
	require.NoError(t, err)

	changed, err := PatchOpenAIEndpoint(path, "http://localhost:9000/v1")
	require.NoError(t, err)
	assert.True(t, changed)

	data, _ := os.ReadFile(path)
	assert.Contains(t, string(data), `url = "http://localhost:9000/v1"`)
	assert.NotContains(t, string(data), `url = "http://localhost:8000/v1"`)
}

func TestPatchOpenAIEndpointAppendsToExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	existing := `version = 1

[defaults.hardware]
gpu_layers = -1

[[plugin]]
name = "some-other-plugin"
url = "http://other:9999/v1"
`
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

	changed, err := PatchOpenAIEndpoint(path, "http://localhost:8000/v1")
	require.NoError(t, err)
	assert.True(t, changed)

	data, _ := os.ReadFile(path)
	content := string(data)
	// Original content preserved.
	assert.Contains(t, content, "some-other-plugin")
	assert.Contains(t, content, "gpu_layers = -1")
	// New block appended.
	assert.Contains(t, content, `name = "openai-endpoint"`)
	assert.Contains(t, content, `url = "http://localhost:8000/v1"`)
}

func TestPatchOpenAIEndpointPreservesOtherContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	existing := `version = 1

[gpu]
device = "cuda"

[[plugin]]
name = "openai-endpoint"
url = "http://old:8000/v1"

[plugin.startup]
optional = true
lazy_start = true

[[models]]
model = "Qwen/Qwen3-8B:Q4_K_M"
`
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

	changed, err := PatchOpenAIEndpoint(path, "http://new:9000/v1")
	require.NoError(t, err)
	assert.True(t, changed)

	data, _ := os.ReadFile(path)
	content := string(data)
	assert.Contains(t, content, `device = "cuda"`)
	assert.Contains(t, content, "Qwen/Qwen3-8B:Q4_K_M")
	assert.Contains(t, content, `url = "http://new:9000/v1"`)
	assert.NotContains(t, content, "http://old:8000/v1")
}

// ── RemoveOpenAIEndpoint ──────────────────────────────────────────────────────

func TestRemoveOpenAIEndpointNotPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("version = 1\n"), 0o644))

	changed, err := RemoveOpenAIEndpoint(path)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestRemoveOpenAIEndpointMissingFile(t *testing.T) {
	changed, err := RemoveOpenAIEndpoint(filepath.Join(t.TempDir(), "nonexistent.toml"))
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestRemoveOpenAIEndpointRemovesBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	_, err := PatchOpenAIEndpoint(path, "http://localhost:8000/v1")
	require.NoError(t, err)

	changed, err := RemoveOpenAIEndpoint(path)
	require.NoError(t, err)
	assert.True(t, changed)

	data, _ := os.ReadFile(path)
	assert.NotContains(t, string(data), "openai-endpoint")
}

func TestRemoveOpenAIEndpointPreservesOtherBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	existing := `version = 1

[[plugin]]
name = "openai-endpoint"
url = "http://localhost:8000/v1"

[plugin.startup]
optional = true

[[plugin]]
name = "other"
url = "http://other:9000/v1"
`
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

	changed, err := RemoveOpenAIEndpoint(path)
	require.NoError(t, err)
	assert.True(t, changed)

	data, _ := os.ReadFile(path)
	content := string(data)
	assert.NotContains(t, content, "openai-endpoint")
	assert.Contains(t, content, "other")
}

// ── PatchJoinToken ────────────────────────────────────────────────────────────

func TestPatchJoinTokenCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mesh-llm", "config.toml")

	require.NoError(t, PatchJoinToken(path, "tok123"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `join_token = "tok123"`)
}

func TestPatchJoinTokenUpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := "version = 1\n\n[owner_control]\njoin_token = \"old\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	require.NoError(t, PatchJoinToken(path, "new-token"))

	data, _ := os.ReadFile(path)
	assert.Contains(t, string(data), `join_token = "new-token"`)
	assert.NotContains(t, string(data), `"old"`)
}

func TestPatchJoinTokenAppendsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	require.NoError(t, os.WriteFile(path, []byte("version = 1\n"), 0o644))
	require.NoError(t, PatchJoinToken(path, "mytoken"))

	data, _ := os.ReadFile(path)
	assert.Contains(t, string(data), "[owner_control]")
	assert.Contains(t, string(data), `join_token = "mytoken"`)
}

// ── findOpenAIBlock ───────────────────────────────────────────────────────────

func TestFindOpenAIBlockFound(t *testing.T) {
	content := `version = 1

[[plugin]]
name = "openai-endpoint"
url = "http://localhost:8000/v1"
`
	lines := strings.Split(content, "\n")
	start, end, urlLine := findOpenAIBlock(lines)
	assert.GreaterOrEqual(t, start, 0)
	assert.Greater(t, end, start)
	assert.GreaterOrEqual(t, urlLine, 0)
	assert.Equal(t, `url = "http://localhost:8000/v1"`, strings.TrimSpace(lines[urlLine]))
}

func TestFindOpenAIBlockNotFound(t *testing.T) {
	content := "version = 1\n\n[[plugin]]\nname = \"other\"\n"
	lines := strings.Split(content, "\n")
	start, end, urlLine := findOpenAIBlock(lines)
	assert.Equal(t, -1, start)
	assert.Equal(t, -1, end)
	assert.Equal(t, -1, urlLine)
}

func TestFindOpenAIBlockAmongMultiple(t *testing.T) {
	content := `version = 1

[[plugin]]
name = "first"
url = "http://first"

[[plugin]]
name = "openai-endpoint"
url = "http://localhost:8000/v1"

[[plugin]]
name = "last"
url = "http://last"
`
	lines := strings.Split(content, "\n")
	start, end, urlLine := findOpenAIBlock(lines)
	assert.GreaterOrEqual(t, start, 0)
	// The target block must be between first and last.
	assert.Greater(t, end, start)
	assert.Contains(t, lines[start], "[[plugin]]")
	assert.Contains(t, lines[urlLine], "localhost:8000")
}
