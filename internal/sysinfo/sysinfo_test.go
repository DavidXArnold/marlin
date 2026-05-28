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
		return []byte("0, NVIDIA A100-SXM4-80GB, 81920, 75000\n1, NVIDIA A100-SXM4-80GB, 81920, 70000\n"), nil
	}
	defer func() { runNvidiaSmi = old }()

	gpus := detectGPUs()
	require.Len(t, gpus, 2)
	assert.Equal(t, 0, gpus[0].Index)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", gpus[0].Name)
	assert.Equal(t, uint64(81920), gpus[0].VRAMTotalMB)
	assert.Equal(t, uint64(75000), gpus[0].VRAMFreeMB)
	assert.Equal(t, 1, gpus[1].Index)
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
