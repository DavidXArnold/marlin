package cmd

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopCmdRegistered(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "top" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestRunTopProgError(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	old := topProgFunc
	topProgFunc = func(_ tea.Model) error { return fmt.Errorf("no tty") }
	defer func() { topProgFunc = old }()

	cmd := cmdWithContext(nil)
	err := runTop(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tty")
}

func TestRunTopSuccess(t *testing.T) {
	cleanup := tempEnv(t)
	defer cleanup()

	var gotModel tea.Model
	old := topProgFunc
	topProgFunc = func(m tea.Model) error {
		gotModel = m
		return nil
	}
	defer func() { topProgFunc = old }()

	cmd := cmdWithContext(nil)
	err := runTop(cmd, nil)
	require.NoError(t, err)
	assert.NotNil(t, gotModel)
}
