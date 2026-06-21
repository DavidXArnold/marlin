package doctor

import (
	"context"
	"fmt"

	marlinConfig "github.com/DavidXArnold/marlin/internal/config"
)

func configChecks(cfgPath string) []Check {
	return []Check{
		&funcCheck{
			id: "config.loaded",
			run: func(_ context.Context) Result {
				_, err := marlinConfig.Load(cfgPath)
				if err != nil {
					return Result{
						ID:     "config.loaded",
						Level:  LevelFail,
						Detail: fmt.Sprintf("parse error: %v", err),
						Hint:   "fix the TOML syntax in " + cfgPath,
					}
				}
				if cfgPath == "" {
					return Result{ID: "config.loaded", Level: LevelPass, Detail: "using defaults"}
				}
				return Result{ID: "config.loaded", Level: LevelPass, Detail: cfgPath}
			},
		},
	}
}
