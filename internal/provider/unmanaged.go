package provider

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// inferenceImageFragments are substrings matched against container image names
// to identify likely inference server containers not managed by marlin.
var inferenceImageFragments = []string{
	"vllm",
	"nim",
	"tgi",                       // text-generation-inference short tag
	"text-generation-inference", // huggingface TGI full image name
	"ollama",
	"llama.cpp",
	"llamacpp",
	"localai",
	"lmstudio",
	"deepspeed",
	"triton", // NVIDIA Triton Inference Server
}

// UnmanagedContainer describes a running container that looks like an
// inference server but is not managed by marlin.
type UnmanagedContainer struct {
	ID    string
	Image string
	Names []string
}

// DetectUnmanaged lists running containers that match known inference server
// image patterns but do not carry the marlin.managed label.
func DetectUnmanaged(ctx context.Context, docker dockerClient) ([]UnmanagedContainer, error) {
	// Only look at running containers.
	containers, err := docker.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("status", "running")),
	})
	if err != nil {
		return nil, err
	}

	var result []UnmanagedContainer
	for _, c := range containers {
		if c.Labels[labelManaged] == "true" {
			continue
		}
		if looksLikeInference(c.Image) {
			result = append(result, UnmanagedContainer{
				ID:    c.ID,
				Image: c.Image,
				Names: c.Names,
			})
		}
	}
	return result, nil
}

func looksLikeInference(image string) bool {
	lower := strings.ToLower(image)
	for _, frag := range inferenceImageFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}
