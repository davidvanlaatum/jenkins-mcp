package security

import (
	"errors"
	"path/filepath"
	"strings"
)

func CleanRelativePath(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) {
		return "", errors.New("unsafe path")
	}
	clean := filepath.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "..") || strings.Contains(clean, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return "", errors.New("unsafe path")
	}
	return clean, nil
}

func SafeJoin(root, name string) (string, error) {
	if root == "" {
		return "", errors.New("root is required")
	}
	clean, err := CleanRelativePath(name)
	if err != nil {
		return "", errors.New("unsafe path")
	}
	joined := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, joined)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errors.New("unsafe path")
	}
	return joined, nil
}

func RedactSecret(s string) string {
	if s == "" {
		return ""
	}
	return "<redacted>"
}
