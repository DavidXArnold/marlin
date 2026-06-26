package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/pkg/render"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the marlin systemd service unit (vLLM provider)",
	Long: `Write /etc/systemd/system/<unit>.service and reload systemd.

The generated unit sources the active model env file and the secrets env file,
then runs 'vllm serve' with the configured host and port.

After installing, run 'marlin start <model>' to start the service.
Use --enable to also configure the service to start automatically at boot.`,
	Args: cobra.NoArgs,
	RunE: runInstall,
}

// installSystemdManagerFunc is injectable for tests.
var installSystemdManagerFunc = func(unit string) *service.SystemdManager {
	return service.NewSystemdManager(unit)
}

// installUnitPathFunc is injectable for tests.
var installUnitPathFunc = render.SystemdUnitPath

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().Bool("enable", false, "Also enable the service to start at boot")
	installCmd.Flags().Bool("force", false, "Overwrite an existing unit file without prompting")
}

func runInstall(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	force, _ := cmd.Flags().GetBool("force")
	enable, _ := cmd.Flags().GetBool("enable")

	unitPath := installUnitPathFunc(cfg)

	var unitContent []byte
	if cfg.Service.VLLMMode == "binary" {
		vllmBin, found := render.ResolveVLLMBin(cfg.Service.VLLMBin)
		if !found {
			_, _ = fmt.Fprintf(w, "warning: vllm not found in PATH — unit will use bare name %q\n", vllmBin)
			_, _ = fmt.Fprintln(w, "  set service.vllm_bin in config.toml to the full path if vllm is in a venv")
		} else {
			_, _ = fmt.Fprintf(w, "using vllm binary: %s\n", vllmBin)
		}
		unitContent = []byte(render.SystemdUnit(cfg, vllmBin))
	} else {
		containerBin, found := render.ResolveContainerBin(cfg)
		if !found {
			_, _ = fmt.Fprintln(w, "warning: no container runtime (docker/podman/nerdctl) found in PATH")
			_, _ = fmt.Fprintln(w, "  install Docker:  https://docs.docker.com/engine/install/")
			_, _ = fmt.Fprintln(w, "  install Podman:  https://podman.io/docs/installation")
			_, _ = fmt.Fprintln(w, "  or set service.container_runtime in config.toml")
			_, _ = fmt.Fprintln(w, "  to use a bare vllm install instead: set service.vllm_mode = \"binary\"")
		} else {
			_, _ = fmt.Fprintf(w, "using container runtime: %s\n", containerBin)
		}
		unitContent = []byte(render.SystemdUnitContainerized(cfg, containerBin))
	}

	// Warn if the unit file already exists.
	if !force {
		if _, statErr := os.Stat(unitPath); statErr == nil {
			_, _ = fmt.Fprintf(w, "warning: %s already exists\n", unitPath)
			if !confirmPrompt(w, installPromptReader, "overwrite? [y/N] ") {
				_, _ = fmt.Fprintln(w, "cancelled")
				return nil
			}
		}
	}

	// Write unit file (requires root on most systems).
	ok, err := privilege.PromptAndWriteFile(w, filepath.Dir(unitPath), unitPath, unitContent)
	if err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}
	if !ok {
		return nil // user declined sudo prompt
	}
	_, _ = fmt.Fprintf(w, "wrote %s\n", unitPath)

	// Reload systemd so it picks up the new unit.
	svc := installSystemdManagerFunc(cfg.Service.SystemdUnit)
	if err := svc.DaemonReload(cmd.Context()); err != nil {
		_, _ = fmt.Fprintf(w, "warning: daemon-reload failed: %v\n", err)
	} else {
		_, _ = fmt.Fprintln(w, "systemd daemon reloaded")
	}

	if enable {
		if err := svc.Enable(cmd.Context()); err != nil {
			_, _ = fmt.Fprintf(w, "warning: enable failed: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(w, "enabled %s at boot\n", cfg.Service.SystemdUnit)
		}
	}

	_, _ = fmt.Fprintf(w, "\nservice installed — run 'marlin start <model>' to start\n")
	return nil
}

// installPromptReader is injectable for tests.
var installPromptReader io.Reader = os.Stdin
