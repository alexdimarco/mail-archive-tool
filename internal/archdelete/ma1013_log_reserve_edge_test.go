package archdelete

import (
	"os"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1013, R36
// At the edge of the cap — room for a started line but not for the delete's later lines — the log
// is still never written past what MailArchive reads (the delete residuals' security pass, CON-05:
// with logReserve set to 0, MA-1013's own test stayed green while a log with 500 bytes of room
// ended at 33,555,112 bytes, unreadable). For room from 200 to 4,000 bytes the delete either
// refuses at step 0 or completes, and the log stays readable every time. Positive twin first: a
// log with the reserve and more left takes a whole delete and stays readable.
func TestTheDeletionLogStaysReadableAtTheEdgeOfItsCap(t *testing.T) {
	pad := func(path string, size int64) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	twin := newRig(t, smallArchive(t, t.TempDir()))
	pad(twin.x.Log, logCap-logReserve-64<<10)
	if res := twin.run(t, false); res.Status != StatusComplete {
		t.Fatalf("positive twin: a log with room left: %s", res.Message())
	}
	if _, err := readLog(twin.x.Log); err != nil {
		t.Fatalf("positive twin: the log after the delete cannot be read: %v", err)
	}

	readable := 0
	for room := int64(200); room <= 4000; room += 100 {
		r := newRig(t, smallArchive(t, t.TempDir()))
		pad(r.x.Log, logCap-room)
		res := r.run(t, false)
		if _, err := readLog(r.x.Log); err != nil {
			fi, _ := os.Stat(r.x.Log)
			t.Errorf("with %d bytes of room the delete (%s) left a log of %d bytes MailArchive cannot read: %v", room, res.Status, fi.Size(), err)
			continue
		}
		readable++
	}
	assure.Reached(t, readable == 39, "the log readable after a delete at each of 39 rooms at the edge of its cap")
}
