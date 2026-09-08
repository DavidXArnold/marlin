package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifyChecksum computes sha256(data) and compares it (hex, case-insensitive)
// against expectedHex, returning a *ChecksumMismatchError on mismatch.
func VerifyChecksum(profileID, version string, data []byte, expectedHex string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expectedHex) {
		return &ChecksumMismatchError{
			ProfileID: profileID,
			Version:   version,
			Expected:  expectedHex,
			Actual:    actual,
		}
	}
	return nil
}
