package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/state"
)

// nimDigester is implemented by NIM providers; vLLM does not implement it.
type nimDigester interface {
	PullImage(ctx context.Context, image string) error
	GetDigest(ctx context.Context, image string) (string, error)
}

var updateCmd = &cobra.Command{
	Use:   "update [model]",
	Short: "Pull the latest NIM container image and restart if changed",
	Long: `Pull the latest version of the active (or specified) NIM model image.
If the image digest changed, the container is restarted with the new image.
For vLLM models, update is not applicable.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	slug := cur.ActiveModel
	if len(args) > 0 {
		slug = args[0]
	}
	if slug == "" {
		return fmt.Errorf("no active model — specify a model slug or run 'marlin switch' first")
	}

	m, err := config.ResolveModel(slug, effectiveDirs(cfg)...)
	if err != nil {
		return fmt.Errorf("model %q: %w", slug, err)
	}
	if m.Model.Type != config.ProviderNIM {
		return fmt.Errorf("marlin update applies to nim providers only (model %q is %s)", slug, m.Model.Type)
	}
	if m.Model.Image == "" {
		return fmt.Errorf("model %q has no image set", slug)
	}

	p, err := buildProvider(m.Model.Type, cfg)
	if err != nil {
		return err
	}

	dg, ok := p.(nimDigester)
	if !ok {
		return fmt.Errorf("provider does not support image updates")
	}

	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "checking %s for updates…\n", m.Model.Image)

	if err := dg.PullImage(cmd.Context(), m.Model.Image); err != nil {
		return fmt.Errorf("pulling %s: %w", m.Model.Image, err)
	}

	newDigest, err := dg.GetDigest(cmd.Context(), m.Model.Image)
	if err != nil {
		return fmt.Errorf("reading image digest: %w", err)
	}

	if newDigest != "" && newDigest == cur.PinnedDigest {
		_, _ = fmt.Fprintf(w, "already up to date (%s)\n", shortDigest(newDigest))
		return nil
	}

	if cur.PinnedDigest != "" && newDigest != "" {
		_, _ = fmt.Fprintf(w, "update available: %s → %s\n",
			shortDigest(cur.PinnedDigest), shortDigest(newDigest))
	}

	_, _ = fmt.Fprintf(w, "restarting %s with new image\n", slug)
	if err := p.Switch(cmd.Context(), slug); err != nil {
		return err
	}

	cur.PinnedDigest = newDigest
	if saveErr := state.SavePrivileged(cmd.ErrOrStderr(), cfg.Paths.StateFile, cur); saveErr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save state: %v\n", saveErr)
	}

	_, _ = fmt.Fprintf(w, "updated to %s\n", shortDigest(newDigest))
	return nil
}

// shortDigest returns the last 12 hex chars of a sha256:... digest for display.
func shortDigest(d string) string {
	d = strings.TrimPrefix(d, "sha256:")
	if len(d) > 12 {
		return "sha256:" + d[:12] + "…"
	}
	return "sha256:" + d
}
