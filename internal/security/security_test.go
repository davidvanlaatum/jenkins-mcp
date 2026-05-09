package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeJoinRejectsTraversal(t *testing.T) {
	tests := []string{"../secret", "/tmp/secret", "nested/../../secret"}
	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			r := require.New(t)

			_, err := SafeJoin("/tmp/root", test)
			r.Error(err, "SafeJoin() should reject unsafe path")
		})
	}
}

func TestSafeJoinAllowsNestedRelativePath(t *testing.T) {
	r := require.New(t)

	got, err := SafeJoin("/tmp/root", "job/artifact.txt")
	r.NoError(err, "SafeJoin()")
	r.Equal("/tmp/root/job/artifact.txt", got, "SafeJoin()")
}
