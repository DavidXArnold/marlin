//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testBinary is the path to the compiled marlin binary, built once in TestMain.
var testBinary string

func TestMain(m *testing.M) {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find project root: %v\n", err)
		os.Exit(1)
	}

	bin, err := os.CreateTemp("", "marlin-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating temp file for binary: %v\n", err)
		os.Exit(1)
	}
	bin.Close()
	testBinary = bin.Name()

	cmd := exec.Command("go", "build", "-o", testBinary, "./cmd/marlin")
	cmd.Dir = root
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "building marlin binary: %v\n%s\n", buildErr, out)
		os.Remove(testBinary)
		os.Exit(1)
	}

	code := m.Run()
	os.Remove(testBinary)
	os.Exit(code)
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir != "/" {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("go.mod not found — cannot determine project root")
}
