package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyChecksumMatch(t *testing.T) {
	data := []byte("hello world")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])

	err := VerifyChecksum("some-profile", "1.0.0", data, hexSum)
	require.NoError(t, err)
}

func TestVerifyChecksumMatchCaseInsensitive(t *testing.T) {
	data := []byte("hello world")
	sum := sha256.Sum256(data)
	upper := strings.ToUpper(hex.EncodeToString(sum[:]))

	err := VerifyChecksum("some-profile", "1.0.0", data, upper)
	require.NoError(t, err)
}

func TestVerifyChecksumMismatch(t *testing.T) {
	data := []byte("hello world")
	err := VerifyChecksum("some-profile", "1.0.0", data, "deadbeef")
	require.Error(t, err)

	var mismatch *ChecksumMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, "some-profile", mismatch.ProfileID)
	assert.Equal(t, "1.0.0", mismatch.Version)
	assert.Equal(t, "deadbeef", mismatch.Expected)
	assert.NotEqual(t, "deadbeef", mismatch.Actual)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Contains(t, err.Error(), "deadbeef")
}
