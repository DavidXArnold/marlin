package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const ngcAPIBase = "https://api.ngc.nvidia.com/v2"

type NGC struct {
	apiKey    string
	client    *http.Client
	base      string // overridable for tests
	log       io.Writer
	verbosity int
}

func NewNGC(apiKey string) *NGC {
	return &NGC{
		apiKey: apiKey,
		client: &http.Client{},
		base:   ngcAPIBase,
	}
}

// SetVerbose enables debug logging at the given level (1=requests, 2=headers, 3=bodies).
func (n *NGC) SetVerbose(w io.Writer, level int) {
	n.log = w
	n.verbosity = level
}

func (n *NGC) logf(level int, format string, args ...any) {
	if n.log != nil && n.verbosity >= level {
		fmt.Fprintf(n.log, "[ngc] "+format, args...)
	}
}

func (n *NGC) Name() string { return "ngc" }

func (n *NGC) Search(ctx context.Context, query string) ([]ModelInfo, error) {
	endpoint := fmt.Sprintf("%s/search/resources/CONTAINER?q=%s&pageSize=20", n.base, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if n.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.apiKey)
	}

	n.logf(1, "GET %s\n", endpoint)
	if n.verbosity >= 2 {
		for k, vs := range req.Header {
			if strings.EqualFold(k, "authorization") {
				n.logf(2, "  %s: Bearer ***\n", k)
			} else {
				n.logf(2, "  %s: %s\n", k, strings.Join(vs, ", "))
			}
		}
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ngc search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ngc search: reading response: %w", err)
	}

	n.logf(1, "status: %d\n", resp.StatusCode)
	n.logf(3, "response body: %s\n", body)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if n.apiKey != "" {
			return nil, fmt.Errorf("ngc search: authentication failed — run 'marlin configure' to update NGC_API_KEY or generate a new key at https://org.ngc.nvidia.com/setup/personal-keys")
		}
		return nil, fmt.Errorf("ngc search: authentication required — run 'marlin configure' to add an NGC_API_KEY")
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		n.logf(1, "error body: %s\n", snippet)
		return nil, fmt.Errorf("ngc search: unexpected status %d: %s", resp.StatusCode, snippet)
	}

	var raw ngcSearchResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ngc search: decoding response: %w", err)
	}

	n.logf(3, "results: %d\n", len(raw.Results))

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
