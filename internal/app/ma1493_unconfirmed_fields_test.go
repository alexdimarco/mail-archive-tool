package app

import "testing"

// covers: MA-1493
// An R-f2 refusal says its archive and its cause (the per-mailbox friction review's F9): refusalF2
// keeps the archive it could not confirm the folder is outside, why ("here", "fuse", "walk", or ""
// for two different shares) and whether the cause is on the archive's side, so the wizard can name
// the archive and give each cause its own remedy. Its words are unchanged. Positive twin first: a
// refusal of another kind carries none of them.
func TestAnUnconfirmedRefusalSaysItsArchiveAndCause(t *testing.T) {
	if r := refusalF("/a"); r.Archive != "" || r.Cause != "" || r.ArchiveSide {
		t.Fatalf("positive twin: R-f carries R-f2's fields: %+v", r)
	}
	for _, c := range []struct {
		why  string
		side bool
		want string
	}{{"here", false, "here"}, {"fuse", true, "fuse"}, {"walk", false, "walk"}, {"shares", false, ""}} {
		r := refusalF2("/dest", "/archive", c.why, c.side, false)
		if r.ID != "R-f2" || r.Archive != "/archive" || r.Cause != c.want || r.ArchiveSide != c.side || r.Message == "" {
			t.Errorf("why %q: %+v", c.why, r)
		}
	}
}
