package render

import (
	"fmt"

	"github.com/DavidXArnold/marlin/internal/config"
)

// SystemdUnit renders a systemd service unit file for the marlin vLLM service.
// The unit sources the secrets env file (optional) and the active model env
// symlink, then runs vllm serve with the configured host, port, and extra args.
func SystemdUnit(cfg *config.Config) string {
	return fmt.Sprintf(`[Unit]
Description=Marlin vLLM inference service
After=network.target

[Service]
Type=simple
EnvironmentFile=-%s
EnvironmentFile=%s
ExecStart=/bin/bash -c 'exec vllm serve "$VLLM_MODEL" --host %s --port %d ${VLLM_EXTRA_ARGS:-}'
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
		cfg.Server.Host,
		cfg.Server.Port,
		cfg.Service.SystemdUnit,
	)
}

// SystemdUnitPath returns the filesystem path where the unit file should be installed.
func SystemdUnitPath(cfg *config.Config) string {
	return "/etc/systemd/system/" + cfg.Service.SystemdUnit + ".service"
}
