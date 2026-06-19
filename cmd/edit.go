package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/state"
)

var editCmd = &cobra.Command{
	Use:   "edit [model]",
	Short: "Open a model profile in $EDITOR",
	Args:  cobra.MaximumNArgs(1),
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

// execSudoEditorFunc is injectable for tests.
var execSudoEditorFunc = func(editor, path string) error {
	c := exec.Command("sudo", editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// editPromptReader is injectable for tests.
var editPromptReader io.Reader = os.Stdin

// editNeedsRootFunc is injectable for tests.
var editNeedsRootFunc = privilege.NeedsRoot

func init() {
	rootCmd.AddCommand(editCmd)
}

func runEdit(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	dirs := effectiveDirs(cfg)
	models, names, err := config.ListModelsFromDirs(dirs...)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	query := ""
	if len(args) > 0 {
		query = args[0]
	}

	slug, err := resolveModel(query, names, models, cur.ActiveModel, cur.ModelHistory)
	if err != nil {
		return err
	}

	path, err := config.FindModelPath(slug, dirs...)
	if err != nil {
		return fmt.Errorf("model %q not found", slug)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	if editNeedsRootFunc(filepath.Dir(path)) {
		w := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(w, "warning: %s requires administrator privileges to edit\n", path)
		if !confirmPrompt(w, editPromptReader, "continue with sudo? [y/N] ") {
			_, _ = fmt.Fprintln(w, "cancelled")
			return nil
		}
		return execSudoEditorFunc(editor, path)
	}

	return execEditorFunc(editor, path)
}
