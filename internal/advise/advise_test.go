package advise

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseParamsBillion(t *testing.T) {
	cases := []struct {
		id   string
		want float64
	}{
		{"meta-llama/Llama-3.1-70B-Instruct", 70},
		{"Qwen/Qwen2.5-72B-Instruct-AWQ", 72},
		{"nvidia/Llama-3.1-8b-Instruct-NVFP4", 8},
		{"phi-3.5-mini-instruct", 0}, // no size in name
		{"model-3.5b-chat", 3.5},
		{"my-model", 0},
	}
	for _, c := range cases {
		got := ParseParamsBillion(c.id)
		assert.InDelta(t, c.want, got, 0.01, c.id)
	}
}

func TestEstimateVRAMMB(t *testing.T) {
	// 70B fp16: 70e9 * 2 * 1.2 / 1MiB ≈ 160,217 MiB ≈ 156 GiB
	est := EstimateVRAMMB(70, 2.0)
	assert.Greater(t, est, uint64(130*1024))

	// 0 params → 0
	assert.Equal(t, uint64(0), EstimateVRAMMB(0, 2.0))
}

func TestAdviseLength(t *testing.T) {
	recs := Advise("meta-llama/Llama-3.1-70B-Instruct", 480*1024)
	assert.Equal(t, len(KnownQuants), len(recs))
}

func TestAdviseFits(t *testing.T) {
	// 480 GiB should fit fp16 of a 70B model (~156 GiB) easily.
	recs := Advise("meta-llama/Llama-3.1-70B-Instruct", 480*1024)
	for _, r := range recs {
		assert.True(t, r.Fits, "expected %s to fit in 480 GiB", r.Quant.Name)
	}
}

func TestAdviseNoFitSmallVRAM(t *testing.T) {
	// 4 GiB cannot fit fp16 of a 70B model.
	recs := Advise("meta-llama/Llama-3.1-70B-Instruct", 4*1024)
	fp16 := recs[0]
	assert.Equal(t, "fp16", fp16.Quant.Name)
	assert.False(t, fp16.Fits)
}

func TestAdviseZeroVRAM(t *testing.T) {
	// availableVRAMMB=0 means unknown → all Fits false.
	recs := Advise("meta-llama/Llama-3.1-8B-Instruct", 0)
	for _, r := range recs {
		assert.False(t, r.Fits)
	}
}

func TestBestFitFound(t *testing.T) {
	recs := Advise("meta-llama/Llama-3.1-8B-Instruct", 480*1024)
	best := BestFit(recs)
	assert.NotNil(t, best)
	assert.Equal(t, "fp16", best.Quant.Name) // fp16 is first and fits
}

func TestBestFitNone(t *testing.T) {
	recs := Advise("meta-llama/Llama-3.1-70B-Instruct", 4*1024)
	best := BestFit(recs)
	assert.Nil(t, best)
}

func TestFormatVRAMMB(t *testing.T) {
	assert.Equal(t, "unknown", FormatVRAMMB(0))
	assert.Equal(t, "512 MiB", FormatVRAMMB(512))
	assert.Equal(t, "2 GiB", FormatVRAMMB(2048))
	assert.Equal(t, "480 GiB", FormatVRAMMB(480*1024))
}

func TestSearchQuery(t *testing.T) {
	recs := Advise("meta-llama/Llama-3.1-70B-Instruct", 0)
	for _, r := range recs {
		assert.Contains(t, r.SearchQuery, "Llama-3.1-70B-Instruct")
	}
}
