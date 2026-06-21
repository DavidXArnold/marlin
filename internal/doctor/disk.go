package doctor

import (
	"context"
	"fmt"
	"syscall"

	"github.com/DavidXArnold/marlin/internal/config"
)

const (
	minModelsDirGiB float64 = 50
	minNIMCacheGiB  float64 = 100
)

// statfsFunc is injectable for tests.
var statfsFunc = func(path string) (syscall.Statfs_t, error) {
	var st syscall.Statfs_t
	return st, syscall.Statfs(path, &st)
}

func freeGiB(path string) (float64, error) {
	st, err := statfsFunc(path)
	if err != nil {
		return 0, err
	}
	return float64(st.Bavail) * float64(st.Bsize) / (1 << 30), nil
}

func diskChecks(cfg *config.Config) []Check {
	return []Check{
		&funcCheck{
			id: "disk.models_dir",
			run: func(_ context.Context) Result {
				free, err := freeGiB(cfg.Paths.ModelsDir)
				if err != nil {
					return Result{ID: "disk.models_dir", Level: LevelWarn, Detail: fmt.Sprintf("cannot check: %v", err)}
				}
				if free < minModelsDirGiB {
					return Result{
						ID:     "disk.models_dir",
						Level:  LevelWarn,
						Detail: fmt.Sprintf("%.0f GiB free (want ≥%0.f GiB)", free, minModelsDirGiB),
						Hint:   "free up disk space for model downloads",
					}
				}
				return Result{ID: "disk.models_dir", Level: LevelPass, Detail: fmt.Sprintf("%.0f GiB free", free)}
			},
		},
		&funcCheck{
			id: "disk.nim_cache",
			run: func(_ context.Context) Result {
				free, err := freeGiB(cfg.Paths.NIMCache)
				if err != nil {
					return Result{ID: "disk.nim_cache", Level: LevelWarn, Detail: fmt.Sprintf("cannot check: %v", err)}
				}
				if free < minNIMCacheGiB {
					return Result{
						ID:     "disk.nim_cache",
						Level:  LevelWarn,
						Detail: fmt.Sprintf("%.0f GiB free (want ≥%.0f GiB)", free, minNIMCacheGiB),
						Hint:   "free up disk space for NIM image caching",
					}
				}
				return Result{ID: "disk.nim_cache", Level: LevelPass, Detail: fmt.Sprintf("%.0f GiB free", free)}
			},
		},
	}
}
