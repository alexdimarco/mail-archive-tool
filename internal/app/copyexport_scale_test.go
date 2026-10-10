package app

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// heapBound60k is the stated bound on the heap a 60,000-message run may grow: the
// namer's per-directory fold keys and the sums' path and hash lines are what it
// legitimately holds; an entries slice is what it must not.
const heapBound60k = 40 << 20

// dirSyncCounter counts directory syncs through the contained writer.
type dirSyncCounter struct {
	ceFS
	n atomic.Int64
}

func (d *dirSyncCounter) SyncDir(rel string) error {
	d.n.Add(1)
	return d.ceFS.SyncDir(rel)
}

// covers: MA-432, R29, S52
// Scale (the engine's half): a 60,000-message selection exports with the heap held
// under a stated bound, because each message's state goes to the run record and the
// manifest and sums are streamed from it (no entries slice is held, XC1); the run does
// not pay a directory sync per file (the batched syncs, XC5, through the sync seam);
// every message has its own line in the record; the progress counters advance and an
// ETA is present while it runs; and a 200,001-message selection is refused with R-i.
func TestCopyExportScalesToSixtyThousandMessages(t *testing.T) {
	const n = 60000
	e, src := newCEEnv(t), newFakeSrc()
	src.body = func(id string) []byte { return []byte("From: a@x\r\n\r\n" + id) }
	syncs := &dirSyncCounter{}
	e.fsWrap = func(f ceFS) ceFS { syncs.ceFS = f; return syncs }
	msgs := ceMsgs(n)
	for i := range msgs {
		msgs[i].SizeHint = int64(len(src.body(msgs[i].SourceID)))
	}
	p := cePlan(t, e, src, msgs)
	run := e.NewCopyRun(p, src)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var maxDone int
	var sawETA bool
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			pr := run.Progress()
			mu.Lock()
			if pr.Done > maxDone {
				maxDone = pr.Done
			}
			if pr.ETA > 0 && pr.Done > 0 && pr.Done < n {
				sawETA = true
			}
			mu.Unlock()
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	var res *CopyResult
	peak := peakHeap(func() {
		var err error
		if res, err = run.Run(context.Background()); err != nil {
			t.Errorf("the run was refused: %v", err)
		}
	})
	close(stop)
	wg.Wait()
	if res == nil || res.Counts.Exported != n || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("the 60,000-message export: %+v", res)
	}
	t.Logf("peak heap growth during the run: %d MiB", peak>>20)
	if bound := uint64(heapBound60k + raceHeapAllowance); uint64(peak) > bound {
		t.Errorf("the heap grew by %d MiB over %d messages (bound %d MiB): per-message state is being held in memory", peak>>20, n, bound>>20)
	}
	dirs := 0
	filepath.WalkDir(res.Root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs++
		}
		return nil
	})
	if got := syncs.n.Load(); got > int64(dirs)+2 {
		t.Errorf("%d directory syncs for %d messages in %d directories: syncs are batched, once per directory before the sums, not per file", got, n, dirs)
	}
	lines := 0
	scanLines(e.recordPath(res.RunID), func(line []byte) error {
		if strings.HasPrefix(string(line), `{"t":"msg"`) {
			lines++
		}
		return nil
	})
	if lines != n {
		t.Errorf("the run record has %d progress lines, want %d", lines, n)
	}
	mu.Lock()
	if maxDone < 1 || !sawETA {
		t.Errorf("progress never advanced or showed an ETA (max done %d, ETA seen %v)", maxDone, sawETA)
	}
	mu.Unlock()
	if pr := run.Progress(); pr.Done != n || pr.Selected != n {
		t.Errorf("final progress = %+v", pr)
	}
	if man := manifestOf(t, res.Root); man["counts"].(map[string]any)["exported"] != float64(n) {
		t.Errorf("manifest counts = %v", man["counts"])
	}

	// 200,001 messages: refused with R-i.
	big := make([]CopyMessage, copyMaxMessages+1)
	for i := range big {
		big[i] = CopyMessage{SourceID: "x", SizeHint: -1}
	}
	_, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: big, Dest: p.Dest, Options: CopyOptions{PreserveFolders: true}})
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-i" {
		t.Fatalf("a 200,001-message selection did not refuse with R-i: %v", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names("200,001 messages", "up to 200,000", "Split it into several exports"))
	if _, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: big[:copyMaxMessages], Dest: p.Dest, Options: CopyOptions{PreserveFolders: true}}); err != nil {
		t.Errorf("a 200,000-message selection (the cap itself) was refused: %v", err)
	}
}
