package tbrestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1122, R40
// Friction F13 (settled 2026-11-07): the result groups its count's digits as the plan and
// the progress do ("15,000", not "15000"), and says "1 message" for one.
func TestTheResultGroupsItsCount(t *testing.T) {
	big, one := doneWords(15000, "Restored — Big"), doneWords(1, "Restored — One")
	assure.Reached(t, strings.HasPrefix(big, "Restored 15,000 messages into Thunderbird as the folder 'Restored — Big'"), big)
	assure.Reached(t, strings.HasPrefix(one, "Restored 1 message into Thunderbird"), one)
	assure.Reached(t, Count(1234567, "folder") == "1,234,567 folders" && Count(999, "message") == "999 messages", Count(1234567, "folder"))
}

// covers: MA-1084, R40
// Friction P1 (settled 2026-11-07, the lead's call): the profile list says, for each listed
// profile, whether a restore can go into it — Local Folders present, the one-file-per-folder
// store — read-only, when the list is made, and the default never picks one that cannot:
// in the R7 walk a profile with no Local Folders was the pre-selected one and the person
// learnt it only at Check. Three profiles: one with no Local Folders (Thunderbird opens it
// by default, so it was the old default), one ready, one with the maildir store. The first
// and third say why in the list's own words (no "nothing was restored", no "Check"), and
// keep their place and binding (Resolve still gives them; the plan refuses with its own
// words); the default is the ready one. Twin: with no ready profile, the default is as
// before, so the command line's plan still says W-NOLOCAL.
func TestTheProfileListSaysWhichProfilesCanTakeARestore(t *testing.T) {
	base := t.TempDir()
	makeProfile(t, filepath.Join(base, "Profiles", "a.nolocal"))
	good := makeProfile(t, filepath.Join(base, "Profiles", "b.good"), localPrefs()...)
	maildir := makeProfile(t, filepath.Join(base, "Profiles", "c.maildir"), append(localPrefs(),
		`user_pref("mail.server.server1.storeContractID", "@mozilla.org/msgstore/maildirstore;1");`)...)
	for _, p := range []string{good, maildir} {
		if err := os.MkdirAll(filepath.Join(p, "Mail", "Local Folders"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ini := strings.Join([]string{
		"[Profile0]", "Name=nolocal", "IsRelative=1", "Path=Profiles/a.nolocal", "",
		"[Profile1]", "Name=good", "IsRelative=1", "Path=Profiles/b.good", "",
		"[Profile2]", "Name=maildir", "IsRelative=1", "Path=Profiles/c.maildir", "",
		"[InstallAAAA]", "Default=Profiles/a.nolocal", "",
	}, "\n")
	writeFile(t, filepath.Join(base, ProfilesININame), ini)
	l, err := ReadProfiles([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	ps := assure.Reached(t, l.Profiles, "listed profiles")
	if len(ps) != 3 {
		t.Fatalf("want 3 profiles, got %+v", ps)
	}
	assure.Reached(t, ps[1].NotReady == "" && ps[1].Usable, "the ready profile: no reason")
	assure.Reached(t, ps[0].Usable && ps[0].NotReadyCode == "W-NOLOCAL" && strings.Contains(ps[0].NotReady, "no Local Folders yet") && strings.Contains(ps[0].NotReady, "Add Account"),
		"no Local Folders: said in the list, with what to do: "+ps[0].NotReady)
	assure.Reached(t, ps[2].Usable && ps[2].NotReadyCode == "W-MAILDIR" && strings.Contains(ps[2].NotReady, "maildir"), "the maildir store: said in the list: "+ps[2].NotReady)
	for _, p := range []Profile{ps[0], ps[2]} {
		if strings.Contains(p.NotReady, "nothing was restored") || strings.Contains(p.NotReady, "nothing was written") || strings.Contains(p.NotReady, "Check") {
			t.Errorf("a listed reason speaks of a restore that was not tried: %q", p.NotReady)
		}
	}
	pos, ok := l.Default(nil, "")
	assure.Reached(t, ok && pos == 1, "the default is the ready profile, not the one Thunderbird opens by default that has no Local Folders: "+itoa(pos))
	if _, err := l.Resolve(l.ID, 0); err != nil {
		t.Errorf("a not-ready profile keeps its place and binding: %v", err)
	}

	base2 := t.TempDir()
	makeProfile(t, filepath.Join(base2, "Profiles", "only"))
	writeFile(t, filepath.Join(base2, ProfilesININame), "[Profile0]\nName=only\nIsRelative=1\nPath=Profiles/only\n")
	l2, err := ReadProfiles([]string{base2})
	if err != nil {
		t.Fatal(err)
	}
	pos2, ok2 := l2.Default(nil, "")
	assure.Reached(t, ok2 && pos2 == 0 && l2.Profiles[0].NotReadyCode == "W-NOLOCAL", "twin: with no ready profile the default is as before (the plan then says W-NOLOCAL)")
}
