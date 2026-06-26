package doctor

import (
	"context"
	"os/exec"

	marlinConfig "github.com/DavidXArnold/marlin/internal/config"
)

// DoctorRunCmd is injectable for tests so checks don't need real docker/nvidia-smi.
var DoctorRunCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func doctorRunCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	return DoctorRunCmd(ctx, name, args...)
}

// AllChecks returns the full set of checks for the given config and config file path.
func AllChecks(cfg *marlinConfig.Config, cfgPath string) []Check {
	var checks []Check
	checks = append(checks, configChecks(cfgPath)...)
	checks = append(checks, runtimeChecks(cfg)...)
	checks = append(checks, gpuChecks()...)
	checks = append(checks, secretsChecks(cfg)...)
	checks = append(checks, pathsChecks(cfg)...)
	checks = append(checks, diskChecks(cfg)...)
	return checks
}
