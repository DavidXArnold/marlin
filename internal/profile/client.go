package profile

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

const defaultRawBase = "https://raw.githubusercontent.com"

// Source fetches profile manifests and files from a GitHub repo's raw
// content, unauthenticated — this avoids the tight rate limits of
// api.github.com and mirrors how internal/update downloads release assets
// directly from github.com rather than through the API.
type Source struct {
	RepoOwner, RepoName, RepoRef, Subdir string

	client    *http.Client
	base      string // overridable for tests
	log       io.Writer
	verbosity int
}

// NewSource builds a Source for the given repo/ref/subdir.
func NewSource(repoOwner, repoName, repoRef, subdir string) *Source {
	return &Source{
		RepoOwner: repoOwner,
		RepoName:  repoName,
		RepoRef:   repoRef,
		Subdir:    subdir,
		client:    &http.Client{Timeout: 15 * time.Second},
		base:      defaultRawBase,
	}
}

// NewSourceWithBase is a test constructor pointing at a custom base URL
// (e.g. an httptest server) instead of raw.githubusercontent.com. Exported
// so other packages' tests (cmd's `marlin profile` tests, in particular) can
// inject a fake Source without a real network dependency.
func NewSourceWithBase(repoOwner, repoName, repoRef, subdir, base string) *Source {
	s := NewSource(repoOwner, repoName, repoRef, subdir)
	s.base = base
	return s
}

// SetVerbose enables debug logging at the given level (1=requests, 2=headers, 3=bodies).
func (s *Source) SetVerbose(w io.Writer, level int) {
	s.log = w
	s.verbosity = level
}

func (s *Source) logf(level int, format string, args ...any) {
	if s.log != nil && s.verbosity >= level {
		_, _ = fmt.Fprintf(s.log, "[profile] "+format, args...)
	}
}

func (s *Source) manifestURL() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s/manifest.json", s.base, s.RepoOwner, s.RepoName, s.RepoRef, s.Subdir)
}

func (s *Source) fileURL(relPath string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s", s.base, s.RepoOwner, s.RepoName, s.RepoRef, s.Subdir, relPath)
}

// FetchManifest downloads and parses profiles/manifest.json.
func (s *Source) FetchManifest(ctx context.Context) (*Manifest, error) {
	body, err := s.get(ctx, s.manifestURL(), "manifest")
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&m); err != nil {
		return nil, fmt.Errorf("profile manifest: decoding response: %w", err)
	}
	return &m, nil
}

// FetchProfileBytes downloads the raw TOML for a profile file at relPath
// (as published in the manifest, relative to the configured subdir).
func (s *Source) FetchProfileBytes(ctx context.Context, relPath string) ([]byte, error) {
	return s.get(ctx, s.fileURL(relPath), "file "+relPath)
}

func (s *Source) get(ctx context.Context, endpoint, what string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	s.logf(1, "GET %s\n", endpoint)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("profile %s: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("profile %s: reading response: %w", what, err)
	}

	s.logf(1, "status: %d\n", resp.StatusCode)
	s.logf(3, "response body: %s\n", body)

	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		return nil, fmt.Errorf("profile %s: unexpected status %d: %s", what, resp.StatusCode, snippet)
	}

	return body, nil
}
