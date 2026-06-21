package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/sysinfo"
)

func TestAdviseCmdRegistered(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "advise" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestAdviseNoDetect(t *testing.T) {
	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("no-detect", true, "")
	err := runAdvise(cmd, []string{"meta-llama/Llama-3.1-70B-Instruct"})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Llama-3.1-70B-Instruct")
	assert.Contains(t, out, "70B params")
	assert.Contains(t, out, "fp16")
	assert.Contains(t, out, "unknown")
}

func TestAdviseWithVRAM(t *testing.T) {
	old := adviseDetectFunc
	adviseDetectFunc = func() (*sysinfo.SystemInfo, error) {
		return &sysinfo.SystemInfo{
			GPUs: []sysinfo.GPUInfo{
				{VRAMTotalMB: 480 * 1024},
			},
		}, nil
	}
	defer func() { adviseDetectFunc = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("no-detect", false, "")
	err := runAdvise(cmd, []string{"meta-llama/Llama-3.1-8B-Instruct"})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "480 GiB")
	assert.Contains(t, out, "✓")
	assert.Contains(t, out, "recommendation")
}

func TestAdviseVRAMDetectError(t *testing.T) {
	old := adviseDetectFunc
	adviseDetectFunc = func() (*sysinfo.SystemInfo, error) {
		return nil, fmt.Errorf("no GPU")
	}
	defer func() { adviseDetectFunc = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("no-detect", false, "")
	err := runAdvise(cmd, []string{"meta-llama/Llama-3.1-8B-Instruct"})
	require.NoError(t, err)
	// When detect fails, shows unknown VRAM but still prints quant table.
	out := buf.String()
	assert.Contains(t, out, "unknown")
	assert.Contains(t, out, "fp16")
}

func TestAdviseNoFit(t *testing.T) {
	old := adviseDetectFunc
	adviseDetectFunc = func() (*sysinfo.SystemInfo, error) {
		// 1 GiB — won't fit any meaningful model.
		return &sysinfo.SystemInfo{
			GPUs: []sysinfo.GPUInfo{{VRAMTotalMB: 1024}},
		}, nil
	}
	defer func() { adviseDetectFunc = old }()

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("no-detect", false, "")
	err := runAdvise(cmd, []string{"meta-llama/Llama-3.1-70B-Instruct"})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "no quantization fits")
}

func TestAdviseUnknownParams(t *testing.T) {
	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("no-detect", true, "")
	// Model ID with no size hint.
	err := runAdvise(cmd, []string{"some-org/mystery-model"})
	require.NoError(t, err)
	// Should still print the table (with "unknown" VRAM estimates).
	assert.Contains(t, buf.String(), "fp16")
}
