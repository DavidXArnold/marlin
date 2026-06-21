package top

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

const (
	barWidth    = 20
	refreshRate = 2 * time.Second
)

// SampleFn collects a system snapshot. Injectable for tests.
type SampleFn func() (*sysinfo.SystemInfo, error)

// StatusLine is a one-line description of the active model, shown at the bottom.
type StatusLine struct {
	Model    string
	Provider string
	Running  bool
}

// Model is the bubbletea model for marlin top.
type Model struct {
	sample  SampleFn
	info    *sysinfo.SystemInfo
	status  StatusLine
	err     error
	width   int
	quitting bool
}

// New creates a Model with the given sampler and initial status.
func New(sample SampleFn, status StatusLine) *Model {
	return &Model{sample: sample, status: status, width: 80}
}

type tickMsg time.Time
type sampleMsg struct {
	info *sysinfo.SystemInfo
	err  error
}

// Init kicks off the first sample and the refresh ticker.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(doSample(m.sample), tick())
}

func doSample(fn SampleFn) tea.Cmd {
	return func() tea.Msg {
		info, err := fn()
		return sampleMsg{info: info, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshRate, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		return m, tea.Batch(doSample(m.sample), tick())
	case sampleMsg:
		m.info = msg.info
		m.err = msg.err
	}
	return m, nil
}

// View renders the dashboard.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.err != nil {
		return fmt.Sprintf("error collecting system info: %v\n\npress q to quit\n", m.err)
	}
	if m.info == nil {
		return "collecting system info…\n"
	}

	var sb strings.Builder

	sb.WriteString("marlin top  (q to quit)\n\n")

	for _, g := range m.info.GPUs {
		_, _ = fmt.Fprintf(&sb, "GPU %d  %s\n", g.Index, g.Name)

		vramUsed := g.VRAMTotalMB - g.VRAMFreeMB
		if g.VRAMTotalMB > 0 {
			pct := float64(vramUsed) / float64(g.VRAMTotalMB)
			_, _ = fmt.Fprintf(&sb, "  VRAM   %s  %s / %s  (%.0f%%)\n",
				bar(pct, barWidth),
				sysinfo.FormatMB(vramUsed),
				sysinfo.FormatMB(g.VRAMTotalMB),
				pct*100)
		} else {
			_, _ = fmt.Fprintf(&sb, "  VRAM   (unified memory)\n")
		}

		if g.PowerLimitW > 0 {
			pct := g.PowerDrawW / g.PowerLimitW
			_, _ = fmt.Fprintf(&sb, "  Power  %s  %.0f / %.0f W  (%.0f%%)\n",
				bar(pct, barWidth), g.PowerDrawW, g.PowerLimitW, pct*100)
		}

		if g.TempC > 0 {
			_, _ = fmt.Fprintf(&sb, "  Temp   %.0f°C", g.TempC)
			if g.MemTempC > 0 {
				_, _ = fmt.Fprintf(&sb, "  Mem %.0f°C", g.MemTempC)
			}
			sb.WriteString("\n")
		}

		if g.GraphicsClockMHz > 0 {
			_, _ = fmt.Fprintf(&sb, "  Clocks %d MHz GR  %d MHz MEM\n",
				g.GraphicsClockMHz, g.MemClockMHz)
		}
		sb.WriteString("\n")
	}

	if m.info.RAMTotalMB > 0 {
		ramUsed := m.info.RAMTotalMB - m.info.RAMFreeMB
		pct := float64(ramUsed) / float64(m.info.RAMTotalMB)
		_, _ = fmt.Fprintf(&sb, "RAM    %s  %s / %s  (%.0f%%)\n\n",
			bar(pct, barWidth),
			sysinfo.FormatMB(ramUsed),
			sysinfo.FormatMB(m.info.RAMTotalMB),
			pct*100)
	}

	if m.status.Model != "" {
		runningStr := "● stopped"
		if m.status.Running {
			runningStr = "● running"
		}
		_, _ = fmt.Fprintf(&sb, "Model: %s (%s)  %s\n",
			m.status.Model, m.status.Provider, runningStr)
	}

	return sb.String()
}

// bar renders a filled/empty ASCII progress bar of the given width for [0,1] pct.
func bar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
