package doctor

import (
	"context"
	"encoding/json"
	"strings"
)

func runtimeChecks() []Check {
	return []Check{
		&funcCheck{id: "runtime.docker", run: checkRuntime("docker", []string{"version", "--format", "json"})},
		&funcCheck{id: "runtime.podman", run: checkRuntime("podman", []string{"version", "--format", "json"})},
		&funcCheck{id: "runtime.nerdctl", run: checkRuntime("nerdctl", []string{"version"})},
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
