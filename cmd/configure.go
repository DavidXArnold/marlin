package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/provider"
	"github.com/DavidXArnold/marlin/internal/secrets"
)

// configureIn is injectable for tests.
var configureIn io.Reader = os.Stdin

// dockerLoginFunc is injectable for tests.
var dockerLoginFunc = func(apiKey string) error {
	bin, err := provider.ContainerBinary()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "login", "nvcr.io",
		"--username", "$oauthtoken", "--password-stdin")
	cmd.Stdin = strings.NewReader(apiKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure API keys (HuggingFace, NGC)",
	Long:  `Interactively set or update API keys stored in the secrets file.`,
	Args:  cobra.NoArgs,
	RunE:  runConfigure,
}

func init() {
	rootCmd.AddCommand(configureCmd)
}

type secretSpec struct {
	key     string
	label   string
	infoURL string
}

var secretSpecs = []secretSpec{
	{
		key:     "HF_TOKEN",
		label:   "HuggingFace token (for gated models)",
		infoURL: "https://huggingface.co/settings/tokens",
	},
	{
		key:     "NGC_API_KEY",
		label:   "NVIDIA NGC API key (for NIM containers and registry search)",
		infoURL: "https://org.ngc.nvidia.com/setup/personal-keys",
	},
}

func runConfigure(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	path := cfg.Paths.SecretsEnv
	existing, err := secrets.Load(path)
	if err != nil {
		return fmt.Errorf("loading secrets: %w", err)
	}

	w := cmd.OutOrStdout()
	writef := func(format string, args ...any) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	writeln := func(args ...any) error {
		_, err := fmt.Fprintln(w, args...)
		return err
	}

	if err := writef("Configuring API keys for marlin.\n"); err != nil {
		return err
	}
	if err := writef("Keys will be saved to %s\n\n", path); err != nil {
		return err
	}

	reader := bufio.NewReader(configureIn)
	updates := make(map[string]string)

	for _, spec := range secretSpecs {
		isSet := existing[spec.key] != ""
		keepOrSkip := "skip"
		if isSet {
			keepOrSkip = "keep"
		}

		if err := writef("%s\n", spec.label); err != nil {
			return err
		}
		if err := writef("  Generate: %s\n", spec.infoURL); err != nil {
			return err
		}
		if isSet {
			if err := writef("  Status:   [set]\n"); err != nil {
				return err
			}
		} else {
			if err := writef("  Status:   [not set]\n"); err != nil {
				return err
			}
		}
		if err := writef("  New value (Enter to %s): ", keepOrSkip); err != nil {
			return err
		}

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			updates[spec.key] = line
		}
		if err := writeln(); err != nil {
			return err
		}
	}

	if len(updates) == 0 {
		return writeln("No changes made.")
	}

	if err := secrets.Save(path, updates); err != nil {
		return fmt.Errorf("saving secrets: %w", err)
	}
	if err := writef("Saved to %s\n\n", path); err != nil {
		return err
	}

	// If an NGC key was just set, offer to authenticate Docker to nvcr.io.
	// NIM image pulls require: docker login nvcr.io -u $oauthtoken -p <key>
	if ngcKey, ok := updates["NGC_API_KEY"]; ok && ngcKey != "" {
		if err := writef("NIM images are hosted on nvcr.io and require Docker registry auth.\n"); err != nil {
			return err
		}
		if err := writef("  docker login nvcr.io --username '$oauthtoken' --password-stdin\n\n"); err != nil {
			return err
		}
		if err := writef("Run docker login nvcr.io now? [y/N]: "); err != nil {
			return err
		}

		line, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) == "y" {
			if err := dockerLoginFunc(ngcKey); err != nil {
				if err := writef("docker login failed: %v\n", err); err != nil {
					return err
				}
				if err := writef("Run it manually with your NGC API key as the password.\n"); err != nil {
					return err
				}
			} else {
				if err := writeln("Docker authenticated to nvcr.io."); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
