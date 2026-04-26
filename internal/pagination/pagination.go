package pagination

import "encoding/base64"

type Page struct {
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor,omitempty"`
	NextCursor string `json:"nextCursor,omitempty"`
	Truncated  bool   `json:"truncated"`
}

func EncodeOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(string(rune(offset))))
}
func BoundLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
func TruncateString(s string, max int64) (string, bool) {
	if max <= 0 || int64(len(s)) <= max {
		return s, false
	}
	return s[:max], true
}
