package urlx

import (
	"net/url"
	"path"
	"strings"
)

func JobPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	encoded := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		if part != "" {
			encoded = append(encoded, "job", url.PathEscape(part))
		}
	}
	return strings.Join(encoded, "/")
}

func Join(base string, elems ...string) string {
	out := strings.TrimRight(base, "/")
	for _, elem := range elems {
		if elem != "" {
			out += "/" + strings.Trim(elem, "/")
		}
	}
	return out
}

func RelativePath(relativePath string) string {
	parts := strings.Split(path.Clean(relativePath), "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." {
			encoded = append(encoded, url.PathEscape(part))
		}
	}
	return strings.Join(encoded, "/")
}
