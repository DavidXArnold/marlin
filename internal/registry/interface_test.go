package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
