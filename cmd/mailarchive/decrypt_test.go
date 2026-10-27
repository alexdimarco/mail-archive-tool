package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// `mailarchive decrypt` (design-encrypted-archives-phase3 §10, K5, X14; P3-4 step 6).

var decryptDay = time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)

func refusedText(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

// covers: MA-823, R37
// At a terminal the plan is shown first — the new folder, its size bound — and the
// copy made only after "y" and the password: a plaintext archive in a new folder,
// <archive>-unencrypted-<date>. Answered "n", nothing is made and no key is asked.
func TestDecryptShowsThePlanThenMakesTheCopy(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	parent := t.TempDir()
	var stdout, stderr bytes.Buffer
	a := &scripted{isAttended: true, answers: []string{"n"}}
	err := decryptWith(a, out, parent, "", noKeyFlags(), decryptDay, &stdout, &stderr)
	entries, _ := os.ReadDir(parent)
	assure.Refused(t, 1, func() string { _, m := refusedText(err); return m }(), assure.Code(1), assure.Names("nothing was copied"),
		assure.NoSideEffect(func() bool { return len(entries) == 0 && len(a.prompts) == 1 }))

	stdout.Reset()
	a = &scripted{isAttended: true, answers: []string{"y", flowPassword}}
	if err := decryptWith(a, out, parent, "", noKeyFlags(), decryptDay, &stdout, &stderr); err != nil {
		t.Fatalf("the copy: %v\n%s", err, a.transcript())
	}
	dest := filepath.Join(parent, filepath.Base(out)+"-unencrypted-2026-10-27")
	plan := stdout.String()
	if !strings.Contains(plan, "UNENCRYPTED copy of "+out) || !strings.Contains(plan, "new folder: "+dest) || !strings.Contains(plan, "size: up to") {
		t.Fatalf("the plan shown:\n%s", plan)
	}
	_, err = os.Stat(filepath.Join(dest, ".mailarchive-manifest.json"))
	_, marker := os.Stat(filepath.Join(dest, app.CopyMarkerName))
	assure.Reached(t, err == nil && os.IsNotExist(marker) && strings.Contains(plan, "Unencrypted copy made at "+dest), "a finished plaintext archive in the new folder")
}

// covers: MA-823, R37
// A destination that may leave this computer (K5): at a terminal the new folder's
// path is typed back — a wrong one copies nothing; without a terminal it is refused
// before any key is asked. A local destination without a terminal, with
// -password-file, copies without a question.
func TestDecryptAsksTwiceWhereTheCopyMayLeave(t *testing.T) {
	flowRecord(t)
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")
	out := makeArchive(t)
	synced := filepath.Join(t.TempDir(), "Dropbox")
	os.Mkdir(synced, 0o700)
	var stdout, stderr bytes.Buffer
	a := &scripted{isAttended: true, answers: []string{"y", "/not/the/path"}}
	err := decryptWith(a, out, synced, "", noKeyFlags(), decryptDay, &stdout, &stderr)
	entries, _ := os.ReadDir(synced)
	code, msg := refusedText(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("does not match"),
		assure.NoSideEffect(func() bool { return len(entries) == 0 && strings.Contains(stdout.String(), "synced to Dropbox") }))

	dest := filepath.Join(synced, filepath.Base(out)+"-unencrypted-2026-10-27")
	a = &scripted{isAttended: true, answers: []string{"y", dest, flowPassword}}
	if err := decryptWith(a, out, synced, "", noKeyFlags(), decryptDay, &stdout, &stderr); err != nil {
		t.Fatalf("the path typed back: %v\n%s", err, a.transcript())
	}

	pw := filepath.Join(t.TempDir(), "pw")
	os.WriteFile(pw, []byte(flowPassword+"\n"), 0o600)
	k := noKeyFlags()
	k.passwordFile = &pw
	unattended := &scripted{}
	other := filepath.Join(t.TempDir(), "Dropbox")
	os.Mkdir(other, 0o700)
	err = decryptWith(unattended, out, other, "", k, decryptDay, &stdout, &stderr)
	entries, _ = os.ReadDir(other)
	code, msg = refusedText(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(other, "at a terminal"),
		assure.NoSideEffect(func() bool { return len(entries) == 0 && len(unattended.prompts) == 0 }))

	local := t.TempDir()
	if err := decryptWith(&scripted{}, out, local, "", k, decryptDay, &stdout, &stderr); err != nil {
		t.Fatalf("an unattended copy to a local folder with -password-file: %v", err)
	}
	entries, _ = os.ReadDir(local)
	assure.Reached(t, len(entries) == 1, "the copy was made without a question")
}

// covers: MA-823, R37
// decrypt refuses, before any key is asked, a source that is not an encrypted archive
// and a destination inside the archive; and it cannot be scheduled.
func TestDecryptRefusesBeforeTheKey(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	var stdout, stderr bytes.Buffer
	plain := t.TempDir()
	a := &scripted{isAttended: true}
	code, msg := refusedText(decryptWith(a, plain, t.TempDir(), "", noKeyFlags(), decryptDay, &stdout, &stderr))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(plain, "not an encrypted archive"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
	code, msg = refusedText(decryptWith(a, out, out, "", noKeyFlags(), decryptDay, &stdout, &stderr))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "encrypted archive"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
	_, err := parseJob([]string{"decrypt", "-out", out, "-dest", plain})
	code, msg = refusedText(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("decrypt", "cannot be scheduled"),
		assure.NoSideEffect(func() bool { return true }))
}
