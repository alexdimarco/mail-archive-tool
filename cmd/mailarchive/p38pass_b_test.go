package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The adversarial pass over the P3-8 fixes, group B (keys and words), at the command
// line.

// rememberKey opens out with its password and remembers its key in this test's store,
// returning the archive's id.
func rememberKey(t *testing.T, out string) string {
	t.Helper()
	store, err := secretStore()
	if err != nil {
		t.Fatal(err)
	}
	u, err := keyring.Open(out, keyring.Password(flowPassword), keyring.OpenOptions{Write: true, Remember: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	if !keyring.HasRemembered(out, store) {
		t.Fatal("the key was not remembered")
	}
	return u.ArchiveID().String()
}

// covers: MA-854, R37
// restore-key-file -remembered is refused before any question only when the restore
// would refuse: an archive whose record entry is unproven (as repair-record leaves it),
// both key files gone, and whose key this computer remembers, is rebuilt (the P3-8
// pass, ORDER-1). TestRestoreFromTheRememberedKeyChecksFirst keeps the refusals.
func TestTheRememberedKeyRebuildsAnUnprovenArchive(t *testing.T) {
	out, _ := backedUpArchive(t)
	id := rememberKey(t, out)
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error {
		delete(r.Archives, id)
		return r.Add(id, out, true, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	a := &scripted{isAttended: true, answers: []string{"y", "", "a rebuilt cli passphrase", "a rebuilt cli passphrase", keyAnswer}}
	err := restoreKeyFile(a, out, "", noKeyFlags())
	assure.Reached(t, err == nil && exists(filepath.Join(out, vault.FileName)) && !strings.Contains(a.transcript(), "does not remember"),
		"the key files rebuilt from the remembered key")
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
}

// covers: MA-854, R37
// forget-key on a remembered key the store holds but will not read — its file readable
// by other users — forgets it and says so, never "nothing to forget" (the P3-8 pass,
// F1). Positive twin: a second run, with nothing left, says there was nothing.
func TestForgetKeyForgetsAKeyTheStoreWillNotRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the file store's mode-bit refusal is not how Windows guards a secret file")
	}
	out, _ := backedUpArchive(t)
	rememberKey(t, out)
	store, _ := secretStore()
	dir := store.(interface{ Location() string }).Location()
	files, _ := filepath.Glob(filepath.Join(dir, "*.secret"))
	if len(files) != 1 {
		t.Fatalf("want one stored key file, got %v", files)
	}
	if err := os.Chmod(files[0], 0o644); err != nil {
		t.Fatal(err)
	}
	a := &scripted{isAttended: true}
	err := forgetKey(a, out)
	tr := a.transcript()
	assure.Reached(t, err == nil && !exists(files[0]) && strings.Contains(tr, "could not read") && strings.Contains(tr, "no longer remembers") &&
		!strings.Contains(tr, "nothing to forget"), "the unreadable key forgotten, and said so: "+tr)

	a = &scripted{isAttended: true}
	err = forgetKey(a, out)
	assure.Reached(t, err == nil && strings.Contains(a.transcript(), "nothing to forget"), "positive twin: nothing left: "+a.transcript())
}

// covers: MA-854, R37
// forget-archive's strong confirmation accepts the folder typed back exactly as it is
// shown, where the record's own spelling differs: reached through a linked parent
// folder, the folder since replaced by a link (the P3-8 pass, ORDER-2). Another path
// is still refused, nothing forgotten.
func TestForgetArchiveAcceptsThePathAsShown(t *testing.T) {
	flowRecord(t)
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(base, "via")
	if err := os.Symlink(real, via); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	shown := filepath.Join(via, "archive")
	c := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", keyAnswer}}
	u, err := createEncrypted(c, shown, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, c.transcript())
	}
	u.Destroy()
	moved := filepath.Join(real, "moved")
	if err := os.Rename(filepath.Join(real, "archive"), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, filepath.Join(real, "archive")); err != nil {
		t.Fatal(err)
	}
	if p, err := keyring.PlanForget(shown, nil); err != nil || !p.Strong || encrecord.Names(shown, p.Spelling) {
		t.Fatalf("want a strong plan whose recorded spelling differs from %s: %+v %v", shown, p, err)
	}

	a := &scripted{isAttended: true, answers: []string{filepath.Join(base, "elsewhere"), "n"}}
	code, msg := refused(forgetArchive(a, shown))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("does not match", "nothing was changed"),
		assure.NoSideEffect(func() bool { return recordedAt(t, shown) != "" }))

	a = &scripted{isAttended: true, answers: []string{shown, "n"}}
	err = forgetArchive(a, shown)
	assure.Reached(t, err == nil && strings.Contains(a.transcript(), "\n    "+shown) && recordedAt(t, shown) == "",
		"the path as shown, typed back, forgets it: "+a.transcript())
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
}

// covers: MA-854, R37
// Every command printed for a person to type that names an encrypted archive's folder
// quotes it with encmark.CommandPath, never Go's %q, which doubles each backslash of a
// Windows network folder so the command cannot find it (the P3-8 pass, F2). The
// lines are the P3-8 remedies and the stopped-verify lines; a file is read as source,
// since %q and CommandPath print the same off Windows.
func TestPrintedCommandsQuoteTheFolderForTheTerminal(t *testing.T) {
	quoted := regexp.MustCompile(`-out %q`)
	for file, markers := range map[string][]string{
		"../../internal/app/verifyfull.go": {"-mode full"},
		"../../internal/app/encrypted.go":  {"verify -full -out"},
		"../../internal/health/health.go":  {"restore-key-file -out"},
		"keyverbs.go":                      {"restore-key-file -out"},
		"main.go":                          {"mailarchive verify -out", "mailarchive verify -full -out"},
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, line := range strings.Split(string(b), "\n") {
			for _, m := range markers {
				if strings.Contains(line, m) {
					found++
					if quoted.MatchString(line) {
						t.Errorf("%s prints a folder with %%q: %s", file, strings.TrimSpace(line))
					}
				}
			}
		}
		assure.Reached(t, found, file+": the printed commands are there to check")
	}
}
