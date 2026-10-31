package wizard

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
)

// covers: MA-898
// The encrypted Run now is attended from slice AS8 (R38; design-presync-attended
// §2.2, C-79, C-81, OQ-1, OQ-2, OQ-6). Through the presyncjob seam, a real
// RunJobInProcessKeyed of a sync job into a real encrypted archive, for macOS and
// for Windows, hands the session Attended true, the Desktop's words, the Run now's
// Stop and EndWait, and NeverQuit from AttendedQuitAllowed alone (both values), and
// calls OnReadStart once, right before it reads — but only where
// AttendedStartAllowed is true: where it is false the session is never called,
// StartNotAllowed says so once, and the backup still runs. Positive twin first: the
// start allowed, the session is called once. A job with the sync off installs no
// hook (R30), yet its run still reaches read start (the Lock window's end, §3.2).
func TestKeyedRunNowIsAttended(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(out, "wizard keyed attended passphrase", keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	_, mbox := headlessFixture(t)
	fake := &fakeSession{}
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()
	jobPath := filepath.Join(t.TempDir(), "mailarchive-enc.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-enc", Out: out, Mode: "incremental",
		Inputs: []string{mbox}, ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	apply := func(o *app.Options) (func(), error) { return keyring.Apply(o, u) }
	stop, stopEnd := context.WithCancel(context.Background())
	defer stopEnd()
	endWait, endWaitEnd := context.WithCancel(context.Background())
	defer endWaitEnd()

	n := 0
	for _, goos := range []string{"darwin", "windows"} {
		for _, quitAllowed := range []bool{false, true} {
			r := presync.OverrideTablesForTest(map[string]bool{goos: quitAllowed}, map[string]bool{goos: true})
			var notAllowed, readStarts int
			res, err := RunJobInProcessKeyed(context.Background(), stop, jobPath, logger, RunNow{GOOS: goos, EndWait: endWait,
				StartNotAllowed: func() { notAllowed++ }, OnReadStart: func() { readStarts++ }}, apply)
			r()
			if err != nil || res == "" {
				t.Fatalf("%s: the keyed Run now failed: %v", goos, err)
			}
			n++
			calls := fake.calls()
			if len(calls) != n {
				t.Fatalf("%s, quit allowed %v: the session was called %d times in all, want %d", goos, quitAllowed, len(calls), n)
			}
			c := calls[n-1]
			if !c.Attended || c.NeverQuit != !quitAllowed || c.EndWait != endWait || c.Stop != stop ||
				c.Surface != presync.SurfaceDesktop || c.GOOS != goos {
				t.Errorf("%s, quit allowed %v: handed Attended %v, NeverQuit %v, EndWait %v, Stop %v, Surface %q, GOOS %q",
					goos, quitAllowed, c.Attended, c.NeverQuit, c.EndWait == endWait, c.Stop == stop, c.Surface, c.GOOS)
			}
			if notAllowed != 0 || readStarts != 1 {
				t.Errorf("%s: start allowed, yet StartNotAllowed called %d times; OnReadStart %d times, want 1", goos, notAllowed, readStarts)
			}
		}
		// The start not allowed on this platform (C-49): no hook, said once, the backup runs.
		r := presync.OverrideTablesForTest(map[string]bool{goos: true}, map[string]bool{goos: false})
		var notAllowed int
		res, err := RunJobInProcessKeyed(context.Background(), stop, jobPath, logger, RunNow{GOOS: goos, EndWait: endWait,
			StartNotAllowed: func() { notAllowed++ }}, apply)
		r()
		if err != nil || res == "" {
			t.Fatalf("%s, start not allowed: the backup did not run: %v", goos, err)
		}
		if len(fake.calls()) != n || notAllowed != 1 {
			t.Errorf("%s, start not allowed: the session was called (%d calls, want %d), StartNotAllowed %d times, want 1",
				goos, len(fake.calls()), n, notAllowed)
		}
	}

	// The sync off: no session, and the run still reaches read start.
	off := filepath.Join(t.TempDir(), "mailarchive-enc-off.json")
	if err := job.Write(off, job.Job{Name: "mailarchive-enc-off", Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	r := presync.OverrideTablesForTest(nil, map[string]bool{"darwin": true})
	defer r()
	var readStarts int
	if _, err := RunJobInProcessKeyed(context.Background(), stop, off, logger, RunNow{GOOS: "darwin", OnReadStart: func() { readStarts++ }}, apply); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls()) != n || readStarts != 1 {
		t.Errorf("sync off: %d session calls (want %d), OnReadStart %d times (want 1)", len(fake.calls()), n, readStarts)
	}
}
