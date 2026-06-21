package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DavidXArnold/marlin/internal/doctor"
)

func injectDoctorRunCmd(t *testing.T, fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	t.Helper()
	old := doctor.DoctorRunCmd
	doctor.DoctorRunCmd = fn
	t.Cleanup(func() { doctor.DoctorRunCmd = old })
}

func TestDoctorCmd(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	injectDoctorRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, &stubCmdNotFound{name: name}
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("fix", false, "")
	cmd.Flags().Bool("yes", false, "")

	_ = runDoctor(cmd, nil)

	out := buf.String()
	assert.True(t,
		bytes.Contains([]byte(out), []byte("[PASS]")) ||
			bytes.Contains([]byte(out), []byte("[WARN]")) ||
			bytes.Contains([]byte(out), []byte("[FAIL]")),
		"expected output to contain check markers, got: %s", out)
	assert.Contains(t, out, "PASS,")
}

func TestDoctorExitCode(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	injectDoctorRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, &stubCmdNotFound{name: name}
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("fix", false, "")
	cmd.Flags().Bool("yes", false, "")

	err := runDoctor(cmd, nil)
	out := buf.String()

	if bytes.Contains([]byte(out), []byte("[FAIL]")) {
		assert.Error(t, err, "FAIL results should produce a non-zero exit")
	} else {
		assert.NoError(t, err)
	}
}

func TestDoctorAllPass(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	cfg, err := globalConfig()
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Paths.ModelsDir)

	injectDoctorRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "docker":
			return []byte(`{"Client":{"Version":"27.3.1"}}`), nil
		case "nvidia-smi":
			return []byte("535.0\n"), nil
		default:
			return []byte("ok\n"), nil
		}
	})

	var buf bytes.Buffer
	cmd := cmdWithContext(&buf)
	cmd.Flags().Bool("fix", false, "")
	cmd.Flags().Bool("yes", false, "")
	_ = runDoctor(cmd, nil)
	assert.Contains(t, buf.String(), "PASS,")
}

type stubCmdNotFound struct{ name string }

func (e *stubCmdNotFound) Error() string { return e.name + ": executable file not found in $PATH" }
