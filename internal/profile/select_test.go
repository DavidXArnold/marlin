package profile

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testManifest() *Manifest {
	return &Manifest{
		Profiles: map[string]ManifestProfile{
			"compat-profile": {
				Versions: map[string]ManifestEntryVersion{
					"1.0.0": {SchemaVersion: 1, SHA256: "aaa", Path: "compat-profile/1.0.0.toml"},
					"1.1.0": {SchemaVersion: 1, SHA256: "bbb", Path: "compat-profile/1.1.0.toml"},
				},
			},
			"mixed-profile": {
				Versions: map[string]ManifestEntryVersion{
					"1.0.0": {SchemaVersion: 1, SHA256: "ccc", Path: "mixed-profile/1.0.0.toml"},
					"2.0.0": {SchemaVersion: 3, SHA256: "ddd", Path: "mixed-profile/2.0.0.toml"},
				},
			},
			"all-incompatible-profile": {
				Versions: map[string]ManifestEntryVersion{
					"1.0.0": {SchemaVersion: 5, SHA256: "eee", Path: "all-incompatible-profile/1.0.0.toml"},
				},
			},
		},
	}
}

func TestSelectVersionProfileNotFound(t *testing.T) {
	_, _, err := SelectVersion(testManifest(), "nope", "", false, 1)
	require.Error(t, err)
	var nfErr *ProfileNotFoundError
	require.ErrorAs(t, err, &nfErr)
	assert.Equal(t, "nope", nfErr.ProfileID)
}

func TestSelectVersionUnpinnedAllCompatible(t *testing.T) {
	v, entry, err := SelectVersion(testManifest(), "compat-profile", "", false, 1)
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", v) // highest version, all compatible
	assert.Equal(t, "bbb", entry.SHA256)
}

func TestSelectVersionUnpinnedAutoFallbackToCompatible(t *testing.T) {
	// mixed-profile's overall latest (2.0.0) requires schema 3, unsupported.
	// Unpinned pull without --allow-newer-schema should silently fall back
	// to the highest *compatible* version, 1.0.0 — not error.
	v, entry, err := SelectVersion(testManifest(), "mixed-profile", "", false, 1)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", v)
	assert.Equal(t, "ccc", entry.SHA256)
}

func TestSelectVersionUnpinnedAllowNewerPicksOverallLatest(t *testing.T) {
	v, entry, err := SelectVersion(testManifest(), "mixed-profile", "", true, 1)
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", v)
	assert.Equal(t, "ddd", entry.SHA256)
}

func TestSelectVersionUnpinnedNoCompatibleVersionErrors(t *testing.T) {
	_, _, err := SelectVersion(testManifest(), "all-incompatible-profile", "", false, 1)
	require.Error(t, err)
	var schemaErr *SchemaTooNewError
	require.ErrorAs(t, err, &schemaErr)
	assert.Equal(t, "all-incompatible-profile", schemaErr.ProfileID)
	assert.Equal(t, 5, schemaErr.SchemaVersion)
	assert.Equal(t, 1, schemaErr.Supported)
}

func TestSelectVersionUnpinnedNoCompatibleAllowNewerSucceeds(t *testing.T) {
	v, entry, err := SelectVersion(testManifest(), "all-incompatible-profile", "", true, 1)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", v)
	assert.Equal(t, 5, entry.SchemaVersion)
}

func TestSelectVersionPinnedHit(t *testing.T) {
	v, entry, err := SelectVersion(testManifest(), "mixed-profile", "1.0.0", false, 1)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", v)
	assert.Equal(t, "ccc", entry.SHA256)
}

func TestSelectVersionPinnedIncompatibleNoOverride(t *testing.T) {
	_, _, err := SelectVersion(testManifest(), "mixed-profile", "2.0.0", false, 1)
	require.Error(t, err)
	var schemaErr *SchemaTooNewError
	require.ErrorAs(t, err, &schemaErr)
	assert.Equal(t, 3, schemaErr.SchemaVersion)
}

func TestSelectVersionPinnedIncompatibleWithOverride(t *testing.T) {
	v, entry, err := SelectVersion(testManifest(), "mixed-profile", "2.0.0", true, 1)
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", v)
	assert.Equal(t, 3, entry.SchemaVersion)
}

func TestSelectVersionPinnedMiss(t *testing.T) {
	_, _, err := SelectVersion(testManifest(), "mixed-profile", "9.9.9", false, 1)
	require.Error(t, err)
	var vnfErr *VersionNotFoundError
	require.ErrorAs(t, err, &vnfErr)
	assert.Equal(t, "mixed-profile", vnfErr.ProfileID)
	assert.Equal(t, "9.9.9", vnfErr.Requested)
	assert.Equal(t, []string{"1.0.0", "2.0.0"}, vnfErr.Available)
}

func TestLatestOverall(t *testing.T) {
	v, entry, err := LatestOverall(testManifest(), "mixed-profile")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", v)
	assert.Equal(t, 3, entry.SchemaVersion)
}

func TestLatestOverallProfileNotFound(t *testing.T) {
	_, _, err := LatestOverall(testManifest(), "nope")
	require.Error(t, err)
	assert.True(t, errors.As(err, new(*ProfileNotFoundError)))
}

func TestSchemaTooNewErrorMessage(t *testing.T) {
	err := &SchemaTooNewError{ProfileID: "qwen3.6-35b-a3b-nvfp4", SchemaVersion: 3, Supported: 2}
	want := "profile \"qwen3.6-35b-a3b-nvfp4\" requires schema_version 3, this Marlin build supports up to 2.\n" +
		"Upgrade Marlin, or re-run with --allow-newer-schema to force (unsupported fields will be ignored)."
	assert.Equal(t, want, err.Error())
}
