package encmark

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1053, R37
// The delete marker is one of the reserved names (§15.7, MA-1053): a folder this
// computer recorded as encrypted that holds it beside its archive is still its own
// archive (case None), not "now holds other files" (case Other) — the words 1.5.4 and
// before give (§15.6). Once the delete has removed the vault and the sealed data, the
// marker beside litter reads as the archive gone (case Gone), never as tampered.
// Positive twin first: another MailArchive-shaped name is still other files.
func TestARecordedEncryptedArchiveHoldingTheDeleteMarkerIsStillItsOwn(t *testing.T) {
	out, id := archive(t)
	os.WriteFile(filepath.Join(out, ".mailarchive-something-new.json"), []byte("x"), 0o600)
	if r, _ := At(out); r.Case != Other {
		t.Fatalf("the positive twin: a name no encrypted archive writes is other files: %+v", r)
	}

	out, id = archive(t)
	if err := os.WriteFile(filepath.Join(out, delmark.Name), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := At(out)
	if err != nil || !r.Encrypted || !r.Recorded || r.ID != id || r.Case != None {
		t.Fatalf("a recorded archive holding the delete marker: %+v %v", r, err)
	}
	if !isReserved(delmark.Name) || !slices.Contains(ReservedNames(), delmark.Name) {
		t.Fatal("the delete marker is not a reserved name")
	}

	// The vault and the sealed data removed, the marker left beside litter.
	strip(t, out)
	os.WriteFile(filepath.Join(out, delmark.Name), nil, 0o600)
	os.WriteFile(filepath.Join(out, ".DS_Store"), nil, 0o600)
	if r, _ := At(out); r.Case != Gone || !r.Encrypted {
		t.Fatalf("only the marker and litter: %+v", r)
	}
}

// covers: MA-1053, R37
// The marker is never an encrypted signal (§15.7, MA-1053): a plain archive's folder
// holding it stays plain — vault.EncryptedMarker and Here say so — and so does an empty
// folder holding only it. Positive twin first: a plain archive without it is plain.
func TestTheDeleteMarkerIsNeverAnEncryptedSignal(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
	os.WriteFile(filepath.Join(plain, state.LastRunName), []byte("{}"), 0o600)
	if enc, why, err := Here(plain); enc || err != nil {
		t.Fatalf("the positive twin: a plain archive reads %v %q %v", enc, why, err)
	}
	only := t.TempDir()
	for _, dir := range []string{plain, only} {
		if err := os.WriteFile(filepath.Join(dir, delmark.Name), []byte(`{"what":"MailArchive delete marker","version":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if enc, why, err := vault.EncryptedMarker(dir); enc || err != nil {
			t.Fatalf("%s: the vault's markers read the delete marker as encrypted: %q %v", dir, why, err)
		}
		if enc, why, err := Here(dir); enc || err != nil {
			t.Fatalf("%s: labelled encrypted: %q %v", dir, why, err)
		}
		if r, err := At(dir); r.Encrypted || err != nil {
			t.Fatalf("%s: refused as encrypted: %+v %v", dir, r, err)
		}
	}
}
