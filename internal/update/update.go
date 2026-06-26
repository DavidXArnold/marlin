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
	"strconv"
	"strings"
)

const (
	apiURL    = "https://api.github.com/repos/DavidXArnold/marlin/releases/latest"
	repoOwner = "DavidXArnold"
	repoName  = "marlin"
)

// HTTPClient is injectable for tests.
var HTTPClient = &http.Client{}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

// Check fetches the latest GitHub release and reports whether it is newer than
// current. Returns the latest tag and true when an update is available; returns
// ("", false, nil) when current is already up to date.
func Check(ctx context.Context, current string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("github releases API: status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", false, fmt.Errorf("github releases API: %w", err)
	}

	if IsNewer(current, rel.TagName) {
		return rel.TagName, true, nil
	}
	return "", false, nil
}

// IsNewer reports whether candidate is a higher semver than current.
// Both may optionally carry a leading "v". Non-semver strings return false.
func IsNewer(current, candidate string) bool {
	c := parseVer(current)
	n := parseVer(candidate)
	if c == nil || n == nil {
		return false
	}
	for i := range c {
		if n[i] > c[i] {
			return true
		}
		if n[i] < c[i] {
			return false
		}
	}
	return false
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

func parseVer(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}
