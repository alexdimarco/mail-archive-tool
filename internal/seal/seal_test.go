package seal

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// covers: MA-683
// The pause reason is sanitized at the read boundary (security pass 2026-10-20): the
// sentinel is writable by whoever can write the archive folder and its reason is echoed into
// terminals, logs and dialogs via RefuseWrite, so Reason strips ANSI escapes / embedded
// newlines (StripControl) and bidi overrides (StripBidi) before any consumer sees it — a
// crafted reason cannot forge an output line or reverse text.
func TestMA683_PauseReasonIsSanitized(t *testing.T) {
	dir := t.TempDir()
	// A hostile/damaged reason: an ANSI colour escape, an embedded newline forging a line,
	// and a right-to-left override.
	raw := "parked \x1b[31mFAKE\x1b[0m\nmailarchive: all clear‮gnp.esu"
	if err := Seal(dir, raw); err != nil {
		t.Fatal(err)
	}
	got := assure.Reached(t, Reason(dir), "sanitized pause reason")
	for _, bad := range []string{"\x1b", "\n", "\r", "‮"} {
		if strings.Contains(got, bad) {
			t.Fatalf("Reason must strip control/bidi chars (found %q), got %q", bad, got)
		}
	}
	// RefuseWrite embeds the reason in its error; it must be sanitized there too.
	msg := RefuseWrite(dir).Error()
	for _, bad := range []string{"\x1b", "\n", "‮"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("RefuseWrite must not echo raw control/bidi chars (found %q), got %q", bad, msg)
		}
	}
}
