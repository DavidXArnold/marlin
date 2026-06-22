package render

import (
	"fmt"
	"os/exec"

	"github.com/DavidXArnold/marlin/internal/config"
)

// ResolveVLLMBin returns the binary to use in the systemd ExecStart.
// If configured is non-empty it is used as-is (absolute path or name).
// Otherwise exec.LookPath resolves "vllm" against the current PATH.
// Falls back to the bare name "vllm" if nothing is found; callers that care
// should warn the user in that case.
func ResolveVLLMBin(configured string) (string, bool) {
	if configured != "" {
		return configured, true
	}
	if found, err := exec.LookPath("vllm"); err == nil {
		return found, true
	}
	return "vllm", false
}

// SystemdUnit renders a systemd service unit file for the marlin vLLM service.
// vllmBin is the resolved path to the vllm binary (see ResolveVLLMBin).
// The unit sources the secrets env file (optional) and the active model env
// symlink, then runs vllm serve with the configured host, port, and extra args.
func SystemdUnit(cfg *config.Config, vllmBin string) string {
	return fmt.Sprintf(`[Unit]
Description=Marlin vLLM inference service
After=network.target

[Service]
Type=simple
EnvironmentFile=-%s
EnvironmentFile=%s
ExecStart=/bin/bash -c 'exec %s serve "$VLLM_MODEL" --host %s --port %d ${VLLM_EXTRA_ARGS:-}'
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`,
		cfg.Paths.SecretsEnv,
		cfg.Paths.ActiveSymlink,
		vllmBin,
		cfg.Server.Host,
		cfg.Server.Port,
		cfg.Service.SystemdUnit,
	)
}

// SystemdUnitPath returns the filesystem path where the unit file should be installed.
func SystemdUnitPath(cfg *config.Config) string {
	return "/etc/systemd/system/" + cfg.Service.SystemdUnit + ".service"
}
