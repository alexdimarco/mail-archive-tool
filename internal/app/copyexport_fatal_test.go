package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph/graphtest"
)

// faultFS wraps the contained writer with the failures a destination has: a full
// disk after so many message bytes (finalization, which writes only at the root,
// fits: that is the reserve), a stick that is pulled (every operation fails) and
// placements that fail with a chosen error.
type faultFS struct {
	ceFS
	mu         sync.Mutex
	dead       bool
	deadErr    error
	fullAfter  int64 // message bytes accepted before ENOSPC; < 0: never
	written    int64
	pullOnFull bool // the stick is pulled the moment the disk fills
	placeErr   func(dir, tmp, final string) error
}

func newFaultFS() *faultFS { return &faultFS{fullAfter: -1, deadErr: syscall.ENODEV} }

func (f *faultFS) isDead() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead {
		return &os.PathError{Op: "write", Path: "E:", Err: f.deadErr}
	}
	return nil
}

func (f *faultFS) pull(dead bool) { f.mu.Lock(); f.dead = dead; f.mu.Unlock() }

func (f *faultFS) Mkdir(rel string) error {
	if err := f.isDead(); err != nil {
		return err
	}
	return f.ceFS.Mkdir(rel)
}

func (f *faultFS) CreateTemp(dir string) (ceFile, string, error) {
	if err := f.isDead(); err != nil {
		return nil, "", err
	}
	file, name, err := f.ceFS.CreateTemp(dir)
	if err != nil || !strings.HasPrefix(dir, "messages") {
		return file, name, err
	}
	return &faultFile{ceFile: file, fs: f}, name, nil
}

func (f *faultFS) Place(dir, tmp, final string) error {
	if err := f.isDead(); err != nil {
		return err
	}
	if f.placeErr != nil {
		if err := f.placeErr(dir, tmp, final); err != nil {
			return err
		}
	}
	return f.ceFS.Place(dir, tmp, final)
}

func (f *faultFS) Replace(dir, tmp, final string) error {
	if err := f.isDead(); err != nil {
		return err
	}
	return f.ceFS.Replace(dir, tmp, final)
}

func (f *faultFS) Remove(rel string) error {
	if err := f.isDead(); err != nil {
		return err
	}
	return f.ceFS.Remove(rel)
}

func (f *faultFS) Lstat(rel string) (fs.FileInfo, error) {
	if err := f.isDead(); err != nil {
		return nil, err
	}
	return f.ceFS.Lstat(rel)
}

func (f *faultFS) Open(rel string) (*os.File, error) {
	if err := f.isDead(); err != nil {
		return nil, err
	}
	return f.ceFS.Open(rel)
}

func (f *faultFS) SyncDir(rel string) error {
	if err := f.isDead(); err != nil {
		return err
	}
	return f.ceFS.SyncDir(rel)
}

func (f *faultFS) Probe() error {
	if err := f.isDead(); err != nil {
		return err
	}
	return f.ceFS.Probe()
}

type faultFile struct {
	ceFile
	fs *faultFS
}

func (w *faultFile) Write(p []byte) (int, error) {
	w.fs.mu.Lock()
	full := w.fs.fullAfter >= 0 && w.fs.written+int64(len(p)) > w.fs.fullAfter
	pull := full && w.fs.pullOnFull
	if pull {
		w.fs.dead = true
	}
	if !full {
		w.fs.written += int64(len(p))
	}
	w.fs.mu.Unlock()
	if full {
		return 0, errFull
	}
	return w.ceFile.Write(p)
}

// covers: MA-427, R29, R5, R12
// The per-OS table of destination-fatal write errors, matched with errors.As on
// syscall.Errno and never with errors.Is(err, syscall.ENOSPC): the Windows codes 112
// (disk full), 39, 19 (write protected), 1167 (device not connected) and 82 classify
// as destination-fatal on every CI OS, a code that is not in the table does not, and
// the unix table does for this host's own ENOSPC and EROFS.
func TestCopyExportDestinationFatalErrnoTable(t *testing.T) {
	for _, code := range []syscall.Errno{112, 39, 19, 1167, 82, 21, 55, 64, 59} {
		err := &os.PathError{Op: "write", Path: "E:/x", Err: code}
		if !destinationFatal("windows", err) {
			t.Errorf("Windows error %d is not destination-fatal", code)
		}
		if !destinationFatal("windows", fmt.Errorf("download message M: %w", &ceWriteError{err})) {
			t.Errorf("Windows error %d inside a wrapped download error is not destination-fatal", code)
		}
	}
	for _, code := range []syscall.Errno{5, 32, 2, 183} { // access denied, sharing violation, not found, already exists
		if destinationFatal("windows", &os.PathError{Err: code}) {
			t.Errorf("Windows error %d is destination-fatal; it is a per-message failure", code)
		}
	}
	if destinationFatal("windows", errors.New("plain")) {
		t.Error("an error with no errno is destination-fatal")
	}
	for _, code := range []syscall.Errno{syscall.ENOSPC, syscall.EROFS, syscall.EIO, syscall.ENODEV, syscall.ETIMEDOUT} {
		if !destinationFatal("linux", &os.PathError{Err: code}) {
			t.Errorf("%v is not destination-fatal on unix", code)
		}
	}
	if destinationFatal("linux", &os.PathError{Err: syscall.EACCES}) {
		t.Error("EACCES is destination-fatal; it is a per-message failure")
	}
	if !sharingViolation("windows", &os.PathError{Err: syscall.Errno(32)}) || sharingViolation("linux", &os.PathError{Err: syscall.Errno(32)}) {
		t.Error("a sharing violation is retried in place on Windows only")
	}
}

// covers: MA-427, R29, R5, R12
// A disk that fills mid-run (a fake filesystem that stays full after N message bytes)
// stops the run with R-g, outcome 1: the message being written is listed as retryable
// with the stop's reason, its temp file is removed, the marker is kept and rewritten
// STOPPED, finalization fits (the manifest says failed and the sums verify), and the
// stopped run's record yields "the remaining messages" with the message that was in
// flight when the space ran out last (message 13 of 20, after 14..20). The positive
// twin first: the same run on a disk with room is complete.
func TestCopyExportDiskFullStopsWithRG(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	if res := mustRun(t, e, cePlan(t, e, src, ceMsgs(20)), src); res.Outcome != CopyOutcomeComplete {
		t.Fatalf("the positive twin: %s", res.Label)
	}
	body := len(ceBody("M00001"))
	fake := newFaultFS()
	fake.fullAfter = int64(12*body) + int64(body)/2 // the 13th message does not fit
	e.fsWrap = func(f ceFS) ceFS { fake.ceFS = f; return fake }
	p := cePlan(t, e, src, ceMsgs(20))
	res := mustRun(t, e, p, src)
	if res.Outcome != CopyOutcomeFailed || res.Stop == nil || res.Stop.RID != "R-g" || res.Status != CopyStatusFailed {
		t.Fatalf("a full disk ended as %s (%+v)", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("12 of 20 messages", "no space left", "Export the remaining messages"),
		assure.NoSideEffect(func() bool { return len(tempResidue(t, res.Root)) == 0 }))
	if !strings.Contains(readText(t, res.Root, copyMarkerName), "STOPPED") {
		t.Errorf("the marker is %q: it must be rewritten STOPPED, never removed", readText(t, res.Root, copyMarkerName))
	}
	man := manifestOf(t, res.Root)
	if man["run"].(map[string]any)["status"] != "failed" {
		t.Errorf("the manifest says %v", man["run"].(map[string]any)["status"])
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("finalization on a full disk left a bad export: %v %v %v", bad, missing, extra)
	}
	inflight := assure.Reached(t, failureOf(t, res.Root, "M00013"), "the in-flight message's failure")
	if inflight["class"] != classRetryable || !strings.Contains(inflight["reason"].(string), "R-g") {
		t.Errorf("the message in flight is %v", inflight)
	}
	rest, err := e.RunRemaining(res.RunID)
	if err != nil || len(rest) != 8 || rest[0].SourceID != "M00014" || rest[6].SourceID != "M00020" || rest[7].SourceID != "M00013" {
		var ids []string
		for _, m := range rest {
			ids = append(ids, m.SourceID)
		}
		t.Errorf("the remaining messages are %v (%v), want M00014..M00020 then M00013 last", ids, err)
	}
}

// covers: MA-427, R29, R5, R12
// ENOSPC, then the stick is removed: finalization fails and the marker stays; the
// remaining messages are still read from the run record with the volume gone; once the
// stick is back, Finish as partial completes the part from the run record, rewrites the
// marker STOPPED, and the part verifies with `sha256sum -c`'s in-Go mirror.
func TestCopyExportPulledStickIsFinishedAsPartial(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	body := len(ceBody("M00001"))
	fake := newFaultFS()
	fake.fullAfter, fake.pullOnFull = int64(4*body)+int64(body)/2, true
	e.fsWrap = func(f ceFS) ceFS { fake.ceFS = f; return fake }
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(10)), src)
	if res.Stop == nil || res.Stop.RID != "R-g" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("a pulled stick ended as %s (%+v)", res.Label, res.Stop)
	}
	sum := mustSummary(t, e, res.RunID)
	if sum.Sealed != nil || sum.Stopped == nil || sum.Stopped.RID != "R-g" {
		t.Fatalf("a run whose finalization failed is unsealed with a stopped line: sealed %+v, stopped %+v", sum.Sealed, sum.Stopped)
	}
	if !exists(res.Root, copyMarkerName) {
		t.Fatal("the marker is gone after a failed finalization")
	}
	if rest, err := e.RunRemaining(res.RunID); err != nil || len(rest) != 6 || rest[5].SourceID != "M00005" {
		t.Errorf("with the volume gone the remaining messages are %d (%v), want 6 from the run record, the in-flight M00005 last", len(rest), err)
	}
	// Finishing while the stick is still gone changes nothing: no recorded file is
	// read as missing because the volume did not answer.
	before := mustSummary(t, e, res.RunID)
	if _, err := e.FinishAsPartial(res.RunID); err == nil {
		t.Error("Finish as partial finished a folder on a volume that does not answer")
	}
	if after := mustSummary(t, e, res.RunID); after.Progress != before.Progress || after.Statuses[statusExported] != before.Statuses[statusExported] || after.Sealed != nil {
		t.Errorf("a failed Finish as partial changed the record: %+v -> %+v", before.Statuses, after.Statuses)
	}

	fake.pull(false) // the stick is plugged back in
	fin, err := e.FinishAsPartial(res.RunID)
	if err != nil {
		t.Fatalf("Finish as partial: %v", err)
	}
	if fin.Status != CopyStatusFinishedAsPartial || !strings.Contains(fin.Label, "(finished as partial)") {
		t.Errorf("Finish as partial ended as %s / %s", fin.Status, fin.Label)
	}
	if !strings.Contains(readText(t, res.Root, copyMarkerName), "STOPPED") {
		t.Error("the marker of a finished-as-partial run is not rewritten STOPPED")
	}
	if man := manifestOf(t, res.Root); man["run"].(map[string]any)["status"] != "failed" {
		t.Errorf("the manifest of a finished-as-partial run says %v", man["run"].(map[string]any)["status"])
	}
	// The temp the pulled stick kept is residue the app never cleans (design §3, §5),
	// named by its prefix; nothing else is unlisted or changed.
	bad, missing, extra := sumsCheck(t, res.Root)
	if len(bad)+len(missing) != 0 || len(extra) != 1 || !strings.Contains(extra[0], copyTempPrefix) {
		t.Errorf("the finished part: changed %v, missing %v, unlisted %v; want only the temp residue", bad, missing, extra)
	}
	if ss := mustSummary(t, e, res.RunID); ss.Sealed == nil || ss.Sealed.Status != CopyStatusFinishedAsPartial || ss.Sealed.Counts.Exported != 4 {
		t.Errorf("the sealed line = %+v", ss.Sealed)
	}
}

// covers: MA-427, R29, R5, R12
// Five consecutive write failures of any class are run-fatal (R-g naming the last
// error); four, then a message that writes, are not: the run goes on and ends partial.
func TestCopyExportFiveConsecutiveWriteFailuresAreRunFatal(t *testing.T) {
	run := func(failing int) *CopyResult {
		e, src := newCEEnv(t), newFakeSrc()
		fake := newFaultFS()
		n := 0
		fake.placeErr = func(dir, tmp, final string) error {
			if !strings.HasPrefix(dir, "messages") {
				return nil
			}
			n++
			if n <= failing {
				return &os.LinkError{Op: "rename", Err: syscall.EPERM}
			}
			return nil
		}
		e.fsWrap = func(f ceFS) ceFS { fake.ceFS = f; return fake }
		return mustRun(t, e, cePlan(t, e, src, ceMsgs(8)), src)
	}
	four := run(4)
	if four.Stop != nil || four.Outcome != CopyOutcomePartial || four.Counts.Exported != 4 || four.Counts.Failed != 4 {
		t.Fatalf("four write failures in a row: %s, %+v (stop %+v), want a partial run that went on", four.Label, four.Counts, four.Stop)
	}
	five := run(5)
	if five.Stop == nil || five.Stop.RID != "R-g" || five.Outcome != CopyOutcomeFailed {
		t.Fatalf("five write failures in a row: %s (stop %+v), want run-fatal R-g", five.Label, five.Stop)
	}
	assure.Refused(t, int(five.Outcome), five.Stop.Message, assure.Code(1), assure.Names("operation not permitted", "Export the remaining messages"))
}

// covers: MA-427, R29, R5, R12
// A run-record append that fails mid-run (a full or unwritable state volume) is R-d2,
// never R-g: the run stops naming the state directory, the folder stays marked
// unfinished (the seal cannot be written either), and once the volume takes lines
// again Finish as partial completes the part.
func TestCopyExportStateDirectoryFailureIsRD2(t *testing.T) {
	// A failure that clears at once: the run stops with R-d2 and still finalizes,
	// sealing the stop's reason.
	{
		e, src := newCEEnv(t), newFakeSrc()
		n := 0
		e.faults = &copyFaults{appendErr: func(path string) error {
			if strings.Contains(path, "exports") && strings.HasSuffix(path, ".jsonl") {
				if n++; n == 4 {
					return syscall.ENOSPC
				}
			}
			return nil
		}}
		res := mustRun(t, e, cePlan(t, e, src, ceMsgs(10)), src)
		if res.Stop == nil || res.Stop.RID != "R-d2" || res.Status != CopyStatusFailed {
			t.Fatalf("one failed record append ended as %s (%+v), want a sealed run stopped by R-d2", res.Label, res.Stop)
		}
		if s := mustSummary(t, e, res.RunID).Sealed; s == nil || s.Stop == nil || s.Stop.RID != "R-d2" {
			t.Errorf("the sealed line does not carry R-d2: %+v", s)
		}
	}
	e, src := newCEEnv(t), newFakeSrc()
	n := 0
	e.faults = &copyFaults{appendErr: func(path string) error {
		if strings.HasSuffix(path, ".jsonl") && strings.Contains(path, "exports") {
			n++
			if n > 3 {
				return syscall.ENOSPC
			}
		}
		return nil
	}}
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(10)), src)
	if res.Stop == nil || res.Stop.RID != "R-d2" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("a failing run record ended as %s (%+v), want R-d2", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names(e.StateDir, "Finish as partial", "Export the remaining messages"),
		assure.Forbid("destination is full"))
	if !exists(res.Root, copyMarkerName) || mustSummary(t, e, res.RunID).Sealed != nil {
		t.Error("the folder is not left unfinished: it needs its marker and no seal")
	}
	e.faults = nil // the state volume takes lines again
	fin, err := e.FinishAsPartial(res.RunID)
	if err != nil {
		t.Fatalf("Finish as partial: %v", err)
	}
	if fin.Status != CopyStatusFinishedAsPartial {
		t.Errorf("finished as %s", fin.Status)
	}
	// The file written just before the state volume failed has no line in the record, so
	// it is not listed (W6: "the files exist, unrecorded"); the message is not attempted.
	bad, missing, extra := sumsCheck(t, res.Root)
	if len(bad)+len(missing) != 0 || len(extra) != 1 || !strings.Contains(extra[0], "M00002") {
		t.Errorf("the finished part: changed %v, missing %v, unlisted %v; want exactly the unrecorded M00002", bad, missing, extra)
	}
	if rest, _ := e.RunRemaining(res.RunID); len(rest) != 9 || rest[0].SourceID != "M00002" {
		t.Errorf("the remaining messages are %d, want 9 starting with the unrecorded M00002", len(rest))
	}
}

// covers: MA-427, R29, R5, R12
// A volume a few bytes above the reserve with a message larger than it: that message
// (its size unknown) is aborted at its budget, the run stops with R-g, and
// finalization fits; and with free space below the size cap, a $value whose declared
// length is above the cap is listed above-cap and the run goes on (never R-g), while
// one whose declared length is within the cap but above the budget, announced or
// streamed, stops with R-g.
func TestCopyExportCapIsDecidedBeforeTheBudget(t *testing.T) {
	f := newMeFake(t)
	f.value("ABOVE", graphtest.Sized(100<<10, true))
	f.value("WITHIN", graphtest.Sized(32<<10, true))
	f.value("CHUNKED", graphtest.Sized(32<<10, false))
	e := newCEEnv(t)
	e.sizeCap = 64 << 10
	src := f.source(t)
	var plan *CopyPlan
	e.freeDest = func(string) (uint64, error) {
		if plan == nil {
			return 1 << 40, nil // while the plan is being made
		}
		return uint64(plan.Reserve) + 8<<10, nil
	}

	plan = planFor(t, e, src, idMsgs("OK1", "ABOVE", "OK2", "WITHIN", "OK3"))
	res := runWith(t, e, plan, src)
	if res.Stop == nil || res.Stop.RID != "R-g" {
		t.Fatalf("a message above the budget but within the cap ended the run as %s (%+v), want R-g", res.Label, res.Stop)
	}
	if above := failureOf(t, res.Root, "ABOVE"); above == nil || above["class"] != classTerminal || !strings.Contains(above["reason"].(string), "larger than this version") {
		t.Errorf("a message declared above the cap is listed %v: the run must go on, never R-g", above)
	}
	if res.Counts.Exported != 2 || f.hit("OK2") != 1 {
		t.Errorf("the run did not go on past the above-cap message: %+v", res.Counts)
	}
	if w := failureOf(t, res.Root, "WITHIN"); w == nil || w["class"] != classRetryable {
		t.Errorf("the message aborted at its budget is %v, want the in-flight retryable failure", w)
	}
	if !strings.Contains(readText(t, res.Root, copyMarkerName), "STOPPED") || len(tempResidue(t, res.Root)) != 0 {
		t.Error("finalization did not fit, or a temp was left")
	}
	plan = planFor(t, e, src, idMsgs("OK1", "CHUNKED", "OK2"))
	if streamed := runWith(t, e, plan, src); streamed.Stop == nil || streamed.Stop.RID != "R-g" || streamed.Counts.Exported != 1 {
		t.Errorf("a body streamed past the budget ended as %s (%+v)", streamed.Label, streamed.Stop)
	}
}

// covers: MA-427, R29, R5, R12
// A $value redirected to a foreign host stops the run with R-u3 naming that host, never
// R-u: the message is in failures as retryable, the foreign host receives no request
// (the bearer token was never sent there), and the stop's remedy reaches it.
func TestCopyExportRedirectToAnotherHostIsRU3(t *testing.T) {
	var foreign int
	var fmu sync.Mutex
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmu.Lock()
		foreign++
		fmu.Unlock()
		io.WriteString(w, "stolen")
	}))
	defer other.Close()
	f := newMeFake(t)
	f.value("REDIR", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/x", http.StatusFound) })
	e := newCEEnv(t)
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "REDIR", "A2")), src)
	if res.Stop == nil || res.Stop.RID != "R-u3" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("a redirect to another host ended as %s (%+v), want R-u3", res.Label, res.Stop)
	}
	host := strings.TrimPrefix(other.URL, "http://")
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names(host, "your sign-in was never sent there", "Export the remaining messages"),
		assure.Forbid("been down for 5 minutes"))
	fmu.Lock()
	defer fmu.Unlock()
	if foreign != 0 {
		t.Errorf("the foreign host received %d requests", foreign)
	}
	if fail := failureOf(t, res.Root, "REDIR"); fail == nil || fail["class"] != classRetryable {
		t.Errorf("the redirected message is %v, want a retryable failure", fail)
	}
	if rest, _ := e.RunRemaining(res.RunID); len(rest) != 2 || rest[0].SourceID != "A2" || rest[1].SourceID != "REDIR" {
		t.Errorf("the remaining messages are %+v, want A2 then REDIR last", rest)
	}
}

// covers: MA-427, MA-516, R29, R5, R12
// A retried message leaves no .mailexport-*.tmp, and the budget is read again before
// each retry: a download cut after its first bytes is retried, and a volume that fills
// between the two attempts stops the run with R-g at the second, with the message
// still first in not_attempted (the disk stopped it before a byte of the second try).
func TestCopyExportRetryReadsTheBudgetAgain(t *testing.T) {
	f := newMeFake(t)
	f.value("FLAKY", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, _ := hj.Hijack()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\npartial body")
		buf.Flush()
		conn.Close()
	})
	e := newCEEnv(t)
	src := f.source(t)
	var plan *CopyPlan
	calls := 0
	e.freeDest = func(string) (uint64, error) {
		calls++
		if calls <= 4 { // the checks before the message and before its first attempt, and Start's
			return 1 << 40, nil
		}
		return 0, nil
	}
	plan = planFor(t, e, src, idMsgs("FLAKY", "A2"))
	res := runWith(t, e, plan, src)
	if res.Stop == nil || res.Stop.RID != "R-g" {
		t.Fatalf("a volume that filled between two attempts ended as %s (%+v), want R-g", res.Label, res.Stop)
	}
	if f.hit("FLAKY") != 1 {
		t.Errorf("FLAKY was requested %d times: the second attempt must stop before its first byte", f.hit("FLAKY"))
	}
	if r := tempResidue(t, res.Root); len(r) != 0 {
		t.Errorf("the cut attempt left %v", r)
	}
	if rest, _ := e.RunRemaining(res.RunID); len(rest) != 2 || rest[0].SourceID != "FLAKY" {
		t.Errorf("the remaining messages are %+v, want FLAKY first (the disk stopped it, not the message)", rest)
	}
}

// covers: MA-427, R29, R5, R12
// A 401 whose forced token refresh fails stops the run with R-c (never R-u, never a
// per-message failure that lets the run go through every message): the message in
// flight is listed retryable, the outcome is 1, and the remedy is to sign in again.
func TestCopyExportA401WhoseRefreshFailsIsRC(t *testing.T) {
	f := newMeFake(t)
	for _, id := range []string{"B1", "B2", "B3"} {
		f.value(id, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"InvalidAuthenticationToken","message":"expired"}}`)
		})
	}
	f.tokenFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":"invalid_grant","error_description":"revoked"}`)
	}
	e := newCEEnv(t)
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "B1", "B2", "B3")), src)
	if res.Stop == nil || res.Stop.RID != "R-c" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("a 401 whose refresh fails ended as %s (%+v), want R-c", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("Sign in again", "Export the remaining messages", "1 of 4 messages"))
	if f.hit("B2") != 0 || f.hit("B3") != 0 {
		t.Errorf("the run went on past the refused sign-in (B2 %d, B3 %d requests)", f.hit("B2"), f.hit("B3"))
	}
	if fail := failureOf(t, res.Root, "B1"); fail == nil || fail["class"] != classRetryable {
		t.Errorf("the message in flight is %v, want a retryable failure", fail)
	}
}

var _ = time.Second
