package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// firstArchivedHTML returns the path of the first archived message page under
// out (a store/folder .html, never a folder index.html).
func firstArchivedHTML(t *testing.T, out string) string {
	t.Helper()
	var found string
	filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".html") && d.Name() != "index.html" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatal("no archived html found")
	}
	return found
}

// covers: MA-154, R18, R5, S31
// verify persists a SEPARATE .mailarchive-lastverify.json record: a healthy
// verify finalizes it "done" with the verdict (attested + counts + exit code);
// a refusal before the check commits (a locked archive) writes no record; and
// verify never writes the export last-run record. Positive twin: the healthy
// verify records a finalized, attested verdict.
func TestVerifyWritesLastVerifyRecord(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t, out, fixtureMsg{subject: "one"}, fixtureMsg{subject: "two"})

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	lv, st, err := state.ReadLastVerify(out)
	if err != nil || st != state.LastVerifyPresent {
		t.Fatalf("no last-verify record after a verify: state=%v err=%v", st, err)
	}
	if lv.Status != state.VerifyDone || lv.Finished == nil {
		t.Errorf("record not finalized: %+v", lv)
	}
	if !lv.Attested || lv.ExitCode != 0 || lv.Records != rep.Records || lv.Modified != 0 {
		t.Errorf("attested record wrong: %+v (rep %+v)", lv, rep)
	}
	// verify writes no EXPORT last-run record.
	if _, lrst, _ := state.ReadLastRun(out); lrst != state.LastRunAbsent {
		t.Errorf("verify wrote an export last-run record (state=%v)", lrst)
	}

	// A lock refusal (before the check commits) writes NO last-verify record.
	out2 := tmpDir(t)
	buildFixityArchive(t, out2, fixtureMsg{subject: "x"})
	held, err := lockfile.AcquireAs(filepath.Join(out2, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	if _, verr := Verify(out2, VerifyOptions{}, discard(), nil); verr == nil {
		t.Fatal("verify should refuse a locked archive")
	}
	held.Release()
	if _, st2, _ := state.ReadLastVerify(out2); st2 != state.LastVerifyAbsent {
		t.Errorf("a lock refusal wrote a last-verify record (state=%v)", st2)
	}
}

// covers: MA-156, R18, S31
// A verify that finds modified/missing files writes ARCHIVE-INTEGRITY-ATTENTION.txt
// naming the archive, the counts and the restore/re-verify remedy; a following
// attested verify removes it. Positive twin: a clean verify writes no sidecar.
func TestVerifyIntegritySidecar(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t, out, fixtureMsg{subject: "one"}, fixtureMsg{subject: "two"})
	sidecar := filepath.Join(out, state.IntegrityAttentionName)

	// A clean verify writes no integrity sidecar.
	if _, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(sidecar); !os.IsNotExist(serr) {
		t.Fatalf("a clean verify wrote %s", state.IntegrityAttentionName)
	}

	// Damage a file → a not-attested verify writes the sidecar.
	victim := firstArchivedHTML(t, out)
	orig, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	damaged := append([]byte(nil), orig...)
	damaged[len(damaged)/2] ^= 0xff
	if err := os.WriteFile(victim, damaged, 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Attested() || rep.Modified == 0 {
		t.Fatalf("expected a modified, not-attested verify: %+v", rep)
	}
	body, serr := os.ReadFile(sidecar)
	if serr != nil {
		t.Fatalf("a not-attested verify did not write %s: %v", state.IntegrityAttentionName, serr)
	}
	for _, want := range []string{out, "modified", "mailarchive verify -out", "Restore"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("%s lacks %q:\n%s", state.IntegrityAttentionName, want, body)
		}
	}

	// Restore the bytes → an attested verify removes the sidecar.
	if err := os.WriteFile(victim, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Attested() {
		t.Fatalf("archive not attested after restore: %+v", after)
	}
	if _, serr := os.Stat(sidecar); !os.IsNotExist(serr) {
		t.Errorf("an attested verify did not remove %s", state.IntegrityAttentionName)
	}
}
