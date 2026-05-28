package provider

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeInference(t *testing.T) {
	cases := []struct {
		image string
		want  bool
	}{
		{"vllm/vllm-openai:latest", true},
		{"nvcr.io/nim/meta/llama-3.1-8b-instruct:latest", true},
		{"ghcr.io/huggingface/text-generation-inference:latest", true},
		{"ollama/ollama:latest", true},
		{"nvidia/tritonserver:latest", true},
		{"nginx:latest", false},
		{"postgres:16", false},
		{"redis:7", false},
		{"ubuntu:22.04", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, looksLikeInference(c.image), c.image)
	}
}

func TestDetectUnmanagedNone(t *testing.T) {
	d := &stubDocker{}
	containers, err := DetectUnmanaged(context.Background(), d)
	require.NoError(t, err)
	assert.Empty(t, containers)
}

func TestDetectUnmanagedSkipsMarlinManaged(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{
				ID:     "abc123",
				Image:  "vllm/vllm-openai:latest",
				Labels: map[string]string{labelManaged: "true"},
			},
		},
	}
	containers, err := DetectUnmanaged(context.Background(), d)
	require.NoError(t, err)
	assert.Empty(t, containers, "marlin-managed container should not be flagged")
}

func TestDetectUnmanagedFindsUnmanaged(t *testing.T) {
	d := &stubDocker{
		listResult: []container.Summary{
			{
				ID:    "abc123",
				Image: "vllm/vllm-openai:latest",
				Names: []string{"/my-vllm"},
			},
			{
				ID:    "def456",
				Image: "nginx:latest", // not inference
				Names: []string{"/nginx"},
			},
		},
	}
	containers, err := DetectUnmanaged(context.Background(), d)
	require.NoError(t, err)
	require.Len(t, containers, 1)
	assert.Equal(t, "abc123", containers[0].ID)
	assert.Equal(t, "vllm/vllm-openai:latest", containers[0].Image)
}

func TestDetectUnmanagedListError(t *testing.T) {
	d := &stubDocker{listErr: assert.AnError}
	_, err := DetectUnmanaged(context.Background(), d)
	assert.Error(t, err)
}
