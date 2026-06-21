package top

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

func sampleFn(info *sysinfo.SystemInfo, err error) SampleFn {
	return func() (*sysinfo.SystemInfo, error) { return info, err }
}

func TestBar(t *testing.T) {
	assert.Equal(t, "[████████████████████]", bar(1.0, 20))
	assert.Equal(t, "[░░░░░░░░░░░░░░░░░░░░]", bar(0.0, 20))
	assert.Equal(t, "[██████████░░░░░░░░░░]", bar(0.5, 20))
	assert.Equal(t, "[████████████████████]", bar(1.5, 20)) // clamped
	assert.Equal(t, "[░░░░░░░░░░░░░░░░░░░░]", bar(-0.5, 20)) // clamped
}

func TestViewNoInfo(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	out := m.View()
	assert.Contains(t, out, "collecting")
}

func TestViewWithGPU(t *testing.T) {
	info := &sysinfo.SystemInfo{
		GPUs: []sysinfo.GPUInfo{
			{
				Index: 0, Name: "GH200 480GB",
				VRAMTotalMB: 480 * 1024, VRAMFreeMB: 240 * 1024,
				PowerDrawW: 350, PowerLimitW: 700,
				TempC: 72, MemTempC: 60,
				GraphicsClockMHz: 1980, MemClockMHz: 2400,
			},
		},
		RAMTotalMB: 128 * 1024,
		RAMFreeMB:  96 * 1024,
	}
	m := New(sampleFn(info, nil), StatusLine{Model: "llama", Provider: "vllm", Running: true})
	m.info = info

	out := m.View()
	assert.Contains(t, out, "GH200 480GB")
	assert.Contains(t, out, "480 GiB")
	assert.Contains(t, out, "350")
	assert.Contains(t, out, "72°C")
	assert.Contains(t, out, "1980 MHz")
	assert.Contains(t, out, "llama")
	assert.Contains(t, out, "● running")
	assert.Contains(t, out, "RAM")
}

func TestViewWithUMAGPU(t *testing.T) {
	info := &sysinfo.SystemInfo{
		GPUs: []sysinfo.GPUInfo{
			{Index: 0, Name: "GB10", VRAMTotalMB: 0, IsUMA: true},
		},
	}
	m := New(sampleFn(info, nil), StatusLine{})
	m.info = info
	out := m.View()
	assert.Contains(t, out, "unified memory")
}

func TestViewError(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	m.err = assert.AnError
	out := m.View()
	assert.Contains(t, out, "error")
}

func TestViewQuitting(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	m.quitting = true
	assert.Empty(t, m.View())
}

func TestViewStoppedModel(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{Model: "qwen", Provider: "nim", Running: false})
	m.info = &sysinfo.SystemInfo{}
	out := m.View()
	assert.Contains(t, out, "● stopped")
}

func TestUpdateQuit(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	assert.NotNil(t, cmd)
	assert.True(t, updated.(*Model).quitting)
}

func TestUpdateWindowSize(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, updated.(*Model).width)
}

func TestUpdateTick(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	_, cmd := m.Update(tickMsg{})
	assert.NotNil(t, cmd)
}

func TestUpdateSample(t *testing.T) {
	m := New(sampleFn(nil, nil), StatusLine{})
	info := &sysinfo.SystemInfo{RAMTotalMB: 1024}
	updated, _ := m.Update(sampleMsg{info: info})
	assert.Equal(t, info, updated.(*Model).info)
}

func TestInitReturnsCmd(t *testing.T) {
	m := New(sampleFn(&sysinfo.SystemInfo{}, nil), StatusLine{})
	cmd := m.Init()
	assert.NotNil(t, cmd)
}

func TestBarOutput(t *testing.T) {
	b := bar(0.25, 20)
	assert.True(t, strings.HasPrefix(b, "["))
	assert.True(t, strings.HasSuffix(b, "]"))
	assert.Equal(t, 22, len([]rune(b))) // [+20 chars+]
}
