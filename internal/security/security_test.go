package security

import "testing"

func TestSafeJoinRejectsTraversal(t *testing.T) {
	tests := []string{"../secret", "/tmp/secret", "nested/../../secret"}
	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			if _, err := SafeJoin("/tmp/root", test); err == nil {
				t.Fatal("SafeJoin() accepted unsafe path")
			}
		})
	}
}

func TestSafeJoinAllowsNestedRelativePath(t *testing.T) {
	got, err := SafeJoin("/tmp/root", "job/artifact.txt")
	if err != nil {
		t.Fatalf("SafeJoin() error = %v", err)
	}
	if got != "/tmp/root/job/artifact.txt" {
		t.Fatalf("SafeJoin() = %q", got)
	}
}
