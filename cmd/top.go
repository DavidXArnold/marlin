package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/DavidXArnold/marlin/internal/sysinfo"
	"github.com/DavidXArnold/marlin/internal/state"
	"github.com/DavidXArnold/marlin/internal/top"
)

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Live GPU power, temperature, VRAM, and RAM dashboard",
	Args:  cobra.NoArgs,
	RunE:  runTop,
}

func init() {
	rootCmd.AddCommand(topCmd)
}

// topProgFunc is injectable for tests — replaces tea.NewProgram(...).Run().
var topProgFunc = func(m tea.Model) error {
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func runTop(_ *cobra.Command, _ []string) error {
	cfg, err := globalConfig()
	if err != nil {
		return err
	}

	cur, _ := state.Load(cfg.Paths.StateFile)

	// Determine model status for the dashboard footer.
	var statusLine top.StatusLine
	if cur.ActiveModel != "" {
		statusLine = top.StatusLine{
			Model:    cur.ActiveModel,
			Provider: string(cur.ActiveProvider),
			Running:  cur.StoppedAt == nil,
		}
	}

	sample := func() (*sysinfo.SystemInfo, error) {
		info, err := sysinfo.Detect()
		if err != nil {
			return nil, err
		}
		sysinfo.SampleTelemetry(info)
		return info, nil
	}

	m := top.New(sample, statusLine)
	if err := topProgFunc(m); err != nil {
		return fmt.Errorf("top: %w", err)
	}
	return nil
}
