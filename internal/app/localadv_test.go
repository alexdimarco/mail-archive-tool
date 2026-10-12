package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// The tombstones of FI-S2b's adversarial pass (docs/review-fault-isolation-s2b-adversarial.md).

// covers: MA-321, R1, R10, S46
// S2B-AO2 (and S2B-AI5): a healthy file never closes the gap of an unreadable one. A
// maildir file's ledger locator is its folder and the unique part of its name (the part
// a client keeps when it flags the file), so two files can share one: new/abc and
// cur/abc:2,S, or two names the ledger spells alike after it strips control characters.
// The healthy sibling's capture used to close the unreadable file's gap in the same
// run: the run exited 0, said "Recovered" and the message was nowhere. The ledger's own
// spelling is now the key the reader builds (state.BoundLocator, as Graph's), and a gap
// this run observed is closed by a capture only when it says the file vanished. The
// positive twin follows: after the permissions are fixed the file is archived and its
// gap closes as recovered.
func TestAHealthySiblingDoesNotCloseAnUnreadableFilesGap(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix permission check that root and Windows do not make")
	}
	for _, c := range []struct{ name, bad, good string }{
		{"the same unique part, new before cur", "Inbox/new/abc", "Inbox/cur/abc:2,S"},
		{"the same unique part, cur before new", "Inbox/cur/abc:2,S", "Inbox/new/abc"},
		{"a control character the ledger strips", "Inbox/cur/x\x01y:2,S", "Inbox/cur/xy:2,S"},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := filepath.Join(tmpDir(t), "mail")
			p14Write(t, filepath.Join(store, filepath.FromSlash(c.bad)), strings.ReplaceAll(p14Plain("LOSTSUBJECT-ONE", "lost-one"), "\r\n", "\n"))
			p14Write(t, filepath.Join(store, filepath.FromSlash(c.good)), strings.ReplaceAll(p14Plain("HEALTHYSUBJECT-TWO", "healthy-two"), "\r\n", "\n"))
			bad := filepath.Join(store, filepath.FromSlash(c.bad))
			if err := os.Chmod(bad, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(bad, 0o644) })

			out := tmpDir(t)
			res, err := runLocal(t, out, store)
			if !errors.Is(err, ErrPartial) || !res.Partial {
				t.Errorf("run 1 err = %v, partial %v; want ErrPartial: the unreadable file is still not captured", err, res.Partial)
			}
			gaps := ledgerOf(t, out)
			if len(gaps) != 1 || gaps[0].Reason != "file-unreadable" || gaps[0].Class != state.ClassFillable {
				t.Errorf("ledger %s; want the unreadable file's one fillable file-unreadable gap", gapReasons(gaps))
			}
			if res.Gaps.Recovered != 0 {
				t.Errorf("recovered %d; want 0: nothing was recovered, the healthy file is another message", res.Gaps.Recovered)
			}
			text := archiveText(t, out)
			if bytes.Contains(text, []byte("LOSTSUBJECT-ONE")) || !bytes.Contains(text, []byte("HEALTHYSUBJECT-TWO")) {
				t.Errorf("the archive holds the unreadable message or lacks the healthy one")
			}

			// The remedy followed: the file is readable, so its capture closes its own gap.
			if err := os.Chmod(bad, 0o600); err != nil {
				t.Fatal(err)
			}
			ageLedger(t, out, 48*time.Hour)
			res, err = runLocal(t, out, store)
			if err != nil || res.Partial || len(ledgerOf(t, out)) != 0 || res.Gaps.Recovered != 1 {
				t.Errorf("run 2: err %v, partial %v, ledger %s, recovered %d; want the file archived and its gap closed as recovered", err, res.Partial, gapReasons(ledgerOf(t, out)), res.Gaps.Recovered)
			}
			if !bytes.Contains(archiveText(t, out), []byte("LOSTSUBJECT-ONE")) {
				t.Errorf("the fixed file was not archived")
			}
		})
	}
}
