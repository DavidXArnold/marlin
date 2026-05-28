package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
	endpoint := fmt.Sprintf("%s/search/resources?q=%s&resourceType=CONTAINER&pageSize=20", n.base, url.QueryEscape(query))

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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if n.apiKey != "" {
			return nil, fmt.Errorf("ngc search: authentication failed — run 'marlin configure' to update NGC_API_KEY or generate a new key at https://org.ngc.nvidia.com/setup/personal-keys")
		}
		return nil, fmt.Errorf("ngc search: authentication required — run 'marlin configure' to add an NGC_API_KEY")
	}
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
	LatestTag   string `json:"latestTag"`   // image tag; defaults to "latest"
}

func (r ngcResource) toModelInfo() ModelInfo {
	info := ModelInfo{
		ID:          nimImageRef(r.Name, r.LatestTag),
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

// nimImageRef constructs the full nvcr.io pull reference from an NGC resource
// name. Names that already look like full references are returned unchanged.
//
// e.g. "nvidia/llama-3.1-8b-instruct" + "" → "nvcr.io/nim/nvidia/llama-3.1-8b-instruct:latest"
//
//	"nvidia/llama-3.1-8b-instruct" + "1.8" → "nvcr.io/nim/nvidia/llama-3.1-8b-instruct:1.8"
func nimImageRef(name, tag string) string {
	if strings.HasPrefix(name, "nvcr.io/") {
		return name
	}
	if tag == "" {
		tag = "latest"
	}
	return "nvcr.io/nim/" + name + ":" + tag
}
