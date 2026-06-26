package doctor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	marlinConfig "github.com/DavidXArnold/marlin/internal/config"
)

func runtimeChecks(cfg *marlinConfig.Config) []Check {
	checks := []Check{
		&funcCheck{id: "runtime.docker", run: checkRuntime("docker", []string{"version", "--format", "json"})},
		&funcCheck{id: "runtime.podman", run: checkRuntime("podman", []string{"version", "--format", "json"})},
		&funcCheck{id: "runtime.nerdctl", run: checkRuntime("nerdctl", []string{"version"})},
	}
	if cfg.Service.VLLMMode == "binary" {
		checks = append(checks, &funcCheck{id: "runtime.vllm", run: checkVLLMBin(cfg)})
	} else {
		checks = append(checks, &funcCheck{id: "runtime.vllm_container", run: checkVLLMContainer(cfg)})
	}
	return checks
}

// checkVLLMContainer verifies that at least one container runtime is reachable
// when vllm_mode = "container" (the default). Missing runtime is a FAIL since the
// service cannot start without one.
func checkVLLMContainer(cfg *marlinConfig.Config) func(ctx context.Context) Result {
	return func(_ context.Context) Result {
		id := "runtime.vllm_container"
		candidates := []string{"docker", "podman", "nerdctl"}
		if rt := cfg.Service.ContainerRuntime; rt != "" {
			candidates = []string{rt}
		}
		for _, name := range candidates {
			if found, err := exec.LookPath(name); err == nil {
				return Result{ID: id, Level: LevelPass, Detail: "container mode → " + found}
			}
		}
		return Result{
			ID:     id,
			Level:  LevelFail,
			Detail: "no container runtime found (tried: " + strings.Join(candidates, ", ") + ")",
			Hint:   "install Docker (https://docs.docker.com/engine/install/) or Podman (https://podman.io/docs/installation), or set service.vllm_mode = \"binary\" in config.toml",
		}
	}
}

// checkVLLMBin verifies the vllm binary is reachable when vllm_mode = "binary".
func checkVLLMBin(cfg *marlinConfig.Config) func(ctx context.Context) Result {
	return func(_ context.Context) Result {
		id := "runtime.vllm"
		if bin := cfg.Service.VLLMBin; bin != "" {
			if _, err := os.Stat(bin); err != nil {
				return Result{
					ID:     id,
					Level:  LevelWarn,
					Detail: "configured binary not found: " + bin,
					Hint:   "verify service.vllm_bin in config.toml",
				}
			}
			return Result{ID: id, Level: LevelPass, Detail: bin}
		}
		if found, err := exec.LookPath("vllm"); err == nil {
			return Result{ID: id, Level: LevelPass, Detail: found}
		}
		return Result{
			ID:     id,
			Level:  LevelWarn,
			Detail: "vllm not found in PATH",
			Hint:   "set service.vllm_bin in config.toml, or switch to container mode: service.vllm_mode = \"container\"",
		}
	}
}

func checkRuntime(name string, args []string) func(ctx context.Context) Result {
	return func(ctx context.Context) Result {
		out, err := doctorRunCmd(ctx, name, args...)
		if err != nil {
			return Result{
				ID:     "runtime." + name,
				Level:  LevelWarn,
				Detail: "not reachable",
				Hint:   "install " + name + " to use this container runtime",
			}
		}
		version := extractVersion(name, out)
		return Result{
			ID:     "runtime." + name,
			Level:  LevelPass,
			Detail: version,
		}
	}
}

func extractVersion(runtime string, out []byte) string {
	switch runtime {
	case "docker":
		var v struct {
			Client struct {
				Version string `json:"Version"`
			} `json:"Client"`
		}
		if err := json.Unmarshal(out, &v); err == nil && v.Client.Version != "" {
			return runtime + " " + v.Client.Version
		}
	case "podman":
		var v struct {
			Version string `json:"Version"`
		}
		if err := json.Unmarshal(out, &v); err == nil && v.Version != "" {
			return runtime + " " + v.Version
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			return line
		}
	}
	return runtime + " available"
}
