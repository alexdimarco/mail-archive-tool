package health

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-730, R37
// Status on an encrypted archive reads only what is keyless: its manifest, report,
// index and history are not read (here each is garbage that would RED a plaintext
// archive), the report says it is encrypted, and a failed last run is still RED.
// The positive twin: the same garbage without a vault is read and RED.
func TestStatusOnAnEncryptedArchiveReadsOnlyWhatIsKeyless(t *testing.T) {
	garbage := func(t *testing.T) string {
		out := t.TempDir()
		for _, name := range []string{".mailarchive-manifest.json", "search.db"} {
			if err := os.WriteFile(filepath.Join(out, name), []byte("not what it claims"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		started := time.Now().Add(-time.Hour).UTC()
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: started, Finished: started.Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	plain := garbage(t)
	if r := Assess(Gather(plain, ""), time.Now()); r.Posture != "RED" || !slices.Contains(r.Codes, "manifest_unreadable") {
		t.Fatalf("the positive twin: a garbage manifest must RED a plaintext archive, got %s %v", r.Posture, r.Codes)
	}

	sealed := garbage(t)
	p, err := vault.PrepareCreate(sealed, "status test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	in := Gather(sealed, "")
	if !in.Encrypted || in.EncryptedWhy == "" || in.HasManifestFile || in.HasIndexFile || in.ReportExists {
		t.Fatalf("status read a sealed archive's files: %+v", in)
	}
	r := Assess(in, time.Now())
	if r.Posture == "RED" || !slices.Contains(r.Codes, "encrypted") {
		t.Fatalf("an encrypted archive must say so and not RED on its sealed files, got %s %v %v", r.Posture, r.Codes, r.Reasons)
	}

	in.LastRun.Status = state.RunFailed
	if r := Assess(in, time.Now()); r.Posture != "RED" || !slices.Contains(r.Codes, "last_run_failed") {
		t.Fatalf("a failed last run of an encrypted archive must still RED, got %s %v", r.Posture, r.Codes)
	}
}
