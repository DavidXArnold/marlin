package secrets

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMissing(t *testing.T) {
	m, err := Load("/nonexistent/secrets.env")
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestLoadBasic(t *testing.T) {
	f := tmpFile(t, `
HF_TOKEN=hf_abc123
NGC_API_KEY=nvapi_xyz
# this is a comment
EMPTY_VALUE=

WHITESPACE_KEY = spaced
`)
	m, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "hf_abc123", m["HF_TOKEN"])
	assert.Equal(t, "nvapi_xyz", m["NGC_API_KEY"])
	assert.Equal(t, "", m["EMPTY_VALUE"])
	assert.Equal(t, "spaced", m["WHITESPACE_KEY"])
}

func TestLoadSkipsMalformed(t *testing.T) {
	f := tmpFile(t, "NOEQUALSSIGN\nGOOD=value\n")
	m, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "value", m["GOOD"])
	assert.NotContains(t, m, "NOEQUALSSIGN")
}

func TestLoadSkipsComments(t *testing.T) {
	f := tmpFile(t, "# comment\nKEY=val\n")
	m, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "val", m["KEY"])
	assert.Len(t, m, 1)
}

func TestLoadValueWithEquals(t *testing.T) {
	f := tmpFile(t, "TOKEN=abc=def=ghi\n")
	m, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "abc=def=ghi", m["TOKEN"])
}

func TestLoadPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	f := tmpFile(t, "KEY=val\n")
	require.NoError(t, os.Chmod(f, 0000))
	defer func() { _ = os.Chmod(f, 0644) }()

	_, err := Load(f)
	assert.Error(t, err)
}

// --- Save ---

func TestSaveCreatesNewFile(t *testing.T) {
	path := t.TempDir() + "/secrets.env"
	err := Save(path, map[string]string{"HF_TOKEN": "hf_new"})
	require.NoError(t, err)

	m, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "hf_new", m["HF_TOKEN"])
}

func TestSaveMergesExisting(t *testing.T) {
	f := tmpFile(t, "HF_TOKEN=hf_old\nNGC_API_KEY=nvapi_old\n")
	err := Save(f, map[string]string{"HF_TOKEN": "hf_new"})
	require.NoError(t, err)

	m, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "hf_new", m["HF_TOKEN"])
	assert.Equal(t, "nvapi_old", m["NGC_API_KEY"])
}

func TestSaveRemovesKeyOnEmptyValue(t *testing.T) {
	f := tmpFile(t, "HF_TOKEN=hf_abc\nNGC_API_KEY=nvapi_xyz\n")
	err := Save(f, map[string]string{"HF_TOKEN": ""})
	require.NoError(t, err)

	m, err := Load(f)
	require.NoError(t, err)
	assert.NotContains(t, m, "HF_TOKEN")
	assert.Equal(t, "nvapi_xyz", m["NGC_API_KEY"])
}

func TestSaveNoChanges(t *testing.T) {
	f := tmpFile(t, "HF_TOKEN=hf_abc\n")
	err := Save(f, map[string]string{})
	require.NoError(t, err)

	m, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "hf_abc", m["HF_TOKEN"])
}

func TestSaveFileMode(t *testing.T) {
	path := t.TempDir() + "/secrets.env"
	require.NoError(t, Save(path, map[string]string{"HF_TOKEN": "x"}))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func tmpFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "secrets-*.env")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
