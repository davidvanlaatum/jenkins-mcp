package pagination

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	token, err := EncodeCursor("list", 42, "request-signature")
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	offset, signature, err := DecodeCursor(token, "list")
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if offset != 42 || signature != "request-signature" {
		t.Fatalf("DecodeCursor() = %d, %q; want 42, request-signature", offset, signature)
	}
}

func TestDecodeCursorRejectsWrongKind(t *testing.T) {
	token, err := EncodeCursor("list", 42, "")
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	if _, _, err := DecodeCursor(token, "other"); err == nil {
		t.Fatal("DecodeCursor() accepted cursor for wrong kind")
	}
}

func TestDecodeCursorRejectsTampering(t *testing.T) {
	token, err := EncodeCursor("list", 42, "")
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	replacement := "A"
	if token[len(token)-1:] == replacement {
		replacement = "B"
	}
	tampered := token[:len(token)-1] + replacement
	if _, _, err := DecodeCursor(tampered, "list"); err == nil {
		t.Fatal("DecodeCursor() accepted tampered cursor")
	}
}

func TestDecodeCursorRejectsOversizedToken(t *testing.T) {
	token := make([]byte, maxCursorTokenBytes+1)
	for i := range token {
		token[i] = 'A'
	}

	if _, _, err := DecodeCursor(string(token), "list"); err == nil {
		t.Fatal("DecodeCursor() accepted oversized cursor")
	}
}
