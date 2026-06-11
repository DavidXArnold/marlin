package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const hfAPIBase = "https://huggingface.co/api"

type HuggingFace struct {
	token     string
	client    *http.Client
	base      string // overridable for tests
	log       io.Writer
	verbosity int
}

func NewHuggingFace(token string) *HuggingFace {
	return &HuggingFace{
		token:  token,
		client: &http.Client{},
		base:   hfAPIBase,
	}
}

// SetVerbose enables debug logging at the given level (1=requests, 2=headers, 3=bodies).
func (h *HuggingFace) SetVerbose(w io.Writer, level int) {
	h.log = w
	h.verbosity = level
}

func (h *HuggingFace) logf(level int, format string, args ...any) {
	if h.log != nil && h.verbosity >= level {
		_, _ = fmt.Fprintf(h.log, "[hf] "+format, args...)
	}
}

func (h *HuggingFace) Name() string { return "huggingface" }

func (h *HuggingFace) Search(ctx context.Context, query string) ([]ModelInfo, error) {
	endpoint := fmt.Sprintf("%s/models?search=%s&limit=50&full=true", h.base, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}

	h.logf(1, "GET %s\n", endpoint)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("huggingface search: reading response: %w", err)
	}

	h.logf(1, "status: %d\n", resp.StatusCode)
	h.logf(3, "response body: %s\n", body)

	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		return nil, fmt.Errorf("huggingface search: unexpected status %d: %s", resp.StatusCode, snippet)
	}

	var raw []hfModel
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("huggingface search: decoding response: %w", err)
	}

	results := make([]ModelInfo, 0, len(raw))
	for _, r := range raw {
		results = append(results, r.toModelInfo())
	}
	return results, nil
}

func (h *HuggingFace) Fetch(ctx context.Context, id string) (*ModelInfo, error) {
	endpoint := fmt.Sprintf("%s/models/%s", h.base, url.PathEscape(id))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface fetch %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("model %s not found on HuggingFace", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface fetch %s: unexpected status %d", id, resp.StatusCode)
	}

	var raw hfModel
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("huggingface fetch %s: decoding response: %w", id, err)
	}

	info := raw.toModelInfo()
	return &info, nil
}

type hfModel struct {
	ID           string   `json:"id"`
	Private      bool     `json:"private"`
	Description  string   `json:"description"`
	LastModified string   `json:"lastModified"` // "2024-10-10T07:41:18.000Z"
	CreatedAt    string   `json:"createdAt"`
	Tags         []string `json:"tags"`
	// SafeTensors holds per-file parameter counts.
	SafeTensors *hfSafeTensors `json:"safetensors"`
}

type hfSafeTensors struct {
	Total int64 `json:"total"`
}

// paramRegexp matches tokens like "7B", "70b", "8.0B", "0.5b".
var paramRegexp = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)b$`)

// extractParamsFromID splits a model ID on /, - and _ and returns the first
// token that looks like a parameter count (e.g. "8B" → 8.0). Returns 0 if
// none is found.
func extractParamsFromID(id string) float64 {
	isDelim := func(r rune) bool { return r == '/' || r == '-' || r == '_' }
	for _, tok := range strings.FieldsFunc(id, isDelim) {
		if matches := paramRegexp.FindStringSubmatch(tok); len(matches) == 2 {
			if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
				return v
			}
		}
	}
	return 0
}

var hfTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05Z",
}

func parseHFTime(s string) time.Time {
	for _, layout := range hfTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (m hfModel) toModelInfo() ModelInfo {
	ts := parseHFTime(m.LastModified)
	if ts.IsZero() {
		ts = parseHFTime(m.CreatedAt)
	}
	info := ModelInfo{
		ID:          m.ID,
		Registry:    "huggingface",
		Private:     m.Private,
		Description: m.Description,
		LastUpdated: ts,
	}

	// Parameter count from safetensors metadata (most accurate).
	if m.SafeTensors != nil && m.SafeTensors.Total > 0 {
		info.ParamsBillion = float64(m.SafeTensors.Total) / 1e9
	}

	// Fall back to tag heuristic (e.g. "7b", "70B", "8.0b").
	if info.ParamsBillion == 0 {
		for _, tag := range m.Tags {
			if matches := paramRegexp.FindStringSubmatch(tag); len(matches) == 2 {
				if v, err := strconv.ParseFloat(matches[1], 64); err == nil {
					info.ParamsBillion = v
					break
				}
			}
		}
	}

	// Last resort: extract size token from the model ID (e.g. "Llama-3.1-8B-Instruct" → 8).
	// This handles models whose tags don't include a standalone size token.
	if info.ParamsBillion == 0 {
		info.ParamsBillion = extractParamsFromID(m.ID)
	}

	// Quantization from tags (e.g. "awq", "gptq", "gguf").
	for _, tag := range m.Tags {
		lower := strings.ToLower(tag)
		switch lower {
		case "awq", "gptq", "gguf", "int8", "int4", "fp8":
			info.Quantization = lower
		}
	}

	return info
}
