package profile

import (
	"sort"

	"github.com/DavidXArnold/marlin/internal/semver"
)

// SelectVersion resolves which profile_version `marlin profile pull` should
// use for profileID.
//
//   - pinned != ""            -> must match exactly (else *VersionNotFoundError);
//     the schema gate is applied to that pinned version alone.
//   - pinned == "", !allowNewer -> the highest profile_version whose
//     schema_version <= supported. If no version qualifies, fails with
//     *SchemaTooNewError describing the overall-latest version (the one that
//     would be selected if the operator did pass --allow-newer-schema) —
//     "the only available version(s) exceed local schema support" per spec.
//   - pinned == "", allowNewer  -> the overall highest profile_version,
//     ignoring schema compatibility entirely.
func SelectVersion(m *Manifest, profileID, pinned string, allowNewer bool, supported int) (string, ManifestEntryVersion, error) {
	prof, ok := m.Profiles[profileID]
	if !ok {
		return "", ManifestEntryVersion{}, &ProfileNotFoundError{ProfileID: profileID}
	}

	if pinned != "" {
		entry, ok := prof.Versions[pinned]
		if !ok {
			return "", ManifestEntryVersion{}, &VersionNotFoundError{
				ProfileID: profileID,
				Requested: pinned,
				Available: sortedVersions(prof.Versions),
			}
		}
		if entry.SchemaVersion > supported && !allowNewer {
			return "", ManifestEntryVersion{}, &SchemaTooNewError{
				ProfileID:     profileID,
				SchemaVersion: entry.SchemaVersion,
				Supported:     supported,
			}
		}
		return pinned, entry, nil
	}

	if allowNewer {
		version, entry, found := maxVersion(prof.Versions, nil)
		if !found {
			return "", ManifestEntryVersion{}, &VersionNotFoundError{ProfileID: profileID, Requested: "(latest)"}
		}
		return version, entry, nil
	}

	compatible := func(e ManifestEntryVersion) bool { return e.SchemaVersion <= supported }
	if version, entry, found := maxVersion(prof.Versions, compatible); found {
		return version, entry, nil
	}

	// No compatible version exists — report the schema gap using the overall
	// latest version, per spec: "If the only available version(s) exceed
	// local schema support, fail with a clear error."
	_, latestEntry, found := maxVersion(prof.Versions, nil)
	if !found {
		return "", ManifestEntryVersion{}, &VersionNotFoundError{ProfileID: profileID, Requested: "(latest)"}
	}
	return "", ManifestEntryVersion{}, &SchemaTooNewError{
		ProfileID:     profileID,
		SchemaVersion: latestEntry.SchemaVersion,
		Supported:     supported,
	}
}

// LatestOverall returns the highest profile_version for a profile regardless
// of schema compatibility — used by `marlin profile list` to show what's
// actually newest (as opposed to what an unpinned pull would select).
func LatestOverall(m *Manifest, profileID string) (string, ManifestEntryVersion, error) {
	prof, ok := m.Profiles[profileID]
	if !ok {
		return "", ManifestEntryVersion{}, &ProfileNotFoundError{ProfileID: profileID}
	}
	version, entry, found := maxVersion(prof.Versions, nil)
	if !found {
		return "", ManifestEntryVersion{}, &VersionNotFoundError{ProfileID: profileID, Requested: "(latest)"}
	}
	return version, entry, nil
}

// maxVersion returns the highest profile_version (by semver.Compare) among
// versions for which filter returns true (filter == nil means "all"), and
// whether any qualifying version was found.
func maxVersion(versions map[string]ManifestEntryVersion, filter func(ManifestEntryVersion) bool) (string, ManifestEntryVersion, bool) {
	var bestVersion string
	var bestEntry ManifestEntryVersion
	found := false
	for v, e := range versions {
		if filter != nil && !filter(e) {
			continue
		}
		if !found || semver.Compare(v, bestVersion) > 0 {
			bestVersion, bestEntry, found = v, e, true
		}
	}
	return bestVersion, bestEntry, found
}

// sortedVersions returns the versions map's keys sorted ascending by semver,
// for deterministic "available versions" error messages.
func sortedVersions(versions map[string]ManifestEntryVersion) []string {
	out := make([]string, 0, len(versions))
	for v := range versions {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return semver.Compare(out[i], out[j]) < 0 })
	return out
}
