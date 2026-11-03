package main

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
)

// ma1048LastVerify is the archive's last-verify record's bytes, "" when there is none.
func ma1048LastVerify(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".mailarchive-lastverify.json"))
	if err != nil {
		return ""
	}
	return string(b)
}

// covers: MA-1048, R36
// `mailarchive verify` asks archdelete's one question, as the other actions do (the slice-2 pass's
// LOW-2, its verify part): plain, with -record, and as a scheduled verify (-unattended with its log
// inside the folder), a folder holding the delete marker is refused in the marker-only words with
// "it was not checked", the folder named and the terminal's way to finish it — nothing written:
// the last-verify record is unchanged and no log is made. A folder this computer's log says a
// delete of did not finish is refused in MA-1015's words. Run as the real program in this
// package's throwaway home. Positive twin first: the folder beside it is checked and records it.
func TestMA1048_VerifyRefusesAFolderHoldingTheMarker(t *testing.T) {
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	half, other, logged := filepath.Join(t.TempDir(), "Half deleted"), filepath.Join(t.TempDir(), "Other"), filepath.Join(t.TempDir(), "Logged")
	for _, d := range []string{half, other, logged} {
		if code, stderr := runCLI("-out", d, "-input", src); code != 0 {
			t.Fatalf("the archive %s: exit %d %s", d, code, stderr)
		}
	}
	if code, stderr := runCLI("verify", "-out", half); code != 0 || ma1048LastVerify(half) == "" {
		t.Fatalf("the earlier verify: exit %d %s", code, stderr)
	}
	ma1048Plant(t, half)
	ma1044Started(t, logPath, logged)

	// Positive twin.
	if code, stderr := runCLI("verify", "-out", other); code != 0 || ma1048LastVerify(other) == "" {
		t.Fatalf("positive twin: exit %d %s", code, stderr)
	}

	before := ma1044Files(t, half)
	unchanged := func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }
	names := assure.Names(delmark.OnlyMarkerWords("it was not checked")+" (Folder: "+half+")", "To finish it at this terminal: mailarchive delete-archive -out "+encmark.CommandPath(half))
	for _, args := range [][]string{
		{"verify", "-out", half},
		{"verify", "-record", "-out", half},
		{"verify", "-out", half, "-unattended", "-log", filepath.Join(half, "mailarchive-verify.log")},
	} {
		code, stderr := runCLI(args...)
		assure.Refused(t, code, stderr, assure.Code(1), names, assure.Forbid("Delete again"), assure.NoSideEffect(unchanged))
	}

	lb := ma1044Files(t, logged)
	code, stderr := runCLI("verify", "-out", logged)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not checked; nothing was changed. (Folder: "+logged+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(lb, ma1044Files(t, logged)) }))
}

// covers: MA-1048, R37
// `verify -full` (the check with the key) and an encrypted archive's keyless verify refuse a folder
// holding the delete marker before any key is asked, in the marker-only words with "it was not
// checked" and the folder — its last-verify record unchanged. Positive twin first: the same
// archive without the marker is checked with its key.
func TestMA1048_VerifyFullRefusesAFolderHoldingTheMarker(t *testing.T) {
	ma1044PackageLog(t)
	out, pwFile := backedUpArchive(t)
	if _, err := stdout(t, func() error { return runVerify([]string{"-out", out, "-full", "-password-file", pwFile}) }); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	ma1048Plant(t, out)
	lv := ma1048LastVerify(out)
	same := func() bool { return ma1048LastVerify(out) == lv }
	words := delmark.OnlyMarkerWords("it was not checked") + " (Folder: " + out + ")"
	for _, args := range [][]string{{"-out", out, "-full", "-password-file", pwFile}, {"-out", out}} {
		code, msg := refused(runVerify(args))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(words), assure.NoSideEffect(same))
	}
}
