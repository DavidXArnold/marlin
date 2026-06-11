package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayName(t *testing.T) {
	cases := []struct {
		info ModelInfo
		want string
	}{
		{ModelInfo{ID: "meta/llama-3.1-8b-instruct", Registry: "huggingface"}, "meta/llama-3.1-8b-instruct"},
		{ModelInfo{ID: "nvcr.io/nim/nvidia/llama-3.3-70b-instruct:latest", Registry: "ngc"}, "nvidia/llama-3.3-70b-instruct"},
		{ModelInfo{ID: "nvcr.io/nim/meta/llama-3.1-8b-instruct:1.8", Registry: "ngc"}, "meta/llama-3.1-8b-instruct"},
		{ModelInfo{ID: "nvcr.io/nim/nvidia/llama-3.1-nemotron-ultra-253b-v1:latest", Registry: "ngc"}, "nvidia/llama-3.1-nemotron-ultra-253b-v1"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.info.DisplayName(), "id: %q", c.info.ID)
	}
}

func TestModelInfoEstimatedVRAMMB(t *testing.T) {
	cases := []struct {
		name         string
		quantization string
		params       float64
		wantZero     bool
		wantLessThan uint64
	}{
		{name: "unknown", params: 0, wantZero: true},
		{name: "fp16", params: 7, wantLessThan: 20_000},
		{name: "awq", quantization: "awq", params: 7, wantLessThan: 5_000},
		{name: "int8", quantization: "int8", params: 7, wantLessThan: 10_000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ModelInfo{ParamsBillion: c.params, Quantization: c.quantization}.EstimatedVRAMMB()
			if c.wantZero {
				assert.Zero(t, got)
				return
			}
			assert.Positive(t, got)
			assert.Less(t, got, c.wantLessThan)
		})
	}
}
