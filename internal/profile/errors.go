package profile

import (
	"fmt"
	"strings"
)

// ProfileNotFoundError is returned when a profile id has no entry at all in
// the manifest.
type ProfileNotFoundError struct {
	ProfileID string
}

func (e *ProfileNotFoundError) Error() string {
	return fmt.Sprintf("profile %q not found in manifest", e.ProfileID)
}

// VersionNotFoundError is returned when a pinned `pull <id>@<version>` names
// a version absent from the manifest.
type VersionNotFoundError struct {
	ProfileID string
	Requested string
	Available []string
}

func (e *VersionNotFoundError) Error() string {
	return fmt.Sprintf("profile %q has no version %q — available versions: %s",
		e.ProfileID, e.Requested, strings.Join(e.Available, ", "))
}

// SchemaTooNewError is returned when the version that would be selected
// requires a schema newer than this build supports, and --allow-newer-schema
// was not passed. Its Error() text is deliberately the exact spec wording —
// cobra's default error printer prepends "Error: ", producing the full
// documented error message verbatim.
type SchemaTooNewError struct {
	ProfileID     string
	SchemaVersion int
	Supported     int
}

func (e *SchemaTooNewError) Error() string {
	return fmt.Sprintf(
		"profile %q requires schema_version %d, this Marlin build supports up to %d.\n"+
			"Upgrade Marlin, or re-run with --allow-newer-schema to force (unsupported fields will be ignored).",
		e.ProfileID, e.SchemaVersion, e.Supported)
}

// ChecksumMismatchError is returned when a downloaded profile file's SHA-256
// doesn't match the manifest's published checksum for that exact version.
type ChecksumMismatchError struct {
	ProfileID string
	Version   string
	Expected  string
	Actual    string
}

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("checksum mismatch for profile %q@%s: expected %s, got %s",
		e.ProfileID, e.Version, e.Expected, e.Actual)
}
