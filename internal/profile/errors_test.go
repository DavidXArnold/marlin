package profile

import "testing"

func TestProfileNotFoundErrorMessage(t *testing.T) {
	err := &ProfileNotFoundError{ProfileID: "foo"}
	want := `profile "foo" not found in manifest`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestVersionNotFoundErrorMessage(t *testing.T) {
	err := &VersionNotFoundError{ProfileID: "foo", Requested: "9.9.9", Available: []string{"1.0.0", "1.1.0"}}
	want := `profile "foo" has no version "9.9.9" — available versions: 1.0.0, 1.1.0`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}
