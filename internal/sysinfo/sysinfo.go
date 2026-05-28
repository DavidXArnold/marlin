package sysinfo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// Injectable for tests.
var (
	runNvidiaSmi = func() ([]byte, error) {
		return exec.Command("nvidia-smi",
			"--query-gpu=index,name,memory.total,memory.free",
			"--format=csv,noheader,nounits").Output()
	}
	readMeminfo = func() ([]byte, error) {
		return os.ReadFile("/proc/meminfo")
	}
	statfsAt = func(path string) (syscall.Statfs_t, error) {
		var st syscall.Statfs_t
		return st, syscall.Statfs(path, &st)
	}
)

// SystemInfo describes the hardware resources available on this host.
type SystemInfo struct {
	GPUs       []GPUInfo
	RAMTotalMB uint64
	RAMFreeMB  uint64
	Disks      map[string]DiskInfo
}

// GPUInfo holds per-GPU metrics from nvidia-smi.
type GPUInfo struct {
	Index       int
	Name        string
	VRAMTotalMB uint64
	VRAMFreeMB  uint64
}

// DiskInfo holds disk space for a single path.
type DiskInfo struct {
	TotalGB float64
	FreeGB  float64
}

// TotalVRAMMB returns the sum of all GPU VRAM in MB.
func (s *SystemInfo) TotalVRAMMB() uint64 {
	var total uint64
	for _, g := range s.GPUs {
		total += g.VRAMTotalMB
	}
	return total
}

// FreeVRAMMB returns the sum of free VRAM across all GPUs in MB.
func (s *SystemInfo) FreeVRAMMB() uint64 {
	var free uint64
	for _, g := range s.GPUs {
		free += g.VRAMFreeMB
	}
	return free
}

// Detect collects GPU, RAM, and disk information. The paths argument is a list
// of filesystem paths to measure (e.g. models dir, NIM cache). Missing nvidia-smi
// or /proc/meminfo are not errors — the corresponding fields are left zeroed.
func Detect(paths ...string) (*SystemInfo, error) {
	info := &SystemInfo{
		Disks: make(map[string]DiskInfo),
	}
	info.GPUs = detectGPUs()
	info.RAMTotalMB, info.RAMFreeMB = detectRAM()
	for _, p := range paths {
		if d, err := diskUsage(p); err == nil {
			info.Disks[p] = d
		}
	}
	return info, nil
}

func detectGPUs() []GPUInfo {
	out, err := runNvidiaSmi()
	if err != nil {
		return nil
	}
	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ", ")
		if len(parts) != 4 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		name := strings.TrimSpace(parts[1])
		total, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
		free, _ := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
		gpus = append(gpus, GPUInfo{
			Index:       idx,
			Name:        name,
			VRAMTotalMB: total,
			VRAMFreeMB:  free,
		})
	}
	return gpus
}

func detectRAM() (totalMB, freeMB uint64) {
	data, err := readMeminfo()
	if err != nil {
		return 0, 0
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(string(fields[1]), 10, 64)
		switch string(fields[0]) {
		case "MemTotal:":
			totalMB = val / 1024
		case "MemAvailable:":
			freeMB = val / 1024
		}
	}
	return
}

func diskUsage(path string) (DiskInfo, error) {
	st, err := statfsAt(path)
	if err != nil {
		return DiskInfo{}, err
	}
	blockSize := uint64(st.Bsize)
	const gib = float64(1 << 30)
	return DiskInfo{
		TotalGB: float64(st.Blocks*blockSize) / gib,
		FreeGB:  float64(st.Bavail*blockSize) / gib,
	}, nil
}

// FormatMB formats a megabyte count as "X GiB" or "X MiB".
func FormatMB(mb uint64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.0f GiB", float64(mb)/1024)
	}
	return fmt.Sprintf("%d MiB", mb)
}
