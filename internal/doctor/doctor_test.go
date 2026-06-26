package doctor

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	marlinConfig "github.com/DavidXArnold/marlin/internal/config"
)

func injectRunCmd(t *testing.T, fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	t.Helper()
	old := DoctorRunCmd
	DoctorRunCmd = fn
	t.Cleanup(func() { DoctorRunCmd = old })
}

func TestSecretsFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("HF_TOKEN=test\n"), 0o644))

	cfg := marlinConfig.Defaults()
	cfg.Paths.SecretsEnv = path

	checks := secretsChecks(cfg)
	var modeCheck Check
	for _, c := range checks {
		if c.ID() == "secrets.file_mode" {
			modeCheck = c
			break
		}
	}
	require.NotNil(t, modeCheck)

	result := modeCheck.Run(context.Background())
	assert.Equal(t, LevelWarn, result.Level)
	assert.True(t, result.CanFix)

	require.NoError(t, modeCheck.Fix(context.Background()))

	result = modeCheck.Run(context.Background())
	assert.Equal(t, LevelPass, result.Level)
}

func TestPathsModelsDir(t *testing.T) {
	cfg := marlinConfig.Defaults()

	cfg.Paths.ModelsDir = filepath.Join(t.TempDir(), "nonexistent")
	checks := pathsChecks(cfg)
	var modelsCheck Check
	for _, c := range checks {
		if c.ID() == "paths.models_dir" {
			modelsCheck = c
			break
		}
	}
	require.NotNil(t, modelsCheck)

	result := modelsCheck.Run(context.Background())
	assert.Equal(t, LevelFail, result.Level)
	assert.True(t, result.CanFix)

	cfg.Paths.ModelsDir = t.TempDir()
	checks = pathsChecks(cfg)
	for _, c := range checks {
		if c.ID() == "paths.models_dir" {
			modelsCheck = c
			break
		}
	}
	result = modelsCheck.Run(context.Background())
	assert.Equal(t, LevelPass, result.Level)
}

func TestDiskCheck(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Paths.ModelsDir = t.TempDir()

	old := statfsFunc
	statfsFunc = func(path string) (syscall.Statfs_t, error) {
		return syscall.Statfs_t{Bsize: 4096, Bavail: 1000}, nil
	}
	t.Cleanup(func() { statfsFunc = old })

	checks := diskChecks(cfg)
	var diskCheck Check
	for _, c := range checks {
		if c.ID() == "disk.models_dir" {
			diskCheck = c
			break
		}
	}
	require.NotNil(t, diskCheck)

	result := diskCheck.Run(context.Background())
	assert.Equal(t, LevelWarn, result.Level)

	statfsFunc = func(path string) (syscall.Statfs_t, error) {
		const gib = 1 << 30
		return syscall.Statfs_t{Bsize: 4096, Bavail: uint64(100*gib) / 4096}, nil
	}

	result = diskCheck.Run(context.Background())
	assert.Equal(t, LevelPass, result.Level)
}

func TestConfigLoaded(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[behavior]\nswitch_prompt = false\n"), 0o644))

	checks := configChecks(cfgPath)
	require.Len(t, checks, 1)
	result := checks[0].Run(context.Background())
	assert.Equal(t, LevelPass, result.Level)

	require.NoError(t, os.WriteFile(cfgPath, []byte("[[[[invalid toml"), 0o644))
	result = checks[0].Run(context.Background())
	assert.Equal(t, LevelFail, result.Level)
}

func TestRuntimeCheckPass(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`{"Client":{"Version":"27.3.1"}}`), nil
	})

	checks := runtimeChecks(marlinConfig.Defaults())
	var dockerCheck Check
	for _, c := range checks {
		if c.ID() == "runtime.docker" {
			dockerCheck = c
			break
		}
	}
	require.NotNil(t, dockerCheck)
	result := dockerCheck.Run(context.Background())
	assert.Equal(t, LevelPass, result.Level)
	assert.Contains(t, result.Detail, "27.3.1")
}

func TestRuntimeCheckFail(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, &notFoundError{name: name}
	})

	checks := runtimeChecks(marlinConfig.Defaults())
	var dockerCheck Check
	for _, c := range checks {
		if c.ID() == "runtime.docker" {
			dockerCheck = c
			break
		}
	}
	require.NotNil(t, dockerCheck)
	result := dockerCheck.Run(context.Background())
	assert.Equal(t, LevelWarn, result.Level)
}

type notFoundError struct{ name string }

func (e *notFoundError) Error() string { return e.name + ": command not found" }

func TestGPUDriverMissing(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, &notFoundError{name: name}
	})
	result := checkGPUDriver(context.Background())
	assert.Equal(t, LevelWarn, result.Level)
}

func TestGPUDriverPresent(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("535.129.03\n"), nil
	})
	result := checkGPUDriver(context.Background())
	assert.Equal(t, LevelPass, result.Level)
	assert.Contains(t, result.Detail, "535.129.03")
}

func TestSecretsTokensNotSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	cfg := marlinConfig.Defaults()
	cfg.Paths.SecretsEnv = path

	checks := secretsChecks(cfg)
	hfResult := findCheckResult(t, checks, "secrets.hf_token_set")
	assert.Equal(t, LevelWarn, hfResult.Level)

	ngcResult := findCheckResult(t, checks, "secrets.ngc_key_set")
	assert.Equal(t, LevelWarn, ngcResult.Level)
}

func TestSecretsTokensSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("HF_TOKEN=hf_abc\nNGC_API_KEY=nvapi_xyz\n"), 0o600))

	cfg := marlinConfig.Defaults()
	cfg.Paths.SecretsEnv = path

	checks := secretsChecks(cfg)
	hfResult := findCheckResult(t, checks, "secrets.hf_token_set")
	assert.Equal(t, LevelPass, hfResult.Level)

	ngcResult := findCheckResult(t, checks, "secrets.ngc_key_set")
	assert.Equal(t, LevelPass, ngcResult.Level)
}

func findCheckResult(t *testing.T, checks []Check, id string) Result {
	t.Helper()
	for _, c := range checks {
		if c.ID() == id {
			return c.Run(context.Background())
		}
	}
	t.Fatalf("check %q not found", id)
	return Result{}
}

func TestAllChecks(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Paths.ModelsDir = t.TempDir()

	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, &notFoundError{name: name}
	})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("[behavior]\nswitch_prompt = false\n"), 0o644))

	checks := AllChecks(cfg, cfgPath)
	assert.NotEmpty(t, checks)
	for _, c := range checks {
		_ = c.Run(context.Background()) // must not panic
	}
}

func TestCheckComputeCap(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("9.0\n"), nil
	})
	result := checkComputeCap(context.Background())
	assert.Equal(t, LevelPass, result.Level)
	assert.Contains(t, result.Detail, "9.0")

	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, &notFoundError{name: name}
	})
	result = checkComputeCap(context.Background())
	assert.Equal(t, LevelWarn, result.Level)
}

func TestCheckUMA(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("NVIDIA GB200, 0\n"), nil
	})
	result := checkUMA(context.Background())
	assert.Equal(t, LevelPass, result.Level)
	assert.Contains(t, result.Detail, "GB200")

	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("NVIDIA A100-SXM4-80GB, 81920\n"), nil
	})
	result = checkUMA(context.Background())
	assert.Equal(t, LevelPass, result.Level)
	assert.Contains(t, result.Detail, "not a UMA GPU")
}

func TestExtractVersionPodman(t *testing.T) {
	out := []byte(`{"Version":"4.9.4"}`)
	v := extractVersion("podman", out)
	assert.Equal(t, "podman 4.9.4", v)
}

func TestExtractVersionFallback(t *testing.T) {
	out := []byte("nerdctl version 1.7.3\n")
	v := extractVersion("nerdctl", out)
	assert.Equal(t, "nerdctl version 1.7.3", v)
}

func TestPathsNimCache(t *testing.T) {
	cfg := marlinConfig.Defaults()

	// Nonexistent nim cache → WARN with CanFix.
	cfg.Paths.NIMCache = filepath.Join(t.TempDir(), "nim-cache-nonexistent")
	checks := pathsChecks(cfg)
	result := findCheckResult(t, checks, "paths.nim_cache")
	assert.Equal(t, LevelWarn, result.Level)
	assert.True(t, result.CanFix)

	// Existing but wrong perms.
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755))
	cfg.Paths.NIMCache = dir
	checks = pathsChecks(cfg)
	result = findCheckResult(t, checks, "paths.nim_cache")
	assert.Equal(t, LevelWarn, result.Level)
}

func TestPathsStateDirMissing(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Paths.StateFile = filepath.Join(t.TempDir(), "nonexistent-dir", "state.toml")

	checks := pathsChecks(cfg)
	result := findCheckResult(t, checks, "paths.state_dir")
	assert.Equal(t, LevelWarn, result.Level)
	assert.True(t, result.CanFix)
}

func TestPathsStateDirExists(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Paths.StateFile = filepath.Join(t.TempDir(), "state.toml")

	checks := pathsChecks(cfg)
	result := findCheckResult(t, checks, "paths.state_dir")
	assert.Equal(t, LevelPass, result.Level)
}

func TestDiskNimCacheCheck(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Paths.NIMCache = t.TempDir()

	old := statfsFunc
	statfsFunc = func(path string) (syscall.Statfs_t, error) {
		return syscall.Statfs_t{Bsize: 4096, Bavail: 1000}, nil
	}
	t.Cleanup(func() { statfsFunc = old })

	checks := diskChecks(cfg)
	result := findCheckResult(t, checks, "disk.nim_cache")
	assert.Equal(t, LevelWarn, result.Level)

	statfsFunc = func(path string) (syscall.Statfs_t, error) {
		const gib = 1 << 30
		return syscall.Statfs_t{Bsize: 4096, Bavail: uint64(200*gib) / 4096}, nil
	}
	result = findCheckResult(t, checks, "disk.nim_cache")
	assert.Equal(t, LevelPass, result.Level)
}

func TestRuntimeCheckNerdctl(t *testing.T) {
	injectRunCmd(t, func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("nerdctl version 1.7.3\n"), nil
	})
	checks := runtimeChecks(marlinConfig.Defaults())
	result := findCheckResult(t, checks, "runtime.nerdctl")
	assert.Equal(t, LevelPass, result.Level)
}

func TestVLLMContainerCheckNoRuntime(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Service.ContainerRuntime = "no-such-runtime-xyz"
	check := &funcCheck{id: "runtime.vllm_container", run: checkVLLMContainer(cfg)}
	result := check.Run(context.Background())
	assert.Equal(t, LevelFail, result.Level)
	assert.Contains(t, result.Detail, "no-such-runtime-xyz")
}

func TestVLLMBinaryCheckConfiguredMissing(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Service.VLLMMode = "binary"
	cfg.Service.VLLMBin = "/nonexistent/path/to/vllm"
	check := &funcCheck{id: "runtime.vllm", run: checkVLLMBin(cfg)}
	result := check.Run(context.Background())
	assert.Equal(t, LevelWarn, result.Level)
	assert.Contains(t, result.Detail, "/nonexistent/path/to/vllm")
}

func TestVLLMBinaryCheckConfiguredPresent(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "vllm")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh"), 0o755))

	cfg := marlinConfig.Defaults()
	cfg.Service.VLLMMode = "binary"
	cfg.Service.VLLMBin = bin
	check := &funcCheck{id: "runtime.vllm", run: checkVLLMBin(cfg)}
	result := check.Run(context.Background())
	assert.Equal(t, LevelPass, result.Level)
	assert.Contains(t, result.Detail, bin)
}

func TestVLLMBinaryCheckNotInPath(t *testing.T) {
	cfg := marlinConfig.Defaults()
	cfg.Service.VLLMMode = "binary"
	// VLLMBin empty + vllm not in PATH in CI → WARN
	check := &funcCheck{id: "runtime.vllm", run: checkVLLMBin(cfg)}
	result := check.Run(context.Background())
	// May pass if vllm is installed; just verify it's not FAIL.
	assert.NotEqual(t, LevelFail, result.Level)
}
