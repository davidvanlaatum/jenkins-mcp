package pagination

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorRoundTrip(t *testing.T) {
	r := require.New(t)

	token, err := EncodeCursor("list", 42, "request-signature")
	r.NoError(err, "EncodeCursor()")

	offset, signature, err := DecodeCursor(token, "list")
	r.NoError(err, "DecodeCursor()")
	r.Equal(42, offset, "DecodeCursor() offset")
	r.Equal("request-signature", signature, "DecodeCursor() signature")
}

func TestDecodeCursorRejectsWrongKind(t *testing.T) {
	r := require.New(t)

	token, err := EncodeCursor("list", 42, "")
	r.NoError(err, "EncodeCursor()")

	_, _, err = DecodeCursor(token, "other")
	r.Error(err, "DecodeCursor() should reject cursor for wrong kind")
}

func TestDecodeCursorRejectsTampering(t *testing.T) {
	r := require.New(t)

	token, err := EncodeCursor("list", 42, "")
	r.NoError(err, "EncodeCursor()")

	replacement := "A"
	if token[len(token)-1:] == replacement {
		replacement = "B"
	}
	tampered := token[:len(token)-1] + replacement

	_, _, err = DecodeCursor(tampered, "list")
	r.Error(err, "DecodeCursor() should reject tampered cursor")
}

func TestDecodeCursorRejectsOversizedToken(t *testing.T) {
	r := require.New(t)

	token := make([]byte, maxCursorTokenBytes+1)
	for i := range token {
		token[i] = 'A'
	}

	_, _, err := DecodeCursor(string(token), "list")
	r.Error(err, "DecodeCursor() should reject oversized cursor")
}
