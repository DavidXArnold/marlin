package render

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/DavidXArnold/marlin/internal/config"
)

// lookupUserHomeFunc is injectable for tests.
var lookupUserHomeFunc = func(username string) (string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

// ResolveVLLMBin returns the binary to use in the systemd ExecStart.
// If configured is non-empty it is used as-is (absolute path or name).
// Otherwise exec.LookPath resolves "vllm" against the current PATH.
// When running under sudo, PATH is stripped; falls back to checking the
// invoking user's common venv locations via SUDO_USER.
// Falls back to the bare name "vllm" if nothing is found; callers that care
// should warn the user in that case.
func ResolveVLLMBin(configured string) (string, bool) {
	if configured != "" {
		return configured, true
	}
	if found, err := exec.LookPath("vllm"); err == nil {
		return found, true
	}
	// sudo strips PATH; probe the invoking user's common venv locations.
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if home, err := lookupUserHomeFunc(sudoUser); err == nil {
			for _, rel := range []string{
				filepath.Join(".venv", "bin", "vllm"),
				filepath.Join("venv", "bin", "vllm"),
				filepath.Join(".local", "bin", "vllm"),
			} {
				c := filepath.Join(home, rel)
				if _, statErr := os.Stat(c); statErr == nil {
					return c, true
				}
			}
		}
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
StartLimitIntervalSec=300
StartLimitBurst=5

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

// ResolveContainerBin returns the container runtime binary for the containerized vLLM unit.
// If ContainerRuntime is configured it is used (LookPath for the full path when possible).
// Otherwise probes docker → podman → nerdctl. Falls back to "docker" with ok=false.
func ResolveContainerBin(cfg *config.Config) (string, bool) {
	if rt := cfg.Service.ContainerRuntime; rt != "" {
		if found, err := exec.LookPath(rt); err == nil {
			return found, true
		}
		return rt, false
	}
	for _, name := range []string{"docker", "podman", "nerdctl"} {
		if found, err := exec.LookPath(name); err == nil {
			return found, true
		}
	}
	return "docker", false
}

// SystemdUnitContainerized renders a systemd unit that runs vLLM inside a container.
// The unit reads VLLM_IMAGE and VLLM_MODEL from the active model env file (written by
// marlin start), so switching models updates the container automatically.
// containerBin is the resolved path to docker/podman/nerdctl (see ResolveContainerBin).
func SystemdUnitContainerized(cfg *config.Config, containerBin string) string {
	return fmt.Sprintf(`[Unit]
Description=Marlin vLLM inference service (containerized)
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
EnvironmentFile=-%s
EnvironmentFile=%s
ExecStartPre=-%s rm -f marlin-vllm
ExecStart=/bin/bash -c 'exec %s run --rm --gpus all --ipc host --network host --name marlin-vllm --label marlin.managed=true --entrypoint vllm -e "HF_TOKEN=${HF_TOKEN:-}" "${VLLM_IMAGE}" serve "${VLLM_MODEL}" --host 0.0.0.0 --port %d ${VLLM_EXTRA_ARGS:-}'
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
		containerBin,
		containerBin,
		cfg.Server.Port,
		cfg.Service.SystemdUnit,
	)
}

// llamaCppContainerName is the fixed name of the container run by the
// llama.cpp unit, so ExecStartPre can clear a stale one left by a crash.
const llamaCppContainerName = "marlin-llamacpp"

// ResolveHFCache returns the host Hugging Face cache directory mounted into the
// llama.cpp container. paths.hf_cache wins; otherwise the invoking user's
// ~/.cache/huggingface (via SUDO_USER, since install runs under sudo), so
// weights already downloaded by that user are reused instead of re-fetched.
func ResolveHFCache(cfg *config.Config) string {
	if cfg.Paths.HFCache != "" {
		return cfg.Paths.HFCache
	}
	home := ""
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if h, err := lookupUserHomeFunc(sudoUser); err == nil {
			home = h
		}
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	if home == "" {
		return "/root/.cache/huggingface"
	}
	return filepath.Join(home, ".cache", "huggingface")
}

// SystemdUnitLlamaCppContainerized renders a systemd unit that runs llama-server
// inside a container. The unit reads LLAMA_IMAGE, LLAMA_MODEL_ARGS (-hf <ref> or
// -m <file>), LLAMA_CONTAINER_ARGS and LLAMA_EXTRA_ARGS from the active llama.cpp
// env file, so switching models only rewrites that file. hfCache is mounted at
// the container's HF cache so -hf downloads persist across restarts.
//
// The health check is overridden because the image's built-in one probes the
// default port 8080, which would mark the container unhealthy on any other port.
// nerdctl has no --health-cmd, so it gets none.
func SystemdUnitLlamaCppContainerized(cfg *config.Config, containerBin, hfCache string) string {
	health := ""
	if filepath.Base(containerBin) != "nerdctl" {
		health = fmt.Sprintf(`--health-cmd "curl -fsS http://localhost:%d/health || exit 1" `, cfg.Server.Port)
	}
	return fmt.Sprintf(`[Unit]
Description=Marlin llama.cpp inference service (containerized)
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
EnvironmentFile=-%s
EnvironmentFile=%s
ExecStartPre=-%s rm -f %s
ExecStart=/bin/bash -c 'exec %s run --rm --gpus all --ipc host --network host --name %s --label marlin.managed=true --label marlin.provider=llamacpp -e "HF_TOKEN=${HF_TOKEN:-}" -v "%s:/root/.cache/huggingface" %s${LLAMA_CONTAINER_ARGS:-} "${LLAMA_IMAGE}" ${LLAMA_MODEL_ARGS} --host 0.0.0.0 --port %d ${LLAMA_EXTRA_ARGS:-}'
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`,
		cfg.Paths.SecretsEnv,
		llamaCppEnvFile(cfg),
		containerBin, llamaCppContainerName,
		containerBin, llamaCppContainerName,
		hfCache,
		health,
		cfg.Server.Port,
		cfg.Service.LlamaCppUnit,
	)
}

// SystemdUnitLlamaCpp renders a systemd unit that runs a host llama-server
// binary (service.llamacpp_mode = "binary").
func SystemdUnitLlamaCpp(cfg *config.Config, llamaBin string) string {
	return fmt.Sprintf(`[Unit]
Description=Marlin llama.cpp inference service
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
EnvironmentFile=-%s
EnvironmentFile=%s
ExecStart=/bin/bash -c 'exec %s ${LLAMA_MODEL_ARGS} --host %s --port %d ${LLAMA_EXTRA_ARGS:-}'
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=multi-user.target
`,
		cfg.Paths.SecretsEnv,
		llamaCppEnvFile(cfg),
		llamaBin,
		cfg.Server.Host,
		cfg.Server.Port,
		cfg.Service.LlamaCppUnit,
	)
}

// ResolveLlamaCppBin returns the llama-server binary for the binary-mode unit:
// service.llamacpp_bin when set, else llama-server from PATH. ok is false when
// nothing was found and the bare name is returned.
func ResolveLlamaCppBin(configured string) (string, bool) {
	if configured != "" {
		return configured, true
	}
	if found, err := exec.LookPath("llama-server"); err == nil {
		return found, true
	}
	return "llama-server", false
}

func llamaCppEnvFile(cfg *config.Config) string {
	if cfg.Paths.LlamaCppEnvFile != "" {
		return cfg.Paths.LlamaCppEnvFile
	}
	return "/etc/marlin/llamacpp.env"
}

// LlamaCppUnitPath returns where the llama.cpp unit file is installed.
func LlamaCppUnitPath(cfg *config.Config) string {
	return "/etc/systemd/system/" + cfg.Service.LlamaCppUnit + ".service"
}

// SystemdUnitPath returns the filesystem path where the unit file should be installed.
func SystemdUnitPath(cfg *config.Config) string {
	return "/etc/systemd/system/" + cfg.Service.SystemdUnit + ".service"
}
