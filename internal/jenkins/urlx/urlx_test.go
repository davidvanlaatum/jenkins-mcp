package urlx

import "testing"

func TestJobPathEncodesFolders(t *testing.T) {
	got := JobPath("Folder A/My Job")
	want := "job/Folder%20A/job/My%20Job"
	if got != want {
		t.Fatalf("JobPath() = %q, want %q", got, want)
	}
}

func TestRelativePathEscapesSegments(t *testing.T) {
	got := RelativePath("linux reports/report #1.xml")
	want := "linux%20reports/report%20%231.xml"
	if got != want {
		t.Fatalf("RelativePath() = %q, want %q", got, want)
	}
}
