//go:build integration

package integration_test

import (
	"context"
	"os"
	"testing"

	"github.com/DavidXArnold/marlin/internal/vllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vllmBase returns the base URL to test against: real server if MARLIN_TEST_HOST
// is set, otherwise an embedded mock that mimics the vLLM OpenAI-compatible API.
func vllmBase(t *testing.T) string {
	t.Helper()
	if host := os.Getenv("MARLIN_TEST_HOST"); host != "" {
		return "http://" + host + ":8000"
	}
	return mockVLLMServer(t).URL
}

func TestIntegrationVLLMHealth(t *testing.T) {
	c := vllm.NewClientFromBase(vllmBase(t), "")
	status, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Ready)
}

func TestIntegrationVLLMModels(t *testing.T) {
	c := vllm.NewClientFromBase(vllmBase(t), "")
	models, err := c.Models(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models, "server should return at least one model")
	for _, m := range models {
		t.Logf("  model: %s", m.ID)
	}
}

func TestIntegrationVLLMHealthUnreachable(t *testing.T) {
	c := vllm.NewClientFromBase("http://127.0.0.1:19999", "")
	status, err := c.Health(context.Background())
	require.NoError(t, err, "unreachable host should return not-ready, not an error")
	assert.False(t, status.Ready)
}
