package seal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-668
// The archive-local pause sentinel is presence-based and FAIL-CLOSED: absent reads
// writable; a regular file reads sealed and its reason round-trips; a NON-regular
// sentinel (a directory, or a symlink that is never followed) reads SEALED, not
// opened; Seal is atomic + owner-only, Unseal is lock-free + idempotent; an empty
// out is never sealed (S2-C4, S2-C6).
func TestMA668_SealSentinelFailClosed(t *testing.T) {
	dir := t.TempDir()
	if IsSealed(dir) {
		t.Fatal("a fresh archive must not be sealed")
	}
	if Reason(dir) != "" {
		t.Fatal("no reason when unsealed")
	}

	// Seal -> sealed, and the reason round-trips.
	if err := Seal(dir, "parked by the owner"); err != nil {
		t.Fatal(err)
	}
	if !IsSealed(dir) {
		t.Fatal("after Seal the archive must read sealed")
	}
	if got := assure.Reached(t, Reason(dir), "pause reason"); got != "parked by the owner" {
		t.Fatalf("reason = %q, want %q", got, "parked by the owner")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Lstat(filepath.Join(dir, Name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("sentinel mode = %o, want 600 (owner-only)", fi.Mode().Perm())
		}
	}

	// Unseal -> not sealed, and idempotent.
	if err := Unseal(dir); err != nil {
		t.Fatal(err)
	}
	if IsSealed(dir) {
		t.Fatal("after Unseal the archive must read unsealed")
	}
	if err := Unseal(dir); err != nil {
		t.Fatalf("Unseal must be idempotent on an already-resumed archive: %v", err)
	}

	// FAIL-CLOSED: a non-regular sentinel (a directory) reads as sealed and yields no
	// reason — it is never opened.
	d2 := t.TempDir()
	if err := os.Mkdir(filepath.Join(d2, Name), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsSealed(d2) {
		t.Fatal("a non-regular sentinel must fail closed (sealed)")
	}
	if Reason(d2) != "" {
		t.Fatal("a non-regular sentinel yields no reason (not opened)")
	}

	// FAIL-CLOSED: a symlink sentinel is not followed; it reads as sealed.
	if runtime.GOOS != "windows" {
		d3 := t.TempDir()
		if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), filepath.Join(d3, Name)); err != nil {
			t.Skipf("symlink unsupported here: %v", err)
		}
		if !IsSealed(d3) {
			t.Fatal("a symlinked sentinel must fail closed (sealed), never be followed")
		}
	}

	// An empty location is not an archive.
	if IsSealed("") {
		t.Fatal("empty out must not read sealed")
	}
	if err := Seal("", "x"); err == nil {
		t.Fatal("Seal on an empty out must error")
	}
}
