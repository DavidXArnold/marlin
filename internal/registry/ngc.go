package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const ngcAPIBase = "https://api.ngc.nvidia.com/v2"

type NGC struct {
	apiKey string
	client *http.Client
	base   string // overridable for tests
}

func NewNGC(apiKey string) *NGC {
	return &NGC{
		apiKey: apiKey,
		client: &http.Client{},
		base:   ngcAPIBase,
	}
}

func (n *NGC) Name() string { return "ngc" }

func (n *NGC) Search(ctx context.Context, query string) ([]ModelInfo, error) {
	endpoint := fmt.Sprintf("%s/search/resources/MODEL?q=%s&pageSize=20", n.base, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if n.apiKey != "" {
		req.Header.Set("Authorization", "ApiKey "+n.apiKey)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ngc search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ngc search: unexpected status %d", resp.StatusCode)
	}

	var raw ngcSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ngc search: decoding response: %w", err)
	}

	results := make([]ModelInfo, 0, len(raw.Results))
	for _, r := range raw.Results {
		results = append(results, r.toModelInfo())
	}
	return results, nil
}

func (n *NGC) Fetch(_ context.Context, _ string) (*ModelInfo, error) {
	return nil, fmt.Errorf("ngc fetch not yet implemented")
}

type ngcSearchResponse struct {
	Results []ngcResource `json:"results"`
}

type ngcResource struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"shortDescription"`
	UpdatedDate string `json:"updatedDate"` // RFC3339 or similar
}

func (r ngcResource) toModelInfo() ModelInfo {
	info := ModelInfo{
		ID:          r.Name,
		Registry:    "ngc",
		Description: r.Description,
	}
	if r.UpdatedDate != "" {
		if t, err := time.Parse(time.RFC3339, r.UpdatedDate); err == nil {
			info.LastUpdated = t
		}
	}
	return info
}
