package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestCurrentUsesLdflagValues(t *testing.T) {
	oldName, oldVersion, oldCommit, oldDate := Name, Version, Commit, Date
	t.Cleanup(func() {
		Name, Version, Commit, Date = oldName, oldVersion, oldCommit, oldDate
	})

	Name = "custom-agent"
	Version = "v1.2.3"
	Commit = "abc1234"
	Date = "2026-06-10T00:00:00Z"

	info := Current()
	if info.Name != Name || info.Version != Version || info.Commit != Commit || info.Date != Date {
		t.Fatalf("unexpected build info: %#v", info)
	}
	if info.GoVersion != runtime.Version() || info.GOOS != runtime.GOOS || info.GOARCH != runtime.GOARCH {
		t.Fatalf("unexpected runtime info: %#v", info)
	}
}

func TestCurrentFallsBackForEmptyValues(t *testing.T) {
	oldName, oldVersion, oldCommit, oldDate := Name, Version, Commit, Date
	t.Cleanup(func() {
		Name, Version, Commit, Date = oldName, oldVersion, oldCommit, oldDate
	})

	Name = ""
	Version = " "
	Commit = ""
	Date = ""

	info := Current()
	if info.Name != "soha-agent" || info.Version != "dev" || info.Commit != "unknown" || info.Date != "unknown" {
		t.Fatalf("unexpected fallback build info: %#v", info)
	}
}

func TestHumanIncludesBuildCoordinates(t *testing.T) {
	oldName, oldVersion, oldCommit, oldDate := Name, Version, Commit, Date
	t.Cleanup(func() {
		Name, Version, Commit, Date = oldName, oldVersion, oldCommit, oldDate
	})

	Name = "soha-agent"
	Version = "v9.9.9"
	Commit = "deadbeef"
	Date = "2026-06-10T01:02:03Z"

	got := Human()
	for _, want := range []string{"soha-agent", "v9.9.9", "deadbeef", "2026-06-10T01:02:03Z", runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(got, want) {
			t.Fatalf("Human() = %q, missing %q", got, want)
		}
	}
}
