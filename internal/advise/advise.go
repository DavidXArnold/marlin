package advise

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Quant describes one quantization scheme and its memory factor.
type Quant struct {
	Name          string  // identifier used in marlin model configs
	BytesPerParam float64 // bytes per parameter (fp16=2, fp8=1, int4≈0.5)
	Label         string  // human-readable display name
}

// KnownQuants lists supported quantizations in quality order (best first).
var KnownQuants = []Quant{
	{Name: "fp16",       BytesPerParam: 2.0,   Label: "fp16 (full precision)"},
	{Name: "fp8",        BytesPerParam: 1.0,   Label: "fp8 (8-bit float)"},
	{Name: "nvfp4",      BytesPerParam: 0.5,   Label: "nvfp4 (NVIDIA Blackwell)"},
	{Name: "awq_marlin", BytesPerParam: 0.5,   Label: "AWQ (4-bit)"},
	{Name: "gptq",       BytesPerParam: 0.5,   Label: "GPTQ (4-bit)"},
}

// Recommendation holds a single quant's VRAM estimate and fit status.
type Recommendation struct {
	Quant       Quant
	EstVRAMMB   uint64
	Fits        bool   // true when EstVRAMMB <= availableVRAMMB
	SearchQuery string // HF search query to find sibling models
}

// paramRegexp extracts a parameter count like "70B", "8b", "72B", "3.5b" from model IDs.
var paramRegexp = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)[Bb]`)

// ParseParamsBillion attempts to extract the parameter count in billions from
// a model name such as "meta-llama/Llama-3.1-70B-Instruct". Returns 0 if not found.
func ParseParamsBillion(modelID string) float64 {
	// Use the last segment of the path for accuracy.
	parts := strings.Split(modelID, "/")
	name := parts[len(parts)-1]
	m := paramRegexp.FindStringSubmatch(name)
	if m == nil {
		m = paramRegexp.FindStringSubmatch(modelID)
	}
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// EstimateVRAMMB returns the estimated VRAM requirement in MB for paramsBillion
// parameters at the given quant's bytes-per-param ratio, with 20% overhead.
func EstimateVRAMMB(paramsBillion, bytesPerParam float64) uint64 {
	if paramsBillion <= 0 {
		return 0
	}
	totalBytes := paramsBillion * 1e9 * bytesPerParam * 1.2
	return uint64(totalBytes / (1024 * 1024))
}

// Advise returns recommendations for all KnownQuants sorted by VRAM requirement
// (highest first). availableVRAMMB of 0 means "unknown" — all Fits fields are false.
func Advise(modelID string, availableVRAMMB uint64) []Recommendation {
	params := ParseParamsBillion(modelID)
	recs := make([]Recommendation, len(KnownQuants))

	baseName := modelID
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		baseName = modelID[idx+1:]
	}

	for i, q := range KnownQuants {
		est := EstimateVRAMMB(params, q.BytesPerParam)
		fits := availableVRAMMB > 0 && est <= availableVRAMMB
		recs[i] = Recommendation{
			Quant:       q,
			EstVRAMMB:   est,
			Fits:        fits,
			SearchQuery: fmt.Sprintf("%s %s", baseName, q.Name),
		}
	}
	return recs
}

// BestFit returns the first (highest quality) Recommendation that fits, or nil.
func BestFit(recs []Recommendation) *Recommendation {
	for i := range recs {
		if recs[i].Fits {
			return &recs[i]
		}
	}
	return nil
}

// FormatVRAMMB formats megabytes as "N GiB" or "N MiB".
func FormatVRAMMB(mb uint64) string {
	if mb == 0 {
		return "unknown"
	}
	if mb >= 1024 {
		return fmt.Sprintf("%.0f GiB", float64(mb)/1024)
	}
	return fmt.Sprintf("%d MiB", mb)
}
