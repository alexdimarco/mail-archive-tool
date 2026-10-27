package health

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
)

// P3-8's friction fixes in status and the Desktop's health line (MA-851).

// covers: MA-851, R37
// status on a folder this computer recorded as an encrypted archive, whose drive is
// unplugged, reports on it — never "run an export into it first" — leading with "is
// the drive plugged in?" in the case's own words, said once (no second "not
// reachable" line beside it). The positive twin: a folder nobody recorded, gone, is
// still "no archive" to status.
func TestStatusOfAGoneRecordedArchiveAsksForTheDrive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "archive")
	plain := Gather(out, "")
	if err := NoArchive(plain); err == nil || !strings.Contains(err.Error(), "run an export into it first") {
		t.Fatalf("positive twin: a gone folder nobody recorded is no archive: %v", err)
	}

	id := "0123456789abcdef0123456789abcdef"
	encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, out, false, time.Now()) })
	in := Gather(out, "")
	if err := NoArchive(in); err != nil {
		t.Fatalf("a recorded archive gone is reported on, not refused: %v", err)
	}
	r := Assess(in, time.Now())
	text := strings.Join(Summary(in, r), "\n")
	assure.Reached(t, r.Posture == "RED" && len(r.Reasons) > 0 && strings.HasPrefix(r.Reasons[0], "RED: Is the drive plugged in?") &&
		strings.Contains(text, "forget-archive") && !strings.Contains(text, "run an export into it first") &&
		!slices.Contains(r.Codes, "out_absent") && strings.Count(text, "Is the drive plugged in?") == 1 && !strings.Contains(text, "not reachable"),
		"the drive question first, in the case's words, once: "+text)
}

// covers: MA-851, R37
// The status and Desktop health line of an encrypted archive says what it is in
// plain words — "it holds an encrypted archive's key file" — and never names an
// internal file (X10). Its key file is still what is named.
func TestTheHealthLineNamesNoInternalFile(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := t.TempDir()
	sealFolder(t, out)
	in := Gather(out, "")
	r := Assess(in, time.Now())
	text := strings.Join(Summary(in, r), "\n")
	if !in.Encrypted || !slices.Contains(r.Codes, "encrypted") {
		t.Fatalf("an encrypted archive must say so: %v", r.Codes)
	}
	if strings.Contains(text, ".mailarchive") || !strings.Contains(text, "it holds an encrypted archive's key file") {
		t.Fatalf("the health line names an internal file, or not the key file:\n%s", text)
	}
	for why, want := range map[string]string{
		"it holds .mailarchive-manifest.ptr, which only an encrypted archive has": "it holds an encrypted archive's sealed files",
		"its manifest says it is encrypted":                                       "it holds an encrypted archive's sealed files",
		"it is an encrypted archive this computer made":                           "it is an encrypted archive this computer made",
	} {
		if got := plainEncryptedWhy(why); got != want {
			t.Fatalf("%q worded %q, want %q", why, got, want)
		}
	}
}
