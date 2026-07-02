package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ValidateNestedEquals confirms that a JSON file contains the given nested key
// path with the expected string value. Used by providers whose config is JSON.
func ValidateNestedEquals(file string, path []string, want string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}

	cur := root
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: %q is not an object", file, key)
		}
		cur, ok = m[key]
		if !ok {
			return fmt.Errorf("%s: missing key %q", file, strings.Join(path, "."))
		}
	}

	got, ok := cur.(string)
	if !ok {
		return fmt.Errorf("%s: %s is not a string", file, strings.Join(path, "."))
	}
	if got != want {
		return fmt.Errorf("%s: %s is %q, want %q", file, strings.Join(path, "."), got, want)
	}
	return nil
}

// ValidateFileContains confirms a (text) config file exists and contains the
// given substring. Used by providers whose config is a managed text block.
func ValidateFileContains(file, want string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	if !strings.Contains(string(data), want) {
		return fmt.Errorf("%s does not reference the Brevitas proxy (%s)", file, want)
	}
	return nil
}

// ValidateJSONAnyContains confirms that any string value anywhere in a JSON
// document equals want. Used for array-shaped configs (e.g. Continue models)
// where the exact index is not known.
func ValidateJSONAnyContains(file, want string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	if jsonContainsString(root, want) {
		return nil
	}
	return fmt.Errorf("%s does not reference the Brevitas proxy (%s)", file, want)
}

func jsonContainsString(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(t, want)
	case map[string]any:
		for _, e := range t {
			if jsonContainsString(e, want) {
				return true
			}
		}
	case []any:
		for _, e := range t {
			if jsonContainsString(e, want) {
				return true
			}
		}
	}
	return false
}
