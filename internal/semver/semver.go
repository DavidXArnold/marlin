// Package semver provides a small, deliberately limited version comparator
// shared by marlin's self-upgrade check (internal/update) and its versioned
// profile distribution (internal/profile). It supports plain "major.minor.patch"
// strings only — no pre-release or build-metadata suffixes (e.g. "-rc1", "+build").
package semver

import (
	"strconv"
	"strings"
)

// Parse splits v (optionally prefixed with "v") into its three numeric
// components. Returns nil if v is not exactly "major.minor.patch" with
// integer parts.
func Parse(v string) []int {
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

// Compare returns -1, 0, or 1 as a is less than, equal to, or greater than b.
// A version that fails to Parse is treated as less than any version that
// does parse; two unparsable versions compare equal.
func Compare(a, b string) int {
	pa, pb := Parse(a), Parse(b)
	if pa == nil && pb == nil {
		return 0
	}
	if pa == nil {
		return -1
	}
	if pb == nil {
		return 1
	}
	for i := range pa {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// IsNewer reports whether candidate is a higher version than current.
// Non-parsable inputs return false (matching the historical behavior of
// internal/update.IsNewer, which this replaces).
func IsNewer(current, candidate string) bool {
	if Parse(current) == nil || Parse(candidate) == nil {
		return false
	}
	return Compare(candidate, current) > 0
}
