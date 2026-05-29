package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// nimCatalogBase is the NVIDIA NIM hosted API catalog endpoint. The /v1/models
// list covers all publicly available NIM containers and uses the same NGC API key.
const nimCatalogBase = "https://integrate.api.nvidia.com"

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
		base:   nimCatalogBase,
	}
}

// SetVerbose enables debug logging at the given level (1=requests, 2=headers, 3=bodies).
func (n *NGC) SetVerbose(w io.Writer, level int) {
	n.log = w
	n.verbosity = level
}

func (n *NGC) logf(level int, format string, args ...any) {
	if n.log != nil && n.verbosity >= level {
		_, _ = fmt.Fprintf(n.log, "[ngc] "+format, args...)
	}
}

func (n *NGC) Name() string { return "ngc" }

func (n *NGC) Search(ctx context.Context, query string) ([]ModelInfo, error) {
	endpoint := fmt.Sprintf("%s/v1/models", n.base)

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

	var raw nimModelsResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("ngc search: decoding response: %w", err)
	}

	lower := strings.ToLower(query)
	var results []ModelInfo
	for _, m := range raw.Data {
		if strings.Contains(strings.ToLower(m.ID), lower) {
			results = append(results, m.toModelInfo())
		}
		if len(results) == 20 {
			break
		}
	}
	n.logf(3, "matched %d of %d models\n", len(results), len(raw.Data))
	return results, nil
}

func (n *NGC) Fetch(_ context.Context, _ string) (*ModelInfo, error) {
	return nil, fmt.Errorf("ngc fetch not yet implemented")
}

type nimModelsResponse struct {
	Data []nimModel `json:"data"`
}

type nimModel struct {
	ID      string `json:"id"`       // e.g. "meta/llama-3.1-8b-instruct"
	Created int64  `json:"created"`  // Unix timestamp
	OwnedBy string `json:"owned_by"` // e.g. "meta"
}

// nimEpoch is the earliest plausible NIM container creation date (2022-01-01 UTC).
// The NVIDIA API catalog returns non-Unix `created` values for older entries;
// we treat anything before this as unknown rather than showing bogus dates.
const nimEpoch = int64(1640995200)

func (m nimModel) toModelInfo() ModelInfo {
	info := ModelInfo{
		ID:       nimImageRef(m.ID, ""),
		Registry: "ngc",
	}
	if m.Created >= nimEpoch {
		info.LastUpdated = time.Unix(m.Created, 0)
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
