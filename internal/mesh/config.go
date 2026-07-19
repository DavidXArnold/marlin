// Package mesh manages marlin's integration with a local mesh-llm peer.
package mesh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PatchOpenAIEndpoint ensures the mesh-llm config file at configPath contains
// an [[plugin]] block of type "openai-endpoint" whose url field equals vLLMURL.
// Creates the file (and any parent directories) when absent.
// Returns (true, nil) when the file was written; (false, nil) when already correct.
func PatchOpenAIEndpoint(configPath, vLLMURL string) (changed bool, err error) {
	data, readErr := os.ReadFile(configPath)
	if os.IsNotExist(readErr) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return false, fmt.Errorf("creating mesh config directory: %w", err)
		}
		content := "version = 1\n" + openAIPluginBlock(vLLMURL)
		return true, os.WriteFile(configPath, []byte(content), 0o644)
	}
	if readErr != nil {
		return false, fmt.Errorf("reading mesh config %s: %w", configPath, readErr)
	}

	lines := strings.Split(string(data), "\n")
	start, end, urlLine := findOpenAIBlock(lines)

	if start >= 0 {
		// Block found — check if URL already matches.
		if urlLine >= 0 && strings.TrimSpace(lines[urlLine]) == `url = "`+vLLMURL+`"` {
			return false, nil
		}
		// Update the url line in-place, or insert one if missing.
		newLines := make([]string, len(lines))
		copy(newLines, lines)
		if urlLine >= 0 {
			newLines[urlLine] = `url = "` + vLLMURL + `"`
		} else {
			// Insert url after [[plugin]] line.
			ins := make([]string, 0, len(lines)+1)
			ins = append(ins, newLines[:start+1]...)
			ins = append(ins, `url = "`+vLLMURL+`"`)
			ins = append(ins, newLines[start+1:]...)
			newLines = ins
		}
		_ = end
		return true, os.WriteFile(configPath, []byte(strings.Join(newLines, "\n")), 0o644)
	}

	// Block not found: append to end of file.
	raw := string(data)
	if !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	raw += openAIPluginBlock(vLLMURL)
	return true, os.WriteFile(configPath, []byte(raw), 0o644)
}

// RemoveOpenAIEndpoint removes the openai-endpoint [[plugin]] block from the
// mesh-llm config at configPath. No-op if absent or file missing.
// Returns (true, nil) when the file was modified.
func RemoveOpenAIEndpoint(configPath string) (changed bool, err error) {
	data, readErr := os.ReadFile(configPath)
	if os.IsNotExist(readErr) {
		return false, nil
	}
	if readErr != nil {
		return false, fmt.Errorf("reading mesh config %s: %w", configPath, readErr)
	}

	lines := strings.Split(string(data), "\n")
	start, end, _ := findOpenAIBlock(lines)
	if start < 0 {
		return false, nil
	}

	// Remove lines [start, end), collapsing any resulting double blank lines.
	newLines := append(append([]string{}, lines[:start]...), lines[end:]...)
	return true, os.WriteFile(configPath, []byte(strings.Join(newLines, "\n")), 0o644)
}

// PatchJoinToken writes the invite token into the mesh-llm config file's
// [owner_control] section as join_token. Creates the file when absent.
func PatchJoinToken(configPath, token string) error {
	data, readErr := os.ReadFile(configPath)
	if os.IsNotExist(readErr) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return fmt.Errorf("creating mesh config directory: %w", err)
		}
		content := fmt.Sprintf("version = 1\n\n[owner_control]\njoin_token = %q\n", token)
		return os.WriteFile(configPath, []byte(content), 0o644)
	}
	if readErr != nil {
		return fmt.Errorf("reading mesh config %s: %w", configPath, readErr)
	}

	lines := strings.Split(string(data), "\n")

	// Look for an existing join_token line inside [owner_control].
	inSection := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[owner_control]" {
			inSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && trimmed != "[owner_control]" {
			inSection = false
		}
		if inSection && fieldName(trimmed) == "join_token" {
			lines[i] = fmt.Sprintf("join_token = %q", token)
			return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}

	// [owner_control] section not found or join_token missing — append.
	raw := string(data)
	if !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	raw += fmt.Sprintf("\n[owner_control]\njoin_token = %q\n", token)
	return os.WriteFile(configPath, []byte(raw), 0o644)
}

// openAIPluginBlock returns the TOML text for an openai-endpoint plugin block.
func openAIPluginBlock(vLLMURL string) string {
	return fmt.Sprintf(`
[[plugin]]
name = "openai-endpoint"
url = %q

[plugin.startup]
optional = true
lazy_start = true
`, vLLMURL)
}

// findOpenAIBlock locates the [[plugin]] block with name = "openai-endpoint"
// in lines. Returns (start, end, urlLine) where start is the index of the
// [[plugin]] line, end is the first line index after the block (exclusive),
// and urlLine is the index of the url = "..." line within the block (-1 if absent).
// Returns (-1, -1, -1) when the block is not found.
func findOpenAIBlock(lines []string) (start, end, urlLine int) {
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		if trimmed == "[[plugin]]" {
			bStart := i
			bURLLine := -1
			isTarget := false

			i++
			for i < len(lines) {
				inner := strings.TrimSpace(lines[i])
				// Another array table or a non-plugin top-level table ends this block.
				if strings.HasPrefix(inner, "[[") {
					break
				}
				if strings.HasPrefix(inner, "[") && !strings.HasPrefix(inner, "[plugin.") {
					break
				}
				if fieldValue(inner, "name") == "openai-endpoint" {
					isTarget = true
				}
				if fieldName(inner) == "url" {
					bURLLine = i
				}
				i++
			}

			if isTarget {
				return bStart, i, bURLLine
			}
			// Not our target — don't advance i; let outer loop re-process the break line.
		} else {
			i++
		}
	}
	return -1, -1, -1
}

// fieldValue parses a TOML key = "value" line and returns the unquoted value
// if the key matches field, otherwise returns "".
func fieldValue(line, field string) string {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) != field {
		return ""
	}
	v := strings.TrimSpace(parts[1])
	v = strings.Trim(v, `"'`)
	return v
}

// fieldName returns the key name from a TOML key = value line, or "" if unparseable.
func fieldName(line string) string {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}
