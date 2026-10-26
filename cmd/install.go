package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/internal/service"
	"github.com/DavidXArnold/marlin/pkg/render"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the marlin systemd service unit (vLLM or llama.cpp)",
	Long: `Write /etc/systemd/system/<unit>.service and reload systemd.

The generated unit sources the active model env file and the secrets env file,
then runs 'vllm serve' with the configured host and port.

With --provider llamacpp, installs the llama.cpp unit (service.llamacpp_unit)
instead. It runs llama-server in a container (service.llamacpp_image) with the
Hugging Face cache mounted, so model.id GGUF references download once and are
reused; set service.llamacpp_mode = "binary" to run a host llama-server.

After installing, run 'marlin start <model>' to start the service.
Use --enable to also configure the service to start automatically at boot.`,
	Args: cobra.NoArgs,
	RunE: runInstall,
}

// installSystemdManagerFunc is injectable for tests.
var installSystemdManagerFunc = service.NewSystemdManager

// installUnitPathFunc is injectable for tests.
var installUnitPathFunc = render.SystemdUnitPath

// installLlamaCppUnitPathFunc is injectable for tests.
var installLlamaCppUnitPathFunc = render.LlamaCppUnitPath

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().Bool("enable", false, "Also enable the service to start at boot")
	installCmd.Flags().Bool("force", false, "Overwrite an existing unit file without prompting")
	installCmd.Flags().String("provider", "vllm", "Which service unit to install: vllm or llamacpp")
}

func runInstall(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	force, _ := cmd.Flags().GetBool("force")
	enable, _ := cmd.Flags().GetBool("enable")

	providerName, _ := cmd.Flags().GetString("provider")

	var (
		unitPath    string
		unitName    string
		unitContent []byte
	)
	switch providerName {
	case "vllm", "":
		unitPath = installUnitPathFunc(cfg)
		unitName = cfg.Service.SystemdUnit
		unitContent = renderVLLMUnit(cfg, w)
	case "llamacpp":
		unitPath = installLlamaCppUnitPathFunc(cfg)
		unitName = cfg.Service.LlamaCppUnit
		unitContent = renderLlamaCppUnit(cfg, w)
	default:
		return fmt.Errorf("unknown --provider %q (want vllm or llamacpp)", providerName)
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
	svc := installSystemdManagerFunc(unitName)
	if err := svc.DaemonReload(cmd.Context()); err != nil {
		_, _ = fmt.Fprintf(w, "warning: daemon-reload failed: %v\n", err)
	} else {
		_, _ = fmt.Fprintln(w, "systemd daemon reloaded")
	}

	if enable {
		if err := svc.Enable(cmd.Context()); err != nil {
			_, _ = fmt.Fprintf(w, "warning: enable failed: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(w, "enabled %s at boot\n", unitName)
		}
	}

	_, _ = fmt.Fprintf(w, "\nservice installed — run 'marlin start <model>' to start\n")
	return nil
}

func renderVLLMUnit(cfg *config.Config, w io.Writer) []byte {
	if cfg.Service.VLLMMode == "binary" {
		vllmBin, found := render.ResolveVLLMBin(cfg.Service.VLLMBin)
		if !found {
			_, _ = fmt.Fprintf(w, "warning: vllm not found in PATH — unit will use bare name %q\n", vllmBin)
			_, _ = fmt.Fprintln(w, "  set service.vllm_bin in config.toml to the full path if vllm is in a venv")
		} else {
			_, _ = fmt.Fprintf(w, "using vllm binary: %s\n", vllmBin)
		}
		return []byte(render.SystemdUnit(cfg, vllmBin))
	}
	containerBin, found := resolveContainerBinOrWarn(cfg, w)
	if !found {
		_, _ = fmt.Fprintln(w, "  to use a bare vllm install instead: set service.vllm_mode = \"binary\"")
	}
	hfCache := render.ResolveHFCache(cfg)
	_, _ = fmt.Fprintf(w, "mounting Hugging Face cache: %s\n", hfCache)
	return []byte(render.SystemdUnitContainerized(cfg, containerBin, hfCache))
}

func renderLlamaCppUnit(cfg *config.Config, w io.Writer) []byte {
	if cfg.Service.LlamaCppMode == "binary" {
		bin, found := render.ResolveLlamaCppBin(cfg.Service.LlamaCppBin)
		if !found {
			_, _ = fmt.Fprintf(w, "warning: llama-server not found in PATH — unit will use bare name %q\n", bin)
			_, _ = fmt.Fprintln(w, "  set service.llamacpp_bin in config.toml to the full path")
		} else {
			_, _ = fmt.Fprintf(w, "using llama-server binary: %s\n", bin)
		}
		return []byte(render.SystemdUnitLlamaCpp(cfg, bin))
	}
	containerBin, found := resolveContainerBinOrWarn(cfg, w)
	if !found {
		_, _ = fmt.Fprintln(w, "  to use a host llama-server instead: set service.llamacpp_mode = \"binary\"")
	}
	hfCache := render.ResolveHFCache(cfg)
	_, _ = fmt.Fprintf(w, "mounting Hugging Face cache: %s\n", hfCache)
	return []byte(render.SystemdUnitLlamaCppContainerized(cfg, containerBin, hfCache))
}

func resolveContainerBinOrWarn(cfg *config.Config, w io.Writer) (string, bool) {
	containerBin, found := render.ResolveContainerBin(cfg)
	if !found {
		_, _ = fmt.Fprintln(w, "warning: no container runtime (docker/podman/nerdctl) found in PATH")
		_, _ = fmt.Fprintln(w, "  install Docker:  https://docs.docker.com/engine/install/")
		_, _ = fmt.Fprintln(w, "  install Podman:  https://podman.io/docs/installation")
		_, _ = fmt.Fprintln(w, "  or set service.container_runtime in config.toml")
	} else {
		_, _ = fmt.Fprintf(w, "using container runtime: %s\n", containerBin)
	}
	return containerBin, found
}

// installPromptReader is injectable for tests.
var installPromptReader io.Reader = os.Stdin
