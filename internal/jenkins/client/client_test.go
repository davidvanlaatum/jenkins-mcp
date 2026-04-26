package client

import (
	"strings"
	"testing"
)

func TestReadBoundedReturnsErrorWhenLimitExceeded(t *testing.T) {
	_, err := readBounded(strings.NewReader("abcdef"), 5)
	if err == nil {
		t.Fatal("readBounded() succeeded when response exceeded limit")
	}
}

func TestReadBoundedAllowsExactLimit(t *testing.T) {
	got, err := readBounded(strings.NewReader("abcde"), 5)
	if err != nil {
		t.Fatalf("readBounded() error = %v", err)
	}
	if string(got) != "abcde" {
		t.Fatalf("readBounded() = %q", got)
	}
}
