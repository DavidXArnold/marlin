package doctor

import (
	"context"
	"strings"
)

func gpuChecks() []Check {
	return []Check{
		&funcCheck{id: "gpu.driver", run: checkGPUDriver},
		&funcCheck{id: "gpu.compute_cap", run: checkComputeCap},
		&funcCheck{id: "gpu.uma", run: checkUMA},
	}
}

func checkGPUDriver(ctx context.Context) Result {
	out, err := doctorRunCmd(ctx, "nvidia-smi",
		"--query-gpu=driver_version",
		"--format=csv,noheader")
	if err != nil {
		return Result{
			ID:     "gpu.driver",
			Level:  LevelWarn,
			Detail: "nvidia-smi not available",
			Hint:   "install NVIDIA drivers or nvidia-smi to enable GPU checks",
		}
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return Result{ID: "gpu.driver", Level: LevelWarn, Detail: "no GPU detected"}
	}
	return Result{ID: "gpu.driver", Level: LevelPass, Detail: "driver " + version}
}

func checkComputeCap(ctx context.Context) Result {
	out, err := doctorRunCmd(ctx, "nvidia-smi",
		"--query-gpu=compute_cap",
		"--format=csv,noheader")
	if err != nil {
		return Result{ID: "gpu.compute_cap", Level: LevelWarn, Detail: "nvidia-smi not available"}
	}
	val := strings.TrimSpace(string(out))
	if val == "" || val == "N/A" {
		return Result{ID: "gpu.compute_cap", Level: LevelWarn, Detail: "unknown"}
	}
	return Result{ID: "gpu.compute_cap", Level: LevelPass, Detail: "compute cap " + val}
}

func checkUMA(_ context.Context) Result {
	out, err := doctorRunCmd(context.Background(), "nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader")
	if err != nil {
		return Result{ID: "gpu.uma", Level: LevelPass, Detail: "no GPU / not applicable"}
	}
	umaNames := []string{"GB10", "GH200", "GB200", "GB300"}
	for _, line := range strings.Split(string(out), "\n") {
		upper := strings.ToUpper(line)
		for _, n := range umaNames {
			if strings.Contains(upper, n) {
				return Result{
					ID:     "gpu.uma",
					Level:  LevelPass,
					Detail: "unified memory architecture detected (" + n + ")",
				}
			}
		}
	}
	return Result{ID: "gpu.uma", Level: LevelPass, Detail: "not a UMA GPU"}
}
