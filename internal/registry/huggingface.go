package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const hfAPIBase = "https://huggingface.co/api"

type HuggingFace struct {
	token  string
	client *http.Client
	base   string // overridable for tests
}

func NewHuggingFace(token string) *HuggingFace {
	return &HuggingFace{
		token:  token,
		client: &http.Client{},
		base:   hfAPIBase,
	}
}

func (h *HuggingFace) Name() string { return "huggingface" }

func (h *HuggingFace) Search(ctx context.Context, query string) ([]ModelInfo, error) {
	endpoint := fmt.Sprintf("%s/models?search=%s&limit=20", h.base, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface search: unexpected status %d", resp.StatusCode)
	}

	var raw []hfModel
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
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
	defer resp.Body.Close()

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
	ID           string    `json:"id"`
	Private      bool      `json:"private"`
	Description  string    `json:"description"`
	LastModified time.Time `json:"lastModified"`
	Tags         []string  `json:"tags"`
	// SafeTensors holds per-file parameter counts.
	SafeTensors *hfSafeTensors `json:"safetensors"`
}

type hfSafeTensors struct {
	Total int64 `json:"total"`
}

// paramRegexp matches tags like "7B", "70b", "8.0B", "0.5b".
var paramRegexp = regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)b$`)

func (m hfModel) toModelInfo() ModelInfo {
	info := ModelInfo{
		ID:          m.ID,
		Registry:    "huggingface",
		Private:     m.Private,
		Description: m.Description,
		LastUpdated: m.LastModified,
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
