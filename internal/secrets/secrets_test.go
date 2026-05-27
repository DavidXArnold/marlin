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
	defer os.Chmod(f, 0644)

	_, err := Load(f)
	assert.Error(t, err)
}

func tmpFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "secrets-*.env")
	require.NoError(t, err)
	t.Cleanup(func() { os.Remove(f.Name()) })
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()
	return f.Name()
}
