package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DavidXArnold/marlin/internal/semver"
)

const (
	apiURL    = "https://api.github.com/repos/DavidXArnold/marlin/releases/latest"
	webURL    = "https://github.com/DavidXArnold/marlin/releases/latest"
	repoOwner = "DavidXArnold"
	repoName  = "marlin"
)

// HTTPClient is injectable for tests.
var HTTPClient = &http.Client{}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

// LatestTag returns the newest release tag. It first follows the
// github.com/.../releases/latest web redirect, which isn't subject to the
// REST API's 60-requests/hour unauthenticated limit, and falls back to the
// API (authenticated with GITHUB_TOKEN when set) if that doesn't resolve.
func LatestTag(ctx context.Context) (string, error) {
	if tag, err := latestTagFromRedirect(ctx); err == nil {
		return tag, nil
	}
	return latestTagFromAPI(ctx)
}

// latestTagFromRedirect reads the tag from the Location header of the
// releases/latest page (".../releases/tag/v1.2.3") without following it.
func latestTagFromRedirect(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, webURL, nil)
	if err != nil {
		return "", err
	}
	noFollow := &http.Client{
		Transport:     HTTPClient.Transport,
		Timeout:       HTTPClient.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	loc := resp.Header.Get("Location")
	const marker = "/releases/tag/"
	i := strings.LastIndex(loc, marker)
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || i < 0 {
		return "", fmt.Errorf("releases/latest: no tag redirect (status %d)", resp.StatusCode)
	}
	tag := loc[i+len(marker):]
	if tag == "" || strings.Contains(tag, "/") {
		return "", fmt.Errorf("releases/latest: unexpected redirect %q", loc)
	}
	return tag, nil
}

func latestTagFromAPI(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return "", fmt.Errorf("github releases API: status %d (rate limited — set GITHUB_TOKEN or retry later)", resp.StatusCode)
		}
		return "", fmt.Errorf("github releases API: status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("github releases API: %w", err)
	}
	return rel.TagName, nil
}

// Check fetches the latest release and reports whether it is newer than
// current. Returns the latest tag and true when an update is available; returns
// ("", false, nil) when current is already up to date.
func Check(ctx context.Context, current string) (string, bool, error) {
	tag, err := LatestTag(ctx)
	if err != nil {
		return "", false, err
	}
	if IsNewer(current, tag) {
		return tag, true, nil
	}
	return "", false, nil
}

type checkCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// CheckCached is Check backed by an on-disk cache at cachePath, so the
// background update notice costs at most one network request per ttl rather
// than one per marlin invocation. On a network error it falls back to any
// cached answer, however old; cache write failures are ignored.
func CheckCached(ctx context.Context, current, cachePath string, ttl time.Duration) (string, bool, error) {
	var cached *checkCache
	if data, err := os.ReadFile(cachePath); err == nil {
		var c checkCache
		if json.Unmarshal(data, &c) == nil && c.Latest != "" {
			cached = &c
		}
	}

	latest := ""
	if cached != nil && time.Since(cached.CheckedAt) < ttl {
		latest = cached.Latest
	} else {
		tag, err := LatestTag(ctx)
		switch {
		case err == nil:
			latest = tag
			if data, mErr := json.Marshal(checkCache{CheckedAt: time.Now(), Latest: tag}); mErr == nil {
				if os.MkdirAll(filepath.Dir(cachePath), 0o755) == nil {
					_ = os.WriteFile(cachePath, data, 0o644)
				}
			}
		case cached != nil:
			latest = cached.Latest
		default:
			return "", false, err
		}
	}

	if IsNewer(current, latest) {
		return latest, true, nil
	}
	return "", false, nil
}

// IsNewer reports whether candidate is a higher semver than current.
// Both may optionally carry a leading "v". Non-semver strings return false.
func IsNewer(current, candidate string) bool {
	return semver.IsNewer(current, candidate)
}

// AssetURL returns the GitHub release download URL for the tar.gz archive
// matching version, goos, and goarch. version may carry a leading "v".
// The goreleaser archive template is: marlin_{semver}_{os}_{arch}.tar.gz
func AssetURL(version, goos, goarch string) string {
	v := strings.TrimPrefix(version, "v")
	name := fmt.Sprintf("marlin_%s_%s_%s.tar.gz", v, goos, goarch)
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		repoOwner, repoName, version, name)
}

// Download fetches url and writes the body to destPath, overwriting any existing
// file. The caller is responsible for removing destPath on error.
func Download(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

// ExtractBinary extracts the regular file named binName from a .tar.gz archive
// at archivePath and writes it to destPath with mode 0o755.
func ExtractBinary(archivePath, binName, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		if filepath.Base(hdr.Name) != binName || hdr.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("binary %q not found in archive %s", binName, archivePath)
}
