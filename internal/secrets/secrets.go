package secrets

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

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
