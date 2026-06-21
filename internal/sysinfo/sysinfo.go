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
var readLoadavg = func() ([]byte, error) {
	return os.ReadFile("/proc/loadavg")
}

var (
	runNvidiaSmi = func() ([]byte, error) {
		return exec.Command("nvidia-smi",
			"--query-gpu=index,name,memory.total,memory.free,compute_cap",
			"--format=csv,noheader,nounits").Output()
	}
	runNvidiaSmiTelemetry = func() ([]byte, error) {
		return exec.Command("nvidia-smi",
			"--query-gpu=index,power.draw,power.limit,temperature.gpu,temperature.memory,clocks.gr,clocks.mem",
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
	ComputeCap  string // e.g. "12.1", "10.0", "9.0" — empty if unavailable
	IsUMA       bool   // true for unified-memory architectures (GB10, GH200, GB200, GB300)

	// Telemetry fields — populated by SampleTelemetry, zeroed by Detect.
	PowerDrawW      float64 // current power draw in watts
	PowerLimitW     float64 // power limit in watts
	TempC           float64 // GPU junction temp in Celsius
	MemTempC        float64 // memory temp in Celsius (0 if unavailable)
	GraphicsClockMHz int    // current graphics clock in MHz
	MemClockMHz      int    // current memory clock in MHz
}

// umaGPUNames lists GPU model strings that use unified CPU+GPU memory.
// nvidia-smi reports memory.total as N/A for these, causing VRAMTotalMB==0,
// but name-matching lets us detect them even if a future driver fixes that.
var umaGPUNames = []string{"GB10", "GH200", "GB200", "GB300"}

// isUMAGPU returns true when the GPU uses a unified memory architecture.
func isUMAGPU(name string, vramTotal uint64) bool {
	if vramTotal == 0 {
		return true
	}
	upper := strings.ToUpper(name)
	for _, n := range umaGPUNames {
		if strings.Contains(upper, n) {
			return true
		}
	}
	return false
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
		if len(parts) < 4 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		name := strings.TrimSpace(parts[1])
		total, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
		free, _ := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
		var cc string
		if len(parts) >= 5 {
			cc = strings.TrimSpace(parts[4])
			if cc == "[N/A]" || cc == "N/A" || cc == "" {
				cc = ""
			}
		}
		g := GPUInfo{
			Index:       idx,
			Name:        name,
			VRAMTotalMB: total,
			VRAMFreeMB:  free,
			ComputeCap:  cc,
		}
		g.IsUMA = isUMAGPU(g.Name, g.VRAMTotalMB)
		gpus = append(gpus, g)
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

// LoadAvg1 returns the 1-minute load average from /proc/loadavg, or 0 if unavailable.
func LoadAvg1() float64 {
	data, err := readLoadavg()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// SampleTelemetry fills power, temperature, and clock fields on each GPUInfo
// in si.GPUs. Missing or unavailable fields are silently zeroed.
// This is a separate call from Detect so the heavier query is opt-in.
func SampleTelemetry(si *SystemInfo) {
	out, err := runNvidiaSmiTelemetry()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ", ")
		if len(parts) < 7 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		powerDraw := parseNvFloat(parts[1])
		powerLimit := parseNvFloat(parts[2])
		tempGPU := parseNvFloat(parts[3])
		tempMem := parseNvFloat(parts[4])
		clockGr := int(parseNvFloat(parts[5]))
		clockMem := int(parseNvFloat(parts[6]))
		for i := range si.GPUs {
			if si.GPUs[i].Index == idx {
				si.GPUs[i].PowerDrawW = powerDraw
				si.GPUs[i].PowerLimitW = powerLimit
				si.GPUs[i].TempC = tempGPU
				si.GPUs[i].MemTempC = tempMem
				si.GPUs[i].GraphicsClockMHz = clockGr
				si.GPUs[i].MemClockMHz = clockMem
				break
			}
		}
	}
}

// parseNvFloat parses an nvidia-smi field, returning 0 for N/A or empty.
func parseNvFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "[N/A]" || s == "N/A" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// SetRunNvidiaSmiForTest replaces the nvidia-smi runner for tests and returns
// a restore function. Only call from test code.
func SetRunNvidiaSmiForTest(fn func() ([]byte, error)) func() {
	old := runNvidiaSmi
	runNvidiaSmi = fn
	return func() { runNvidiaSmi = old }
}

// SetRunNvidiaSmiTelemetryForTest replaces the telemetry runner for tests.
func SetRunNvidiaSmiTelemetryForTest(fn func() ([]byte, error)) func() {
	old := runNvidiaSmiTelemetry
	runNvidiaSmiTelemetry = fn
	return func() { runNvidiaSmiTelemetry = old }
}

// FormatMB formats a megabyte count as "X GiB" or "X MiB".
func FormatMB(mb uint64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.0f GiB", float64(mb)/1024)
	}
	return fmt.Sprintf("%d MiB", mb)
}
