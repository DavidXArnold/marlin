package provider

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/DavidXArnold/marlin/internal/config"
	"github.com/DavidXArnold/marlin/internal/privilege"
	"github.com/DavidXArnold/marlin/pkg/render"
)

// writeContainerFiles renders the side files a profile's [container] table
// needs — the docker --env-file and the pre_serve script — next to the slug's
// env file, and returns their paths ("" for each one the profile doesn't use).
// c must already have its placeholders expanded.
func writeContainerFiles(w io.Writer, modelsDir, slug string, c config.ContainerConfig) (envFile, hookFile string, err error) {
	write := func(name, content string) (string, error) {
		path := filepath.Join(modelsDir, slug+name)
		ok, err := privilege.PromptAndWriteFile(w, filepath.Dir(path), path, []byte(content))
		if err != nil {
			return "", fmt.Errorf("writing %s: %w", path, err)
		}
		if !ok {
			return "", fmt.Errorf("writing %s: cancelled", path)
		}
		return path, nil
	}
	if len(c.Env) > 0 {
		if envFile, err = write(".container.env", render.ContainerEnvFile(c.Env)); err != nil {
			return "", "", err
		}
	}
	if len(c.PreServe) > 0 {
		if hookFile, err = write(".pre-serve.sh", render.PreServeScript(c.PreServe)); err != nil {
			return "", "", err
		}
	}
	return envFile, hookFile, nil
}
