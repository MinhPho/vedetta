package safepath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Join returns a cleaned absolute path under root, rejecting lexical traversal.
func Join(root string, elems ...string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("root path is required")
	}

	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}

	parts := append([]string{cleanRoot}, elems...)
	target, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}

	rel, err := filepath.Rel(cleanRoot, target)
	if err != nil {
		return "", fmt.Errorf("rel target: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes root")
	}
	return target, nil
}

// FileComponent maps arbitrary text to a conservative single filename component.
func FileComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 160 {
			break
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		return "item"
	}
	return out
}
