package registry

import (
	"context"
	"strings"
	"time"
)

// ModelInfo holds metadata returned from a registry search or lookup.
type ModelInfo struct {
	ID            string
	Registry      string
	Architecture  string
	Quantization  string
	MaxContextLen int
	SizeGB        float64
	Private       bool
	Description   string
	LastUpdated   time.Time
	ParamsBillion float64 // estimated parameter count in billions (0 = unknown)
}

// EstimatedVRAMMB returns a rough VRAM requirement in MB based on parameter count
// and quantization. Returns 0 when ParamsBillion is unknown.
//
// Rule of thumb: fp16 ≈ 2 bytes/param, int8 ≈ 1 byte/param, 4-bit ≈ 0.5 bytes/param.
func (m ModelInfo) EstimatedVRAMMB() uint64 {
	if m.ParamsBillion == 0 {
		return 0
	}
	bytesPerParam := 2.0 // default: fp16
	switch m.Quantization {
	case "awq", "awq_marlin", "gptq", "int4":
		bytesPerParam = 0.5
	case "int8", "w8a8":
		bytesPerParam = 1.0
	}
	params := m.ParamsBillion * 1e9
	// Add ~20% overhead for KV cache and activations.
	totalBytes := params * bytesPerParam * 1.2
	return uint64(totalBytes / (1024 * 1024))
}

// DisplayName returns a short human-friendly name. For NGC models it strips the
// "nvcr.io/nim/" prefix and ":tag" suffix; for others it returns ID unchanged.
func (m ModelInfo) DisplayName() string {
	if m.Registry != "ngc" {
		return m.ID
	}
	name := strings.TrimPrefix(m.ID, "nvcr.io/nim/")
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		name = name[:idx]
	}
	return name
}

// Registry is the interface all model registries implement.
type Registry interface {
	Name() string
	Search(ctx context.Context, query string) ([]ModelInfo, error)
	Fetch(ctx context.Context, id string) (*ModelInfo, error)
}
