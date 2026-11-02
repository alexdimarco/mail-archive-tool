package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
)

// started is a deletions.log started line with no finished line: a delete of folder, of kind, that
// did not finish (its folder gone).
func started(run, folder, kind string) map[string]any {
	return map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": run, "folder": folder, "row": folder, "kind": kind}
}

// covers: MA-1041, R36
// With several deletes not finished, uninstall's refusal is plural and says each folder and the
// command that finishes it on a line of its own; the sentence that an encrypted archive's key stays
// remembered is said only when one of them is of an encrypted archive (the 1.5.3 re-walk's N9, the
// operator's delegate's decision: "the delete of A; B; C is finished … which that delete needs",
// all the commands in one bracket, and the key's sentence for plain archives). Positive twin first:
// one plain delete keeps the one-delete words, with no key sentence.
func TestUninstallsRefusalOfSeveralDeletesIsPlural(t *testing.T) {
	noSchedulesNoSignIn(t)
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
	a, b, c := filepath.Join(t.TempDir(), "Old A"), filepath.Join(t.TempDir(), "Old B"), filepath.Join(t.TempDir(), "Old C")
	plan := func(lines ...map[string]any) (*DeleteNotFinishedError, string) {
		t.Helper()
		log := logLines(t, cfg, lines...)
		before, _ := os.ReadFile(log)
		rep, err := Uninstall(cfg, nil, "", true)
		var dn *DeleteNotFinishedError
		if !errors.As(err, &dn) || rep == nil {
			t.Fatalf("the plan should come with the refusal: %v", err)
		}
		if after, _ := os.ReadFile(log); string(after) != string(before) || !exists(filepath.Join(cfg, "desktop-settings.json")) {
			t.Fatal("the plan changed something")
		}
		return dn, log
	}
	finish := func(f string) string { return "mailarchive delete-archive -out " + encmark.CommandPath(f) }
	const keySentence = "this computer then still remembers its key"

	// Positive twin: one plain delete.
	dn, log := plan(started("r1", a, "plain"))
	if u := dn.Until(); !strings.HasPrefix(u, "Uninstall will refuse until the delete of "+a+" is finished (it is running or did not finish): ") ||
		!strings.Contains(u, finish(a)) || strings.Contains(u, keySentence) || strings.Contains(u, "\n") ||
		!strings.Contains(u, "MailArchive's deletion log ("+encmark.CommandPath(log)+"), which that delete needs") {
		t.Fatalf("one plain delete: %q", u)
	}
	if e := dn.Error(); !strings.Contains(e, "MailArchive's deletion log ("+encmark.CommandPath(log)+"), which that delete needs") {
		t.Fatalf("one plain delete's refusal: %q", e)
	}
	if dn.Waits() != "Uninstall waits until that delete is finished." {
		t.Fatalf("one delete's tooltip: %q", dn.Waits())
	}

	// Three plain deletes: plural, a line each, no key sentence.
	dn, log = plan(started("r1", a, "plain"), started("r2", b, "plain"), started("r3", c, "plain"))
	// The log's path quoted, as -yes's refusal quotes it (the settle pass's LOW-4).
	want := "Uninstall will refuse until these 3 deletes are finished (each is running or did not finish): uninstalling would remove MailArchive's deletion log (" + encmark.CommandPath(log) + "), which they need to finish.\n" +
		"  - " + a + ": " + finish(a) + "\n" +
		"  - " + b + ": " + finish(b) + "\n" +
		"  - " + c + ": " + finish(c) + "\n" +
		"Wait for their results, or finish each one (Delete… on its row in MailArchive Desktop, or the command beside it above), then uninstall again. If you no longer want to finish them, copy deletions.log somewhere you keep records, delete it, then uninstall again."
	if got := dn.Until(); got != want {
		t.Fatalf("three plain deletes:\n%s\nwant:\n%s", got, want)
	}
	if dn.Waits() != "Uninstall waits until those deletes are finished." {
		t.Fatalf("several deletes' tooltip: %q", dn.Waits())
	}
	// The real run says the same, as a refusal.
	_, err := Uninstall(cfg, nil, "", false)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assure.Refused(t, refusalCode(err), msg, assure.Code(1),
		assure.Names("Deletes of these 3 folders are running or did not finish, so uninstall removed nothing", "MailArchive's deletion log ("+encmark.CommandPath(log)+"), which they need", "\n  - "+b+": "+finish(b)+"\n", "If you no longer want to finish them"),
		assure.Forbid(keySentence, "; mailarchive delete-archive"),
		assure.NoSideEffect(func() bool { return exists(log) && exists(filepath.Join(cfg, "desktop-settings.json")) }))

	// One of them encrypted: the key sentence, once, for "an encrypted archive among them".
	dn, _ = plan(started("r1", a, "plain"), started("r2", b, "encrypted"))
	if u := dn.Until(); !strings.HasSuffix(u, "then uninstall again; for an encrypted archive among them, this computer then still remembers its key until you use Forget this encrypted archive here, or Forget every encrypted archive on this computer.") ||
		strings.Count(u, keySentence) != 1 {
		t.Fatalf("with an encrypted archive: %q", u)
	}
	// One encrypted alone keeps the one-delete key sentence.
	dn, _ = plan(started("r1", b, "encrypted"))
	if u := dn.Until(); !strings.HasSuffix(u, "then uninstall again; for an encrypted archive, this computer then still remembers its key until you use Forget this encrypted archive here, or Forget every encrypted archive on this computer.") {
		t.Fatalf("one encrypted delete: %q", u)
	}
}

// covers: MA-1041, R36
// uninstall -yes's refusal quotes the deletion log's path, so it can be pasted into a copy command
// (the 1.5.3 re-walk's N10: on a Mac it holds a space, "Application Support"). Positive twin first:
// with -log-copied nothing is refused.
func TestTheLogNotCopiedRefusalQuotesThePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Application Support", "mailarchive")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	log := logLines(t, dir, started("r1", filepath.Join(dir, "x"), "plain"))
	if err := CheckLogCopied(log, true); err != nil {
		t.Fatalf("the twin: %v", err)
	}
	err := CheckLogCopied(log, false)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	var nc *LogNotCopiedError
	code := 0
	if errors.As(err, &nc) {
		code = 1
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("MailArchive's deletion log, "+encmark.CommandPath(log)+", lists the deletes made on this computer", "then run mailarchive uninstall -yes -log-copied."),
		assure.NoSideEffect(func() bool { return exists(log) }))
	kept := (&LogNotCopiedError{Log: log, Kept: true}).Error()
	if !strings.HasPrefix(kept, "MailArchive's deletion log, "+encmark.CommandPath(log)+", now lists") {
		t.Fatalf("the kept refusal: %q", kept)
	}
}
