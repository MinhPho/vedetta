package api

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxDisplayNameLength = 120

func normalizeRequiredDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	return normalizeOptionalDisplayName(name)
}

func normalizeOptionalDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if utf8.RuneCountInString(name) > maxDisplayNameLength {
		return "", fmt.Errorf("name must be at most %d characters", maxDisplayNameLength)
	}
	for _, r := range name {
		if r == '<' || r == '>' {
			return "", fmt.Errorf("name cannot contain angle brackets")
		}
		if unicode.IsControl(r) {
			return "", fmt.Errorf("name cannot contain control characters")
		}
	}
	return name, nil
}
