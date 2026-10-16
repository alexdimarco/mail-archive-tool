package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-422, R29, S52
// Recipient verification without the tool, in Go on every OS: the coreutils-format
// check (two spaces, forward slashes, lowercase hex, sorted: sumsCheck parses it
// strictly) passes on a fresh export, fails naming the file after a byte flip, and
// its extra-files check reports a planted file; README.txt names all three verify
// forms, the extra-files check and the fingerprint step, and repeats the run id on its
// first line (the content nonce); and the fingerprint the ledger, the run record's
// sealed line and the journal's end carry is the SHA-256 of SHA256SUMS.txt, computed
// from the in-memory bytes.
func TestCopyExportRecipientVerification(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(4, "Inbox", "Clients")), src)
	root := res.Root

	if bad, missing, extra := sumsCheck(t, root); len(bad)+len(missing)+len(extra) != 0 {
		t.Fatalf("a fresh export fails its check: %v %v %v", bad, missing, extra)
	}
	readme := readText(t, root, copyReadmeName)
	if first, _, _ := strings.Cut(readme, "\n"); first != copyRunLinePrefix+res.RunID {
		t.Errorf("README's first line is %q, want the run id line", first)
	}
	for _, need := range []string{
		"sha256sum -c SHA256SUMS.txt", "shasum -a 256 -c SHA256SUMS.txt", "Get-FileHash -Algorithm SHA256", `"OK: $n files checked"`,
		"comm -23", "Compare-Object", "sha256sum SHA256SUMS.txt", "shasum -a 256 SHA256SUMS.txt", "Get-FileHash SHA256SUMS.txt",
		"does not detect files added", "in a terminal", "Status: complete — valid only while no " + copyMarkerName,
	} {
		if !strings.Contains(strings.ReplaceAll(readme, "Open a terminal", "in a terminal"), need) {
			t.Errorf("README does not say %q", need)
		}
	}

	raw := []byte(readText(t, root, copySumsName))
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:])
	if res.Fingerprint != want {
		t.Errorf("the result's fingerprint %s is not sha256(SHA256SUMS.txt) %s", res.Fingerprint, want)
	}
	ledger, err := e.readLedger()
	if err != nil || len(ledger) != 1 || ledger[0].Run != res.RunID || ledger[0].Fingerprint != want || ledger[0].Kind != "sealed" {
		t.Errorf("the ledger = %+v (%v), want one sealed line carrying %s", ledger, err, want)
	}
	sealed := assure.Reached(t, mustSummary(t, e, res.RunID).Sealed, "the run record's sealed line")
	if sealed.Fingerprint != want {
		t.Errorf("the sealed line's fingerprint is %s, want %s", sealed.Fingerprint, want)
	}
	var end *ceJournalLine
	lines, _ := e.readJournal()
	for i := range lines {
		if lines[i].Event == "end" {
			end = &lines[i]
		}
	}
	if end == nil || end.Fingerprint != want {
		t.Errorf("the journal end = %+v, want fingerprint %s", end, want)
	}

	// A flipped byte is named; a planted file is reported.
	var victim string
	for p := range treeFiles(t, root) {
		if strings.HasPrefix(p, "messages/") {
			victim = p
			break
		}
	}
	full := filepath.Join(root, filepath.FromSlash(victim))
	orig, _ := os.ReadFile(full)
	os.WriteFile(full, append([]byte("X"), orig[1:]...), 0o600)
	if bad, _, _ := sumsCheck(t, root); len(bad) != 1 || bad[0] != victim {
		t.Errorf("a byte flip in %s was reported as %v", victim, bad)
	}
	os.WriteFile(full, orig, 0o600)
	os.WriteFile(filepath.Join(root, "extra.txt"), []byte("x"), 0o600)
	if _, _, extra := sumsCheck(t, root); len(extra) != 1 || extra[0] != "extra.txt" {
		t.Errorf("a planted file was reported as %v", extra)
	}
}

// mustSummary reads a run's record.
func mustSummary(t *testing.T, e *CopyExportEnv, run string) *ceRecSummary {
	t.Helper()
	s, err := readSummary(e.recordPath(run))
	if err != nil {
		t.Fatalf("the run record of %s: %v", run, err)
	}
	return s
}
