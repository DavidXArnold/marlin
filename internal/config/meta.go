package config

// SupportedSchemaVersion is the highest model-profile TOML schema version this
// build understands. Bump only when the schema itself changes (fields added,
// removed, or semantically changed) — not for model/content-only changes.
const SupportedSchemaVersion = 1

// ProfileMeta declares versioning information for a distributable model
// profile, as pulled via `marlin profile pull`. Profiles that predate this
// feature have no [meta] table at all; see EffectiveSchemaVersion.
type ProfileMeta struct {
	SchemaVersion  int    `toml:"schema_version"`
	ProfileVersion string `toml:"profile_version"`
}

// EffectiveSchemaVersion returns m.Meta.SchemaVersion, defaulting to 1 when
// the [meta] table (or its schema_version field) is absent — the backward
// compatibility baseline for profiles that predate the [meta] table.
func (m *ModelConfig) EffectiveSchemaVersion() int {
	if m.Meta.SchemaVersion == 0 {
		return 1
	}
	return m.Meta.SchemaVersion
}
