package doctor

import (
	"context"
	"fmt"
	"os"

	"github.com/DavidXArnold/marlin/internal/config"
	marlinSecrets "github.com/DavidXArnold/marlin/internal/secrets"
)

func secretsChecks(cfg *config.Config) []Check {
	path := cfg.Paths.SecretsEnv
	return []Check{
		&funcCheck{
			id: "secrets.file_exists",
			run: func(_ context.Context) Result {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					return Result{
						ID:     "secrets.file_exists",
						Level:  LevelWarn,
						Detail: "not found",
						Hint:   fmt.Sprintf("run 'marlin configure' to create %s", path),
					}
				}
				return Result{ID: "secrets.file_exists", Level: LevelPass, Detail: path}
			},
		},
		&funcCheck{
			id: "secrets.file_mode",
			run: func(_ context.Context) Result {
				fi, err := os.Stat(path)
				if err != nil {
					return Result{ID: "secrets.file_mode", Level: LevelWarn, Detail: "file not found, skipping mode check"}
				}
				mode := fi.Mode().Perm()
				if mode != 0o600 {
					return Result{
						ID:     "secrets.file_mode",
						Level:  LevelWarn,
						Detail: fmt.Sprintf("%04o (want 0600)", mode),
						Hint:   "run with --fix to correct",
						CanFix: true,
					}
				}
				return Result{ID: "secrets.file_mode", Level: LevelPass, Detail: "0600"}
			},
			fix: func(_ context.Context) error {
				return os.Chmod(path, 0o600)
			},
		},
		&funcCheck{
			id: "secrets.hf_token_set",
			run: func(_ context.Context) Result {
				m, err := marlinSecrets.Load(path)
				if err != nil || m["HF_TOKEN"] == "" {
					return Result{
						ID:     "secrets.hf_token_set",
						Level:  LevelWarn,
						Detail: "HF_TOKEN not set",
						Hint:   "run 'marlin configure' to set your HuggingFace token",
					}
				}
				return Result{ID: "secrets.hf_token_set", Level: LevelPass, Detail: "set"}
			},
		},
		&funcCheck{
			id: "secrets.ngc_key_set",
			run: func(_ context.Context) Result {
				m, err := marlinSecrets.Load(path)
				if err != nil || m["NGC_API_KEY"] == "" {
					return Result{
						ID:     "secrets.ngc_key_set",
						Level:  LevelWarn,
						Detail: "NGC_API_KEY not set",
						Hint:   "run 'marlin configure' to set your NGC API key (required for NIM)",
					}
				}
				return Result{ID: "secrets.ngc_key_set", Level: LevelPass, Detail: "set"}
			},
		},
	}
}
