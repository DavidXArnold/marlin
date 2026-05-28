package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm <model>",
	Short: "Remove a model profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runRm,
}

func init() {
	rootCmd.AddCommand(rmCmd)
}

func runRm(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	slug := args[0]
	path := filepath.Join(cfg.Paths.ModelsDir, slug+".toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("model %q not found", slug)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
	return err
}
