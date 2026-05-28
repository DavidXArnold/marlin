package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/secrets"
)

// configureIn is injectable for tests.
var configureIn io.Reader = os.Stdin

// dockerLoginFunc is injectable for tests.
var dockerLoginFunc = func(apiKey string) error {
	cmd := exec.Command("docker", "login", "nvcr.io",
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
	fmt.Fprintf(w, "Configuring API keys for marlin.\n")
	fmt.Fprintf(w, "Keys will be saved to %s\n\n", path)

	reader := bufio.NewReader(configureIn)
	updates := make(map[string]string)

	for _, spec := range secretSpecs {
		isSet := existing[spec.key] != ""
		keepOrSkip := "skip"
		if isSet {
			keepOrSkip = "keep"
		}

		fmt.Fprintf(w, "%s\n", spec.label)
		fmt.Fprintf(w, "  Generate: %s\n", spec.infoURL)
		if isSet {
			fmt.Fprintf(w, "  Status:   [set]\n")
		} else {
			fmt.Fprintf(w, "  Status:   [not set]\n")
		}
		fmt.Fprintf(w, "  New value (Enter to %s): ", keepOrSkip)

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			updates[spec.key] = line
		}
		fmt.Fprintln(w)
	}

	if len(updates) == 0 {
		fmt.Fprintln(w, "No changes made.")
		return nil
	}

	if err := secrets.Save(path, updates); err != nil {
		return fmt.Errorf("saving secrets: %w", err)
	}
	fmt.Fprintf(w, "Saved to %s\n\n", path)

	// If an NGC key was just set, offer to authenticate Docker to nvcr.io.
	// NIM image pulls require: docker login nvcr.io -u $oauthtoken -p <key>
	if ngcKey, ok := updates["NGC_API_KEY"]; ok && ngcKey != "" {
		fmt.Fprintf(w, "NIM images are hosted on nvcr.io and require Docker registry auth.\n")
		fmt.Fprintf(w, "  docker login nvcr.io --username '$oauthtoken' --password-stdin\n\n")
		fmt.Fprintf(w, "Run docker login nvcr.io now? [y/N]: ")

		line, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) == "y" {
			if err := dockerLoginFunc(ngcKey); err != nil {
				fmt.Fprintf(w, "docker login failed: %v\n", err)
				fmt.Fprintf(w, "Run it manually with your NGC API key as the password.\n")
			} else {
				fmt.Fprintln(w, "Docker authenticated to nvcr.io.")
			}
		}
	}

	return nil
}
