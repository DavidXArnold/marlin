package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const apiURL = "https://api.github.com/repos/DavidXArnold/marlin/releases/latest"

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
	defer resp.Body.Close()

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
