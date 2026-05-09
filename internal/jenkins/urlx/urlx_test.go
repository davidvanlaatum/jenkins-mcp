package urlx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobPathEncodesFolders(t *testing.T) {
	r := require.New(t)

	got := JobPath("Folder A/My Job")
	want := "job/Folder%20A/job/My%20Job"
	r.Equal(want, got, "JobPath()")
}

func TestRelativePathEscapesSegments(t *testing.T) {
	r := require.New(t)

	got := RelativePath("linux reports/report #1.xml")
	want := "linux%20reports/report%20%231.xml"
	r.Equal(want, got, "RelativePath()")
}
