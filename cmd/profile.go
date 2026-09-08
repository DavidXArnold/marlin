package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/profile"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage versioned model profiles from the marlin profile repository",
	Long: `Fetch model profiles (TOML configs) from a GitHub-hosted profile
repository, independent of marlin's own release cycle.

Profiles declare a schema_version; marlin refuses to pull or apply a profile
whose schema exceeds what this build understands, unless overridden with
--allow-newer-schema. See 'marlin profile list' and 'marlin profile pull'.`,
}

func init() {
	rootCmd.AddCommand(profileCmd)
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profilePullCmd)

	profileListCmd.Flags().Bool("refresh", false, "Bypass the manifest cache and re-fetch")

	profilePullCmd.Flags().Bool("allow-newer-schema", false,
		"Download and apply even if schema_version exceeds this build's support; unrecognized fields are ignored (warns)")
	profilePullCmd.Flags().Bool("global", false, "Install to system models dir")
	profilePullCmd.Flags().Bool("refresh", false, "Bypass the manifest cache and re-fetch")
}

// newProfileSource is injectable for tests, mirroring buildProvider/newMeshSvcManager.
var newProfileSource = func(cfg *config.Config) *profile.Source {
	return profile.NewSource(cfg.Profiles.RepoOwner, cfg.Profiles.RepoName, cfg.Profiles.RepoRef, cfg.Profiles.Subdir)
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List profiles available in the profile repository",
	RunE:  runProfileList,
}

var profilePullCmd = &cobra.Command{
	Use:   "pull <profile-id>[@<profile_version>]",
	Short: "Fetch a profile from the profile repository",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfilePull,
}

// splitProfileRef splits "id" or "id@version" on the last "@".
func splitProfileRef(raw string) (id, version string) {
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		return raw[:i], raw[i+1:]
	}
	return raw, ""
}

type profileListItem struct {
	ID               string `json:"id"`
	LatestVersion    string `json:"latest_version"`
	SchemaVersion    int    `json:"schema_version"`
	SchemaCompatible bool   `json:"schema_compatible"`
}

// setProfileVerbosity wires the global -v/-vv/-vvv flag into src's request/
// header/body logging, matching how registry clients are wired in search.go.
func setProfileVerbosity(cmd *cobra.Command, src *profile.Source) {
	if Verbosity > 0 {
		src.SetVerbose(cmd.ErrOrStderr(), Verbosity)
	}
}

func runProfileList(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}
	refresh, _ := cmd.Flags().GetBool("refresh")

	src := newProfileSource(cfg)
	setProfileVerbosity(cmd, src)

	m, err := profile.LoadManifest(cmdCtx(cmd), src, cfg.Paths.ProfileCacheFile, cfg.Profiles.CacheTTLDuration(), refresh)
	if err != nil {
		return fmt.Errorf("loading profile manifest: %w", err)
	}

	ids := make([]string, 0, len(m.Profiles))
	for id := range m.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	w := cmd.OutOrStdout()
	if len(ids) == 0 {
		_, err := fmt.Fprintln(w, "no profiles found in the profile repository")
		return err
	}

	items := make([]profileListItem, 0, len(ids))
	for _, id := range ids {
		version, entry, err := profile.LatestOverall(m, id)
		if err != nil {
			// The id came from m.Profiles itself, so this shouldn't happen —
			// skip defensively rather than fail the whole listing.
			continue
		}
		items = append(items, profileListItem{
			ID:               id,
			LatestVersion:    version,
			SchemaVersion:    entry.SchemaVersion,
			SchemaCompatible: entry.SchemaVersion <= config.SupportedSchemaVersion,
		})
	}

	return renderProfileList(w, items)
}

func renderProfileList(w io.Writer, items []profileListItem) error {
	switch outputFormat {
	case "json":
		return writeJSON(w, items)
	case "jsonl":
		for _, it := range items {
			if err := writeJSONLine(w, it); err != nil {
				return err
			}
		}
		return nil
	case "plain":
		for _, it := range items {
			compat := "compatible"
			if !it.SchemaCompatible {
				compat = "incompatible"
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", it.ID, it.LatestVersion, it.SchemaVersion, compat); err != nil {
				return err
			}
		}
		return nil
	default: // table
		if _, err := fmt.Fprintf(w, "%-30s %-10s %-6s %s\n", "PROFILE", "LATEST", "SCHEMA", ""); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%-30s %-10s %-6s %s\n", "-------", "------", "------", ""); err != nil {
			return err
		}
		for _, it := range items {
			flag := ""
			if !it.SchemaCompatible {
				flag = fmt.Sprintf("⚠ requires marlin upgrade (needs schema %d, this build supports %d)",
					it.SchemaVersion, config.SupportedSchemaVersion)
			}
			if _, err := fmt.Fprintf(w, "%-30s %-10s %-6d %s\n", it.ID, it.LatestVersion, it.SchemaVersion, flag); err != nil {
				return err
			}
		}
		return nil
	}
}

func runProfilePull(cmd *cobra.Command, args []string) error {
	id, pinned := splitProfileRef(args[0])

	cfg, err := globalConfig()
	if err != nil {
		return err
	}
	allowNewer, _ := cmd.Flags().GetBool("allow-newer-schema")
	global, _ := cmd.Flags().GetBool("global")
	refresh, _ := cmd.Flags().GetBool("refresh")

	src := newProfileSource(cfg)
	setProfileVerbosity(cmd, src)

	w := cmd.OutOrStdout()
	ctx := cmdCtx(cmd)

	m, err := profile.LoadManifest(ctx, src, cfg.Paths.ProfileCacheFile, cfg.Profiles.CacheTTLDuration(), refresh)
	if err != nil {
		return fmt.Errorf("loading profile manifest: %w", err)
	}

	// SelectVersion enforces the schema gate before any profile file is
	// downloaded — on the default (no-override) path, an incompatible
	// profile fails here without touching the network again or writing
	// anything to disk.
	version, entry, err := profile.SelectVersion(m, id, pinned, allowNewer, config.SupportedSchemaVersion)
	if err != nil {
		return err
	}

	raw, err := src.FetchProfileBytes(ctx, entry.Path)
	if err != nil {
		return err
	}

	if err := profile.VerifyChecksum(id, version, raw, entry.SHA256); err != nil {
		return err
	}

	result, err := profile.Decode(raw)
	if err != nil {
		return err
	}

	// Only warn when the override was actually load-bearing (the profile's
	// schema truly exceeds local support) — passing --allow-newer-schema on
	// an already-compatible profile must stay silent.
	usedOverride := entry.SchemaVersion > config.SupportedSchemaVersion
	if usedOverride && len(result.UnrecognizedKeys) > 0 {
		if _, err := fmt.Fprintf(w, "Warning: profile uses fields unsupported by this Marlin build and they will be ignored: %s\n",
			strings.Join(result.UnrecognizedKeys, ", ")); err != nil {
			return err
		}
	}

	destDir := installDir(cfg, global)
	destPath := filepath.Join(destDir, id+".toml")

	data, err := config.ModelConfigToBytes(result.Config)
	if err != nil {
		return fmt.Errorf("encoding profile: %w", err)
	}
	written, err := privilege.PromptAndWriteFile(w, destDir, destPath, data)
	if err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}
	if !written {
		return nil // cancelled
	}

	_, err = fmt.Fprintf(w, "pulled %s@%s -> %s\n", id, version, destPath)
	return err
}
