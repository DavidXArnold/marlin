package sysinfo

import (
	"fmt"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectGPUsSuccess(t *testing.T) {
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) {
		return []byte("0, NVIDIA A100-SXM4-80GB, 81920, 75000, 8.0\n1, NVIDIA A100-SXM4-80GB, 81920, 70000, 8.0\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 2)
	assert.Equal(t, 0, gpus[0].Index)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", gpus[0].Name)
	assert.Equal(t, uint64(81920), gpus[0].VRAMTotalMB)
	assert.Equal(t, uint64(75000), gpus[0].VRAMFreeMB)
	assert.Equal(t, "8.0", gpus[0].ComputeCap)
	assert.False(t, gpus[0].IsUMA)
	assert.Equal(t, 1, gpus[1].Index)
}

func TestDetectGPUsLegacyFourFields(t *testing.T) {
	// Older drivers that don't emit compute_cap — should still parse cleanly.
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) {
		return []byte("0, NVIDIA A100-SXM4-80GB, 81920, 75000\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 1)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", gpus[0].Name)
	assert.Empty(t, gpus[0].ComputeCap)
	assert.False(t, gpus[0].IsUMA)
}

func TestDetectGPUsUMA_ZeroVRAM(t *testing.T) {
	// GB10 nvidia-smi reports N/A for memory, which our parser leaves as 0.
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) {
		return []byte("0, NVIDIA GB10, 0, 0, 12.1\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 1)
	assert.True(t, gpus[0].IsUMA)
	assert.Equal(t, "12.1", gpus[0].ComputeCap)
}

func TestDetectGPUsUMA_NameMatch(t *testing.T) {
	// GH200 reports VRAM, but should still be flagged UMA by name.
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) {
		return []byte("0, NVIDIA GH200 96GB, 98304, 90000, 9.0\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 1)
	assert.True(t, gpus[0].IsUMA)
}

func TestDetectGPUsComputeCapNA(t *testing.T) {
	// Driver returns [N/A] for compute_cap — should be stored as empty string.
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) {
		return []byte("0, NVIDIA A100, 81920, 75000, [N/A]\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 1)
	assert.Empty(t, gpus[0].ComputeCap)
}

func TestDetectGPUsNotFound(t *testing.T) {
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) { return nil, fmt.Errorf("nvidia-smi: not found") }
	defer func() { runNvidiaSmi = old }()

	assert.Empty(t, detectGPUs())
}

func TestDetectGPUsMalformedLine(t *testing.T) {
	old := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) {
		// One valid line and one with wrong field count — bad line is skipped.
		return []byte("0, A100, 81920, 75000\nbadline\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 1)
	assert.Equal(t, "A100", gpus[0].Name)
}

func TestIsUMAGPU(t *testing.T) {
	cases := []struct {
		name  string
		vram  uint64
		want  bool
	}{
		{"NVIDIA A100-SXM4-80GB", 81920, false},
		{"NVIDIA H100 80GB HBM3", 81920, false},
		{"NVIDIA B200", 192512, false}, // discrete Blackwell, not UMA
		{"NVIDIA GB10", 0, true},       // DGX Spark: VRAMTotal=0
		{"NVIDIA GB10", 128000, true},  // GB10 by name even if driver reports VRAM
		{"NVIDIA GH200 96GB", 98304, true},
		{"NVIDIA GB200", 0, true},
		{"NVIDIA GB300", 0, true},
		{"SomeGPU", 0, true},           // zero VRAM without name → still UMA
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isUMAGPU(c.name, c.vram), "name=%q vram=%d", c.name, c.vram)
	}
}

func TestDetectRAM(t *testing.T) {
	old := readMeminfo
	readMeminfo = func() ([]byte, error) {
		return []byte("MemTotal:       131072 kB\nMemFree:        32768 kB\nMemAvailable:   65536 kB\n"), nil
	}
	defer func() { readMeminfo = old }()

	total, free := detectRAM()
	assert.Equal(t, uint64(128), total) // 131072 / 1024
	assert.Equal(t, uint64(64), free)   // 65536 / 1024
}

func TestDetectRAMMeminfoMissing(t *testing.T) {
	old := readMeminfo
	readMeminfo = func() ([]byte, error) { return nil, fmt.Errorf("no such file") }
	defer func() { readMeminfo = old }()

	total, free := detectRAM()
	assert.Equal(t, uint64(0), total)
	assert.Equal(t, uint64(0), free)
}

func TestDiskUsage(t *testing.T) {
	// Use a real temp dir — cross-platform and exercises real syscall path.
	dir := t.TempDir()
	d, err := diskUsage(dir)
	require.NoError(t, err)
	assert.Greater(t, d.TotalGB, 0.0)
	assert.Greater(t, d.FreeGB, 0.0)
}

func TestDiskUsageNotFound(t *testing.T) {
	old := statfsAt
	statfsAt = func(_ string) (syscall.Statfs_t, error) { return syscall.Statfs_t{}, fmt.Errorf("no device") }
	defer func() { statfsAt = old }()

	_, err := diskUsage("/nonexistent")
	assert.Error(t, err)
}

func TestSystemInfoHelpers(t *testing.T) {
	si := &SystemInfo{
		GPUs: []GPUInfo{
			{VRAMTotalMB: 80000, VRAMFreeMB: 70000},
			{VRAMTotalMB: 80000, VRAMFreeMB: 60000},
		},
	}
	assert.Equal(t, uint64(160000), si.TotalVRAMMB())
	assert.Equal(t, uint64(130000), si.FreeVRAMMB())
}

func TestDetect(t *testing.T) {
	oldGPU := runNvidiaSmi
	runNvidiaSmi = func() ([]byte, error) { return nil, fmt.Errorf("not found") }
	defer func() { runNvidiaSmi = oldGPU }()

	dir := t.TempDir()
	info, err := Detect(dir)
	require.NoError(t, err)
	assert.Empty(t, info.GPUs)
	assert.Contains(t, info.Disks, dir)
	assert.Greater(t, info.Disks[dir].TotalGB, 0.0)
}

func TestFormatMB(t *testing.T) {
	assert.Equal(t, "80 GiB", FormatMB(80*1024))
	assert.Equal(t, "512 MiB", FormatMB(512))
	assert.Equal(t, "1 GiB", FormatMB(1024))
}

func TestLoadAvg1(t *testing.T) {
	old := readLoadavg
	defer func() { readLoadavg = old }()

	readLoadavg = func() ([]byte, error) { return []byte("1.25 0.75 0.50 2/400 12345\n"), nil }
	assert.InDelta(t, 1.25, LoadAvg1(), 0.001)

	readLoadavg = func() ([]byte, error) { return nil, fmt.Errorf("no /proc") }
	assert.Equal(t, 0.0, LoadAvg1())

	readLoadavg = func() ([]byte, error) { return []byte(""), nil }
	assert.Equal(t, 0.0, LoadAvg1())
}

func TestSampleTelemetry(t *testing.T) {
	restore := SetRunNvidiaSmiTelemetryForTest(func() ([]byte, error) {
		return []byte("0, 142.5, 240.0, 71, 65, 1455, 2619\n"), nil
	})
	defer restore()

	si := &SystemInfo{
		GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GB10"}},
	}
	SampleTelemetry(si)
	assert.InDelta(t, 142.5, si.GPUs[0].PowerDrawW, 0.01)
	assert.InDelta(t, 240.0, si.GPUs[0].PowerLimitW, 0.01)
	assert.InDelta(t, 71.0, si.GPUs[0].TempC, 0.01)
	assert.Equal(t, 1455, si.GPUs[0].GraphicsClockMHz)
}

func TestSampleTelemetryNvidiaSmiMissing(t *testing.T) {
	restore := SetRunNvidiaSmiTelemetryForTest(func() ([]byte, error) {
		return nil, fmt.Errorf("nvidia-smi not found")
	})
	defer restore()

	si := &SystemInfo{GPUs: []GPUInfo{{Index: 0}}}
	SampleTelemetry(si) // must not panic
	assert.Equal(t, 0.0, si.GPUs[0].PowerDrawW)
}

func TestSampleTelemetryNAFields(t *testing.T) {
	restore := SetRunNvidiaSmiTelemetryForTest(func() ([]byte, error) {
		return []byte("0, [N/A], [N/A], 45, N/A, 1200, N/A\n"), nil
	})
	defer restore()

	si := &SystemInfo{GPUs: []GPUInfo{{Index: 0}}}
	SampleTelemetry(si)
	assert.Equal(t, 0.0, si.GPUs[0].PowerDrawW)
	assert.InDelta(t, 45.0, si.GPUs[0].TempC, 0.01)
	assert.Equal(t, 1200, si.GPUs[0].GraphicsClockMHz)
}
