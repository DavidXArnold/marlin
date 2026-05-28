package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Behavior   BehaviorConfig   `toml:"behavior"`
	Paths      PathsConfig      `toml:"paths"`
	Service    ServiceConfig    `toml:"service"`
	Server     ServerConfig     `toml:"server"`
	Registries RegistriesConfig `toml:"registries"`
}

type BehaviorConfig struct {
	SwitchPrompt            bool `toml:"switch_prompt"`
	AddAutoDetect           bool `toml:"add_auto_detect"`
	LogTailLines            int  `toml:"log_tail_lines"`
	AllowTypeSwitch         bool `toml:"allow_type_switch"`
	WarnUnmanagedContainers bool `toml:"warn_unmanaged_containers"`
	CheckUpdates            bool `toml:"check_updates"`
}

type PathsConfig struct {
	ModelsDir     string `toml:"models_dir"`
	ActiveSymlink string `toml:"active_symlink"`
	SecretsEnv    string `toml:"secrets_env"`
	StateFile     string `toml:"state_file"`
	NIMCache      string `toml:"nim_cache"` // host path mounted into NIM containers
}

type ServiceConfig struct {
	SystemdUnit      string `toml:"systemd_unit"`
	DockerContainer  string `toml:"docker_container"`
	ContainerRuntime string `toml:"container_runtime"` // "docker" (default), "podman", or "containerd"
	ContainerSocket  string `toml:"container_socket"`  // optional custom socket path (docker/podman)
	VLLMImage        string `toml:"vllm_image"`        // Docker image used for ad-hoc vLLM runs
}

type ServerConfig struct {
	Host  string `toml:"host"`
	Port  int    `toml:"port"`
	Alias string `toml:"alias"`
}

type RegistriesConfig struct {
	HuggingFace RegistryConfig `toml:"huggingface"`
	NGC         RegistryConfig `toml:"ngc"`
	ModelScope  RegistryConfig `toml:"modelscope"`
}

type RegistryConfig struct {
	Enabled bool `toml:"enabled"`
}

// defaultSecretsPath returns the user-local secrets path so that marlin
// configure works without sudo. Falls back to the system path when the home
// directory cannot be determined (e.g. headless service contexts).
func defaultSecretsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/etc/marlin/secrets.env"
	}
	return filepath.Join(home, ".config", "marlin", "secrets.env")
}

func Defaults() *Config {
	return &Config{
		Behavior: BehaviorConfig{
			SwitchPrompt:            true,
			AddAutoDetect:           false,
			LogTailLines:            100,
			AllowTypeSwitch:         true,
			WarnUnmanagedContainers: true,
			CheckUpdates:            true,
		},
		Paths: PathsConfig{
			ModelsDir:     "/etc/marlin/models",
			ActiveSymlink: "/etc/marlin/model.env",
			SecretsEnv:    defaultSecretsPath(),
			StateFile:     "/var/lib/marlin/state.toml",
			NIMCache:      "/var/cache/nim",
		},
		Service: ServiceConfig{
			SystemdUnit:     "marlin",
			DockerContainer: "marlin",
			VLLMImage:       "vllm/vllm-openai:latest",
		},
		Server: ServerConfig{
			Host:  "localhost",
			Port:  8000,
			Alias: "local",
		},
		Registries: RegistriesConfig{
			HuggingFace: RegistryConfig{Enabled: true},
			NGC:         RegistryConfig{Enabled: true},
			ModelScope:  RegistryConfig{Enabled: false},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if _, err := toml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
