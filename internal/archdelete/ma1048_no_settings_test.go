package archdelete

import (
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// covers: MA-1048, R36
// The slice-2 adversarial pass's LOW-4, its last item (docs/design-archive-delete.md §14's slice-2b
// note, "Not changed: a computer whose settings folder cannot be found"; built in slice 5): with no
// home folder set, MailArchive's settings folder — and so this computer's deletion log — cannot be
// found, so no log is asked. A folder holding the delete marker is still refused, but in words that
// say what happened — the settings folder could not be found, and why — and name the remedy, never
// that this computer's log "has no record of it" (lead's words, owner-delegated 2026-11-02);
// `status`'s line takes the same fact (WithoutSettings). Positive twin first: a folder without the
// marker gets the no-settings refusal it always had.
func TestMA1048_NoSettingsFolderNeverSaysTheLogHasNoRecord(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")
	_, why := DefaultLogPath()
	if why == nil {
		t.Fatal("precondition: with no home folder, MailArchive's settings folder cannot be found")
	}
	cause := cleanErr(why)

	plain := smallArchive(t, filepath.Join(t.TempDir(), "Plain"))
	err := ActionRefusal("", plain, "it was not paused")
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("MailArchive could not find its settings folder, which holds its deletion log", "Check that your home folder is set"))
	if c := ma1045Case(err); c != CaseLogUnreadable {
		t.Fatalf("positive twin: case %q", c)
	}

	half := smallArchive(t, filepath.Join(t.TempDir(), "Half"))
	plantMarker(t, half)
	names := func() []string { n := ma1044Names(half); slices.Sort(n); return n }
	before := names()
	for _, done := range []string{"it was not paused", delmark.WhatNotBackedUp} {
		err = ActionRefusal("", half, done)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names(delmark.NoSettingsWords(done, cause)),
			assure.Forbid("has no record of it", "another computer", "click Delete again"),
			assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
		if c := ma1045Case(err); c != CaseUnfinished {
			t.Fatalf("%s: case %q", done, c)
		}
	}
	err = BackupRefusal("", half)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.NoSettingsWords(delmark.WhatNotBackedUp, cause)))

	u, uerr := UnfinishedToSay("", half, "")
	if uerr != nil || u == nil || !u.MarkerOnly || u.NoSettings != "" {
		t.Fatalf("status's ask, before WithoutSettings: %+v %v", u, uerr)
	}
	if u = WithoutSettings(u, why); u.NoSettings != cause {
		t.Fatalf("WithoutSettings: %q, want %q", u.NoSettings, cause)
	}
	if WithoutSettings(nil, why) != nil {
		t.Fatal("WithoutSettings of no delete is none")
	}
}
