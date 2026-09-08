package profile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// cachedManifest is the on-disk shape of the manifest TTL cache — the first
// real TTL disk cache in this codebase (the self-upgrade check hits the
// network on every invocation with no caching at all; this is deliberately
// different).
type cachedManifest struct {
	FetchedAt time.Time `json:"fetched_at"`
	Manifest  Manifest  `json:"manifest"`
}

// loadCache reads the cache file at path. It is tolerant of a missing or
// corrupt file — always returns nil rather than an error in that case,
// matching internal/state.Load's "missing is not fatal" convention.
func loadCache(path string) *cachedManifest {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c cachedManifest
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

func saveCache(path string, c *cachedManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadManifest returns the profile manifest, preferring a fresh on-disk
// cache (age < ttl) over a network fetch. forceRefresh bypasses the cache
// unconditionally. If the network fetch fails and a stale (or any-age) cache
// exists, LoadManifest falls back to it silently rather than erroring —
// keeping `list`/`pull` usable when GitHub is briefly unreachable — and only
// returns an error when there is no usable cache at all.
func LoadManifest(ctx context.Context, src *Source, cachePath string, ttl time.Duration, forceRefresh bool) (*Manifest, error) {
	if !forceRefresh {
		if cached := loadCache(cachePath); cached != nil && time.Since(cached.FetchedAt) < ttl {
			m := cached.Manifest
			return &m, nil
		}
	}

	m, err := src.FetchManifest(ctx)
	if err != nil {
		if cached := loadCache(cachePath); cached != nil {
			fallback := cached.Manifest
			return &fallback, nil
		}
		return nil, err
	}

	// Best-effort cache write — a failure here shouldn't fail the command.
	_ = saveCache(cachePath, &cachedManifest{FetchedAt: time.Now(), Manifest: *m})

	return m, nil
}
