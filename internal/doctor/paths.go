package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DavidXArnold/marlin/internal/config"
)

func pathsChecks(cfg *config.Config) []Check {
	modelsDir := cfg.Paths.ModelsDir
	nimCache := cfg.Paths.NIMCache
	stateDir := filepath.Dir(cfg.Paths.StateFile)

	return []Check{
		&funcCheck{
			id: "paths.models_dir",
			run: func(_ context.Context) Result {
				if _, err := os.Stat(modelsDir); os.IsNotExist(err) {
					return Result{
						ID:     "paths.models_dir",
						Level:  LevelFail,
						Detail: "does not exist",
						Hint:   fmt.Sprintf("run with --fix to create %s", modelsDir),
						CanFix: true,
					}
				}
				f, err := os.CreateTemp(modelsDir, ".marlin-write-test-*")
				if err != nil {
					return Result{
						ID:     "paths.models_dir",
						Level:  LevelFail,
						Detail: "not writable",
						Hint:   fmt.Sprintf("ensure %s is writable by the current user", modelsDir),
					}
				}
				_ = f.Close()
				_ = os.Remove(f.Name())
				return Result{ID: "paths.models_dir", Level: LevelPass, Detail: modelsDir}
			},
			fix: func(_ context.Context) error {
				return os.MkdirAll(modelsDir, 0o755)
			},
		},
		&funcCheck{
			id: "paths.nim_cache",
			run: func(_ context.Context) Result {
				fi, err := os.Stat(nimCache)
				if os.IsNotExist(err) {
					return Result{
						ID:     "paths.nim_cache",
						Level:  LevelWarn,
						Detail: "does not exist",
						Hint:   fmt.Sprintf("run with --fix to create %s", nimCache),
						CanFix: true,
					}
				}
				if err != nil {
					return Result{ID: "paths.nim_cache", Level: LevelWarn, Detail: err.Error()}
				}
				if fi.Mode().Perm()&0o777 != 0o777 {
					return Result{
						ID:     "paths.nim_cache",
						Level:  LevelWarn,
						Detail: fmt.Sprintf("%04o (want 0777 for NIM containers)", fi.Mode().Perm()),
						Hint:   "run with --fix to chmod 0777",
						CanFix: true,
					}
				}
				return Result{ID: "paths.nim_cache", Level: LevelPass, Detail: nimCache}
			},
			fix: func(_ context.Context) error {
				if err := os.MkdirAll(nimCache, 0o777); err != nil {
					return err
				}
				return os.Chmod(nimCache, 0o777)
			},
		},
		&funcCheck{
			id: "paths.state_dir",
			run: func(_ context.Context) Result {
				if _, err := os.Stat(stateDir); os.IsNotExist(err) {
					return Result{
						ID:     "paths.state_dir",
						Level:  LevelWarn,
						Detail: "does not exist",
						Hint:   fmt.Sprintf("run with --fix to create %s", stateDir),
						CanFix: true,
					}
				}
				return Result{ID: "paths.state_dir", Level: LevelPass, Detail: stateDir}
			},
			fix: func(_ context.Context) error {
				return os.MkdirAll(stateDir, 0o755)
			},
		},
	}
}
