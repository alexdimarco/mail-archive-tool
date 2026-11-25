package wizard

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// movedAwayProfile is a Thunderbird profile in a new home (HOME and USERPROFILE point at it) holding
// the IMAP account folders dirs under ImapMail/, each with a message; prefs writes its prefs.js, one
// account per {host, folder, user name}, each named by its user name.
func movedAwayProfile(t *testing.T, dirs ...string) (profile string, prefs func(accts ...[3]string)) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	profile = filepath.Join(home, ".thunderbird", "abcd1234.default-release")
	for _, d := range dirs {
		p := filepath.Join(profile, "ImapMail", d)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "INBOX"), []byte(mboxOf(d, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "INBOX.msf"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(profile, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile, func(accts ...[3]string) {
		t.Helper()
		var b strings.Builder
		var ids []string
		for i, a := range accts {
			id, key := "account"+string(rune('1'+i)), "server"+string(rune('1'+i))
			ids = append(ids, id)
			for _, l := range []string{
				`user_pref("mail.account.` + id + `.server", "` + key + `");`,
				`user_pref("mail.server.` + key + `.hostname", "` + a[0] + `");`,
				`user_pref("mail.server.` + key + `.type", "imap");`,
				`user_pref("mail.server.` + key + `.directory-rel", "[ProfD]ImapMail/` + a[1] + `");`,
				`user_pref("mail.server.` + key + `.name", "` + a[2] + `");`,
				`user_pref("mail.server.` + key + `.userName", "` + a[2] + `");`,
			} {
				b.WriteString(l + "\n")
			}
		}
		b.WriteString(`user_pref("mail.accountmanager.accounts", "` + strings.Join(ids, ",") + `");` + "\n")
		if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// covers: MA-1494, MA-1495, MA-1504, R12
// CON-06 (docs/design-per-mailbox-archives.md revision 3): two accounts on one mail server have a
// set; the first is removed in Thunderbird with its folder kept and added again, kept at "-2". The
// list shows the old folder as an ordinary row (two live accounts share its server, so it is not
// certain no account uses it), but a run of its archive would stop (C2). Before continuing a child,
// the plan asks whether its folder has no live owner and its account moved (by the identity its
// record kept, else by its name): if so it plans "kept as it is … Click Pause on it", never "Brought
// up to date", so no "run this again" follows. Positive twin first: before the re-add, the same plan
// brings that archive up to date. The same holds for a record with no identity (by the name).
func TestAFolderThunderbirdNoLongerUsesIsKeptNotContinued(t *testing.T) {
	ownSettings(t)
	const host = "imap.qzg.example"
	profile, prefs := movedAwayProfile(t, "imap.qzg.example", "imap.qzg-1.example", "imap.qzg-2.example")
	old, second := filepath.Join(profile, "ImapMail", "imap.qzg.example"), filepath.Join(profile, "ImapMail", "imap.qzg-1.example")
	alice := [3]string{host, "imap.qzg.example", "qzalice@qzg.example"}
	bob := [3]string{host, "imap.qzg-1.example", "qzbob@qzg.example"}
	prefs(alice, bob)
	parent := t.TempDir()
	child := filepath.Join(parent, "qzalice@qzg.example")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{old}, Out: child, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(child); len(lr.ThunderbirdIDs) != 1 {
		t.Fatalf("the child's record must keep its folder's account identity: %+v", lr.ThunderbirdIDs)
	}
	rows := []setRow{
		{path: old, name: "qzalice@qzg.example", shown: "qzalice@qzg.example", make: true},
		{path: second, name: "qzbob@qzg.example", shown: "qzbob@qzg.example", make: true},
	}
	// Restated 2026-11-25 (the re-verify of rounds A and B, RF-03, its words; required whole, as before):
	// "qzalice@qzg.example: kept as it is in qzalice@qzg.example; Thunderbird no longer uses this folder.
	// Click Pause on it to stop its backups." before, where "this folder" read as the archive's folder
	// and Pause had no place. And RF-07: "Brought up to date: FOLDER" is now "To bring up to date:
	// FOLDER", still forbidden for this archive below.
	const keptLine = "qzalice@qzg.example: kept as it is in qzalice@qzg.example, since Thunderbird no longer uses that mailbox's folder. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."

	// Positive twin: alice still owns her folder: brought up to date.
	p, err := planSet(parent, rows, runtime.GOOS)
	if got := outcomesOf(p); err != nil || got[0] != "qzalice@qzg.example: continue qzalice@qzg.example" {
		t.Fatalf("positive twin: the archive of a folder its account still uses is brought up to date: %v %q", err, got)
	}

	// Removed with its folder kept and added again: Thunderbird keeps it at "-2".
	again := alice
	again[1] = "imap.qzg-2.example"
	prefs(bob, again)
	p, err = planSet(parent, rows, runtime.GOOS)
	text := setPlanText(p)
	if got := outcomesOf(p); err != nil || got[0] != "qzalice@qzg.example: kept qzalice@qzg.example exact=true" ||
		!strings.Contains(text, keptLine) || strings.Contains(text, "Brought up to date: qzalice@qzg.example") || strings.Contains(text, setUpToDateLead+"qzalice@qzg.example") {
		t.Errorf("CON-06: the archive whose run would stop is kept as it is, with Pause advised: %v %q\n%s", err, got, text)
	}
	if got := outcomesOf(p); got[1] != "qzbob@qzg.example: new qzbob@qzg.example" {
		t.Errorf("the other mailbox is planned as before: %q", got)
	}

	// A record with no identity: decided by its name, the same.
	lr, _, _ := state.ReadLastRun(child)
	lr.ThunderbirdIDs = nil
	if err := state.WriteLastRun(child, lr); err != nil {
		t.Fatal(err)
	}
	if p, err = planSet(parent, rows, runtime.GOOS); err != nil || !strings.Contains(setPlanText(p), keptLine) {
		t.Errorf("CON-06 by the name, for a record with no identity: %v %q", err, outcomesOf(p))
	}
}
