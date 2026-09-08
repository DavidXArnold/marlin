package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectiveSchemaVersionDefaultsToOne(t *testing.T) {
	m := &ModelConfig{}
	assert.Equal(t, 1, m.EffectiveSchemaVersion())
}

func TestEffectiveSchemaVersionExplicit(t *testing.T) {
	m := &ModelConfig{Meta: ProfileMeta{SchemaVersion: 3}}
	assert.Equal(t, 3, m.EffectiveSchemaVersion())
}

// TestModelConfigToBytesOmitsMetaWhenZeroValue is load-bearing for backward
// compatibility (existing local profiles without [meta] must round-trip
// byte-for-byte, i.e. never gain a spurious empty [meta] block).
func TestModelConfigToBytesOmitsMetaWhenZeroValue(t *testing.T) {
	m := &ModelConfig{
		Model: ModelMeta{ID: "some/model", Registry: "huggingface"},
	}
	data, err := ModelConfigToBytes(m)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "[meta]")
}

func TestModelConfigToBytesIncludesMetaWhenSet(t *testing.T) {
	m := &ModelConfig{
		Meta:  ProfileMeta{SchemaVersion: 2, ProfileVersion: "1.3.0"},
		Model: ModelMeta{ID: "some/model", Registry: "huggingface"},
	}
	data, err := ModelConfigToBytes(m)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, "[meta]")
	assert.Contains(t, s, "schema_version = 2")
	assert.Contains(t, s, `profile_version = "1.3.0"`)
}

func TestSaveAndReloadModelRoundTripsMeta(t *testing.T) {
	original := &ModelConfig{
		Meta:  ProfileMeta{SchemaVersion: 2, ProfileVersion: "1.3.0"},
		Model: ModelMeta{ID: "some/model", Registry: "huggingface"},
	}
	path := writeTempModelFile(t, "meta.toml", "")
	require.NoError(t, SaveModel(path, original))

	loaded, err := LoadModel(path)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.Meta.SchemaVersion)
	assert.Equal(t, "1.3.0", loaded.Meta.ProfileVersion)
	assert.Equal(t, 2, loaded.EffectiveSchemaVersion())
}

// TestLoadModelWithoutMetaTable confirms an existing, pre-this-feature profile
// (no [meta] table at all) still loads cleanly and defaults to schema 1.
func TestLoadModelWithoutMetaTable(t *testing.T) {
	path := writeTempModelFile(t, "legacy.toml", sampleModelTOML)
	m, err := LoadModel(path)
	require.NoError(t, err)
	assert.Equal(t, 1, m.EffectiveSchemaVersion())
	assert.Empty(t, m.Meta.ProfileVersion)
	// And it must not have picked up any [meta]-shaped content from elsewhere.
	assert.False(t, strings.Contains(sampleModelTOML, "[meta]"))
}
