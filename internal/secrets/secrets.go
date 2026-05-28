package secrets

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// knownOrder controls the output order in saved files.
var knownOrder = []string{"HF_TOKEN", "NGC_API_KEY"}

// Load reads a KEY=VALUE env file and returns the parsed map.
// Lines starting with # and blank lines are ignored.
// If path does not exist an empty map is returned without error.
func Load(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("opening secrets file %s: %w", path, err)
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading secrets file %s: %w", path, err)
	}

	return result, nil
}

// Save merges updates into the secrets file at path, creating it if necessary.
// A key mapped to an empty string is removed. Existing keys not present in
// updates are preserved. File is written with mode 0600.
func Save(path string, updates map[string]string) error {
	existing, err := Load(path)
	if err != nil {
		return err
	}
	for k, v := range updates {
		if v == "" {
			delete(existing, k)
		} else {
			existing[k] = v
		}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writing secrets file %s: %w", path, err)
	}
	defer f.Close()

	bw := bufio.NewWriter(f)
	written := make(map[string]bool, len(knownOrder))
	for _, k := range knownOrder {
		if v, ok := existing[k]; ok {
			fmt.Fprintf(bw, "%s=%s\n", k, v)
			written[k] = true
		}
	}
	for k, v := range existing {
		if !written[k] {
			fmt.Fprintf(bw, "%s=%s\n", k, v)
		}
	}
	return bw.Flush()
}
