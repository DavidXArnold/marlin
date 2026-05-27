package registry

import "context"

// ModelInfo holds metadata returned from a registry search or lookup.
type ModelInfo struct {
	ID           string
	Registry     string
	Architecture string
	Quantization string
	MaxContextLen int
	SizeGB       float64
	Private      bool
	Description  string
}

// Registry is the interface all model registries implement.
type Registry interface {
	Name() string
	Search(ctx context.Context, query string) ([]ModelInfo, error)
	Fetch(ctx context.Context, id string) (*ModelInfo, error)
}
