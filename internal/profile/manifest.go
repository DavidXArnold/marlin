// Package profile implements marlin's versioned profile distribution:
// fetching model-profile TOML configs from a GitHub-hosted manifest,
// independent of marlin's own binary release cycle, gated by a schema
// version the local binary declares it understands.
package profile

// Manifest is the top-level structure of profiles/manifest.json in the
// configured profile repository.
type Manifest struct {
	Profiles map[string]ManifestProfile `json:"profiles"`
}

// ManifestProfile lists the available profile_versions for one profile id.
type ManifestProfile struct {
	Versions map[string]ManifestEntryVersion `json:"versions"`
}

// ManifestEntryVersion describes a single published profile_version: which
// schema it was written against, its integrity checksum, and where its TOML
// file lives relative to the configured subdir.
type ManifestEntryVersion struct {
	SchemaVersion int    `json:"schema_version"`
	SHA256        string `json:"sha256"`
	Path          string `json:"path"`
}
