package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/mesh"
	"github.com/DavidXArnold/marlin/internal/service"
)

// cmdCtx returns the cobra command's context, falling back to Background when
// the command is invoked directly (e.g. in tests) without a set context.
func cmdCtx(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

var meshCmd = &cobra.Command{
	Use:   "mesh",
	Short: "Manage the local mesh-llm peer",
	Long: `Manage the local mesh-llm peer.

mesh-llm pools GPUs across machines and presents a single OpenAI-compatible
API at localhost:9337/v1. Marlin integrates with it in two ways:

  1. Auto-registration: when mesh.auto_register = true in the marlin config,
     'marlin switch' automatically registers the active vLLM/NIM endpoint with
     the mesh peer so it's reachable by all connected nodes.

  2. GGUF models: model profiles with type = "mesh" are loaded and unloaded
     via mesh-llm's management API without touching the vLLM service.

Run 'mesh-llm setup' to install mesh-llm as a systemd service before using
'marlin mesh start' and 'marlin mesh stop'.`,
}

func init() {
	rootCmd.AddCommand(meshCmd)
	meshCmd.AddCommand(meshStartCmd)
	meshCmd.AddCommand(meshStopCmd)
	meshCmd.AddCommand(meshStatusCmd)
	meshCmd.AddCommand(meshPeersCmd)
	meshCmd.AddCommand(meshPushConfigCmd)

	meshStartCmd.Flags().String("join", "", "Join a private mesh using this invite token (sets join_token in mesh-llm config)")
	meshStartCmd.Flags().Bool("headless", false, "Start without the web UI (sets headless = true in mesh-llm config)")
}

// newMeshSvcManager is injectable for tests.
var newMeshSvcManager = func(unit string) meshSvcManager {
	return service.NewSystemdManager(unit)
}

type meshSvcManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsActive(ctx context.Context) (bool, error)
}

// --- mesh start ---

var meshStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local mesh-llm peer (via systemd)",
	RunE:  runMeshStart,
}

func runMeshStart(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	join, _ := cmd.Flags().GetString("join")
	if join != "" {
		if cfg.Mesh.ConfigPath == "" {
			return fmt.Errorf("mesh.config_path is not set — cannot write join token")
		}
		if err := mesh.PatchJoinToken(cfg.Mesh.ConfigPath, join); err != nil {
			return fmt.Errorf("writing join token to mesh config: %w", err)
		}
	}

	svc := newMeshSvcManager(cfg.Mesh.SystemdUnit)
	if active, err := svc.IsActive(cmdCtx(cmd)); err == nil && active {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "mesh-llm is already running (unit: %s)\n", cfg.Mesh.SystemdUnit)
		return nil
	}
	if err := svc.Start(cmdCtx(cmd)); err != nil {
		return fmt.Errorf("%w\nhint: install mesh-llm as a service with 'mesh-llm setup'", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "mesh-llm peer started (unit: %s)\n", cfg.Mesh.SystemdUnit)
	return nil
}

// --- mesh stop ---

var meshStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local mesh-llm peer",
	RunE:  runMeshStop,
}

func runMeshStop(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}
	svc := newMeshSvcManager(cfg.Mesh.SystemdUnit)
	if err := svc.Stop(cmdCtx(cmd)); err != nil {
		return fmt.Errorf("stopping mesh-llm: %w", err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "mesh-llm peer stopped\n")
	return nil
}

// --- mesh status ---

var meshStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show mesh-llm peer status and connected nodes",
	RunE:  runMeshStatus,
}

func runMeshStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	out := lineWriter{cmd.OutOrStdout()}

	svc := newMeshSvcManager(cfg.Mesh.SystemdUnit)
	active, _ := svc.IsActive(cmdCtx(cmd))
	svcState := "stopped"
	if active {
		svcState = "running"
	}
	if err := out.printf("mesh-llm     : %s (unit: %s)\n", svcState, cfg.Mesh.SystemdUnit); err != nil {
		return err
	}
	if err := out.printf("inference    : %s/v1\n", cfg.Mesh.InferenceURL); err != nil {
		return err
	}

	client := mesh.NewClient(cfg.Mesh.ManagementURL)
	info, apiErr := client.Runtime(cmdCtx(cmd))
	if apiErr != nil {
		return out.printf("api          : error (%v)\n", apiErr)
	}
	if info == nil {
		return out.printf("api          : not reachable (%s)\n", cfg.Mesh.ManagementURL)
	}

	if err := out.printf("peers        : %d connected\n", len(info.Peers)); err != nil {
		return err
	}
	for _, p := range info.Peers {
		models := ""
		if len(p.Models) > 0 {
			models = "  [" + joinStrings(p.Models, ", ") + "]"
		}
		if err := out.printf("  %-14s %s%s\n", shortID(p.ID), p.Addr, models); err != nil {
			return err
		}
	}

	if err := out.printf("models       : %d loaded\n", len(info.Models)); err != nil {
		return err
	}
	for _, m := range info.Models {
		state := m.State
		if state == "" {
			state = "unknown"
		}
		if err := out.printf("  %s  [%s]\n", m.Ref, state); err != nil {
			return err
		}
	}
	return nil
}

// --- mesh peers ---

var meshPeersCmd = &cobra.Command{
	Use:   "peers",
	Short: "List connected mesh-llm peers",
	RunE:  runMeshPeers,
}

func runMeshPeers(cmd *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	client := mesh.NewClient(cfg.Mesh.ManagementURL)
	info, err := client.Runtime(cmdCtx(cmd))
	if err != nil {
		return fmt.Errorf("querying mesh-llm: %w", err)
	}
	if info == nil {
		return fmt.Errorf("mesh-llm is not running (%s unreachable)", cfg.Mesh.ManagementURL)
	}

	out := lineWriter{cmd.OutOrStdout()}
	if len(info.Peers) == 0 {
		return out.println("no peers connected")
	}
	for _, p := range info.Peers {
		models := "(no models)"
		if len(p.Models) > 0 {
			models = joinStrings(p.Models, ", ")
		}
		if err := out.printf("%-20s  %-20s  %s\n", shortID(p.ID), p.Addr, models); err != nil {
			return err
		}
	}
	return nil
}

// --- mesh push-config ---

var meshPushConfigCmd = &cobra.Command{
	Use:   "push-config <endpoint-token>",
	Short: "Push the local mesh-llm config to a remote node",
	Long: `Push the local mesh-llm config file to a remote mesh-llm node.

The endpoint token is printed by 'mesh-llm runtime bootstrap' on the remote
node. This command fetches the remote node's current config revision, then
applies the local config file with the expected revision for safe concurrency.

The local config file (mesh.config_path in marlin config) is managed
automatically by 'marlin switch' when mesh.auto_register = true.`,
	Args: cobra.ExactArgs(1),
	RunE: runMeshPushConfig,
}

func runMeshPushConfig(cmd *cobra.Command, args []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}
	token := args[0]

	bin := cfg.Mesh.MeshBin
	if bin == "" {
		bin = "mesh-llm"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("mesh-llm binary not found (looked for %q) — install from https://github.com/Mesh-LLM/mesh-llm", bin)
	}

	// Fetch the remote config to get the current revision.
	revision := 0
	rawCfg, err := meshExecFunc(cmdCtx(cmd), bin, "runtime", "get-config",
		"--endpoint", token, "--json")
	if err == nil {
		var state struct {
			Revision int `json:"revision"`
		}
		if jsonErr := json.Unmarshal(rawCfg, &state); jsonErr == nil && state.Revision > 0 {
			revision = state.Revision
		}
	}

	// Push the local mesh-llm config to the remote node.
	if _, err := meshExecFunc(cmdCtx(cmd), bin, "runtime", "apply-config",
		"--endpoint", token,
		"--expected-revision", strconv.Itoa(revision),
		"--config", cfg.Mesh.ConfigPath); err != nil {
		return fmt.Errorf("pushing config to remote node: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config pushed to remote node (revision %d → %d)\n",
		revision, revision+1)
	return nil
}

// meshExecFunc is injectable for tests.
var meshExecFunc = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, args...).Output()
}

// joinStrings joins ss with sep (avoids importing strings just for this).
func joinStrings(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
