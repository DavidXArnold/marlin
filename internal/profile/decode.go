package profile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/DavidXArnold/marlin/internal/config"
)

// DecodeResult is the outcome of parsing a downloaded profile TOML file.
type DecodeResult struct {
	// Config holds only the fields this build's config.ModelConfig struct
	// understands — that's inherent to typed TOML decoding, so no separate
	// "strip unknown fields" step is needed.
	Config *config.ModelConfig
	// UnrecognizedKeys lists every top-level field TOML held that the local
	// schema doesn't map to a struct field, formatted "[table].field" (or
	// just "field" for a top-level key with no table), sorted.
	UnrecognizedKeys []string
}

// Decode parses raw TOML into a config.ModelConfig, using BurntSushi's
// MetaData.Undecoded() to capture which keys the local schema doesn't
// recognize. It never errors solely because of unrecognized fields — the
// caller decides via the schema-version gate whether that's fatal.
func Decode(raw []byte) (*DecodeResult, error) {
	var cfg config.ModelConfig
	md, err := toml.Decode(string(raw), &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing profile TOML: %w", err)
	}

	keys := md.Undecoded()
	unrecognized := make([]string, 0, len(keys))
	for _, k := range keys {
		unrecognized = append(unrecognized, formatKey(k))
	}
	sort.Strings(unrecognized)

	return &DecodeResult{Config: &cfg, UnrecognizedKeys: unrecognized}, nil
}

// formatKey renders a toml.Key as "[table].field" for a nested key, or just
// "field" for a single top-level key — matching the spec's warning example
// (e.g. "[serve].moe_backend").
func formatKey(k toml.Key) string {
	if len(k) <= 1 {
		return k.String()
	}
	table := strings.Join(k[:len(k)-1], ".")
	field := k[len(k)-1]
	return fmt.Sprintf("[%s].%s", table, field)
}
