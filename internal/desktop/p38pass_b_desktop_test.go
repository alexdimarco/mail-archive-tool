package desktop

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// The adversarial pass over the P3-8 fixes, group B (keys and words), in the Desktop.

// covers: MA-854, R37
// A forget plan that asks for the path typed back accepts the path exactly as the plan
// shows it, even where the record's own spelling differs from it — the archive reached
// through a linked parent folder, and its folder since replaced by a link (case L)
// (the P3-8 pass, DESK-1). Another path is still refused, nothing forgotten.
func TestTheForgetPlanAcceptsThePathAsShown(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(base, "via")
	if err := os.Symlink(real, via); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	shown := recordedArchive(t, via, "Planted")
	moved := filepath.Join(real, "moved")
	if err := os.Rename(filepath.Join(real, "Planted"), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, filepath.Join(real, "Planted")); err != nil {
		t.Fatal(err)
	}
	e := newKeysEnv(t, shown)
	_, m := e.post(t, "api/encryption/forget/plan", map[string]any{"path": shown})
	plan := toText(map[string]any{"lines": m["lines"]})
	if m["strong"] != true || !strings.Contains(plan, "at "+shown+".") || m["spelling"] == encrecord.Spelling(shown) {
		t.Fatalf("want a strong plan showing %s, whose recorded spelling differs from it: %v\n%s", shown, m["spelling"], plan)
	}
	_, wrong := e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "typed": filepath.Join(base, "elsewhere")})
	recorded := func() bool { rec, _ := encrecord.Load(); _, ok := rec.Lookup(shown); return ok }
	assure.Refused(t, 1, toText(wrong), assure.Code(1), assure.Names("does not match", "nothing was changed"),
		assure.NoSideEffect(func() bool { return wrong["ok"] == false && recorded() }))

	_, m = e.post(t, "api/encryption/forget/plan", map[string]any{"path": shown})
	_, done := e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "typed": shown})
	assure.Reached(t, done["ok"] == true && !recorded(), "the path typed back exactly as shown forgets it: "+toText(done))
}

// covers: MA-854, R37
// An encrypted archive made at the command line is told how to add new mail in both
// forms it may have been made from — mail files and Microsoft 365 — since what it was
// made from is sealed inside it (the P3-8 pass, DESK-2), on its Weekly backup card too.
func TestACommandLineArchiveIsToldBothBackupForms(t *testing.T) {
	out := `/Volumes/Backup/Mail Archive`
	for _, goos := range []string{"darwin", "linux", "windows"} {
		how := encBackupHow(goos, out, false)
		mail := shellJoin(AddMailArgv("<the mail folder or file it was made from>", out), goos)
		graph := shellJoin(AddGraphArgv(out), goos)
		if !strings.Contains(how, mail) || !strings.Contains(how, graph) || !strings.Contains(how, "Microsoft 365") || !strings.Contains(how, "-tenant") {
			t.Fatalf("%s: both forms are not named: %s", goos, how)
		}
	}
	enc := backedUpEncrypted(t)
	e := newKeysEnv(t, enc)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	page := sessionPage(t, e.h)
	card := page[strings.Index(page, `id="backupEncrypted"`):]
	card = card[:strings.Index(card, "</p>")]
	assure.Reached(t, strings.Contains(card, template.HTMLEscapeString(shellJoin(AddGraphArgv(enc), e.cfg.goos()))),
		"the Weekly backup card names the Microsoft 365 form: "+card)
}

// covers: MA-854, R37
// An archive made in MailArchive Desktop — a backup job set up here — whose key files
// went missing cannot be unlocked here: its Weekly backup card does not say it "was made
// at the command line" beside an Archive now line that tells it to use Run now (the
// P3-8 pass, DESK-3). Positive twin: with its key files, unlocked, Run now is offered.
func TestAnArchiveThatCannotBeUnlockedIsNotSaidMadeAtTheCommandLine(t *testing.T) {
	out := backedUpEncrypted(t)
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out, box)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="runBackupBtn"`) {
		t.Fatal("positive twin: with its key files, Run now is offered")
	}
	e.cfg.Keys.lockAll(true)
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	page := sessionPage(t, e.h)
	i := strings.Index(page, `id="backupEncrypted"`)
	j := strings.Index(page, `id="archiveNowOff"`)
	if i < 0 || j < 0 {
		t.Fatalf("the Weekly backup card or the Archive now line is missing:\n%s", page)
	}
	card := page[i:]
	card = card[:strings.Index(card, "</p>")]
	line := page[j:]
	line = line[:strings.Index(line, "</p>")]
	assure.Reached(t, strings.Contains(line, "Run now") && !strings.Contains(card, "made at the command line") && !strings.Contains(card, "mailarchive -input"),
		"the two cards agree: "+card+"\n"+line)
}
