package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
)

var editCmd = &cobra.Command{
	Use:   "edit <model>",
	Short: "Open a model profile in $EDITOR",
	Args:  cobra.ExactArgs(1),
	RunE:  runEdit,
}

// execEditorFunc is injectable for tests.
var execEditorFunc = func(editor, path string) error {
	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func init() {
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	slug := args[0]
	path, err := config.FindModelPath(slug, effectiveDirs(cfg)...)
	if err != nil {
		return fmt.Errorf("model %q not found", slug)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	return execEditorFunc(editor, path)
}
