package pagination

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type Page struct {
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor,omitempty"`
	NextCursor string `json:"nextCursor,omitempty"`
	Truncated  bool   `json:"truncated"`
}

func EncodeOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(string(rune(offset))))
}

const (
	cursorVersion       = 1
	maxCursorTokenBytes = 4096
)

type cursorEnvelope struct {
	Version   int    `json:"v"`
	Kind      string `json:"k"`
	Offset    int    `json:"o"`
	Signature string `json:"s,omitempty"`
	MAC       string `json:"m"`
}

var (
	cursorKey     []byte
	cursorKeyErr  error
	cursorKeyOnce sync.Once
)

func EncodeCursor(kind string, offset int, signature string) (string, error) {
	if kind == "" {
		return "", errors.New("cursor kind is required")
	}
	if offset < 0 {
		return "", errors.New("cursor offset must be non-negative")
	}
	env := cursorEnvelope{Version: cursorVersion, Kind: kind, Offset: offset, Signature: signature}
	mac, err := cursorMAC(env)
	if err != nil {
		return "", err
	}
	env.MAC = mac
	body, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func DecodeCursor(token string, kind string) (offset int, signature string, err error) {
	if token == "" {
		return 0, "", nil
	}
	if len(token) > maxCursorTokenBytes {
		return 0, "", errors.New("cursor token is too large")
	}
	body, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, "", errors.New("invalid cursor encoding")
	}
	var env cursorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, "", errors.New("invalid cursor payload")
	}
	if env.Version != cursorVersion {
		return 0, "", fmt.Errorf("unsupported cursor version %d", env.Version)
	}
	if env.Kind != kind {
		return 0, "", errors.New("cursor is not valid for this operation")
	}
	if env.Offset < 0 {
		return 0, "", errors.New("cursor offset must be non-negative")
	}
	want, err := cursorMAC(cursorEnvelope{Version: env.Version, Kind: env.Kind, Offset: env.Offset, Signature: env.Signature})
	if err != nil {
		return 0, "", err
	}
	if !hmac.Equal([]byte(env.MAC), []byte(want)) {
		return 0, "", errors.New("invalid cursor signature")
	}
	return env.Offset, env.Signature, nil
}

func cursorMAC(env cursorEnvelope) (string, error) {
	key, err := getCursorKey()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(struct {
		Version   int    `json:"v"`
		Kind      string `json:"k"`
		Offset    int    `json:"o"`
		Signature string `json:"s,omitempty"`
	}{Version: env.Version, Kind: env.Kind, Offset: env.Offset, Signature: env.Signature})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func getCursorKey() ([]byte, error) {
	cursorKeyOnce.Do(func() {
		cursorKey = make([]byte, 32)
		if _, err := rand.Read(cursorKey); err != nil {
			cursorKeyErr = err
		}
	})
	return cursorKey, cursorKeyErr
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
