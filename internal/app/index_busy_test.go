package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/index"
)

// covers: MA-398, R18, R8, S29
// A healthy search index that is merely in use is never reported as damaged.
// status polls the real Gather/Assess while a writer repeatedly opens
// search.db, adds a row and closes it (the shape of an export's checkpoints,
// and of the WAL recovery the next opener runs after a read-only reader left
// the -wal/-shm pair behind): every poll must read the index — never RED
// index_unreadable with a rebuild remedy for a SQLITE_BUSY that only meant
// "wait a moment". Positive twin: with no writer the index reads cleanly.
func TestStatusReadsABusyIndex(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(out, "search.db")

	in := health.Gather(out, "")
	assure.Reached(t, in.Indexed, "indexed rows read by status with no writer")
	if rep := health.Assess(in, time.Now()); reasonFor(rep, "index_unreadable") != "" || reasonFor(rep, "index_busy") != "" {
		t.Fatalf("an idle healthy index was flagged: %v", rep.Reasons)
	}

	stop, done := make(chan struct{}), make(chan int)
	go func() {
		writes := 0
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- writes
				return
			default:
			}
			ix, err := index.Open(dbPath)
			if err != nil {
				continue // the writer's own opens are TestExportIndexOpenWaitsOutStatusPolls's to judge; here it just tries again
			}
			m := reindexMsg(fmt.Sprintf("busy %d", i), fmt.Sprintf("<busy%d@x>", i))
			addErr := ix.Add("s", []string{"Inbox"}, m, fmt.Sprintf("s/Inbox/busy-%d.html", i), fmt.Sprintf("busy-%d", i))
			if ix.Close() == nil && addErr == nil {
				writes++
			}
		}
	}()

	// Two status pollers at once (a monitor and the GUI, say): a read-only
	// reader that closes last leaves the -wal/-shm pair for the next opener to
	// recover, which busies every other opener meanwhile.
	type tally struct {
		polls, unread int
		first         string
	}
	poll := func(deadline time.Time, res chan<- tally) {
		var tl tally
		for ; time.Now().Before(deadline); tl.polls++ {
			in := health.Gather(out, "")
			if rep := health.Assess(in, time.Now()); !in.HasIndex {
				tl.unread++
				if tl.first == "" {
					tl.first = fmt.Sprintf("%v / %v", in.IndexErr, rep.Reasons)
				}
			}
		}
		res <- tl
	}
	deadline, res := time.Now().Add(2*time.Second), make(chan tally)
	go poll(deadline, res)
	go poll(deadline, res)
	a, b := <-res, <-res
	close(stop)
	writes := <-done
	polls, unread := a.polls+b.polls, a.unread+b.unread
	first := a.first + b.first
	assure.Reached(t, polls, "status polls made while the writer ran")
	assure.Reached(t, writes, "writer open/add/close cycles that overlapped the polls")
	if unread > 0 {
		t.Fatalf("%d of %d status polls could not read the healthy, busy index (%d writer cycles); first: %s", unread, polls, writes, first)
	}
}

// covers: MA-398, R18, S29
// The other side of the same moments: status polling a healthy index never
// fails an export's writable open of it. While two status pollers (the real
// Gather) read search.db — each read-only open that closes last leaves the
// -wal/-shm pair, so the next opener runs WAL recovery — an export opening it
// (index.Open, the open every export, Graph run and reindex makes) waits that
// out instead of failing with SQLITE_BUSY_RECOVERY, which failed the whole run
// and recorded it RED run_failed. Positive twin: with no poller the index
// opens for writing.
func TestExportIndexOpenWaitsOutStatusPolls(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(out, "search.db")
	ix, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("positive twin: with no poller the index did not open for writing: %v", err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}

	stop, polled := make(chan struct{}), make(chan int)
	for p := 0; p < 2; p++ {
		go func() {
			n := 0
			for {
				select {
				case <-stop:
					polled <- n
					return
				default:
				}
				health.Gather(out, "")
				n++
			}
		}()
	}
	opens, fails, first := 0, 0, ""
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); opens++ {
		ix, err := index.Open(dbPath)
		if err == nil {
			err = ix.Close()
		}
		if err != nil {
			fails++
			if first == "" {
				first = err.Error()
			}
		}
	}
	close(stop)
	polls := <-polled + <-polled
	assure.Reached(t, polls, "status polls made while the export opened its index")
	assure.Reached(t, opens, "writable opens made while status polled")
	if fails > 0 {
		t.Fatalf("%d of %d writable opens of the healthy index failed while status polled it (%d polls) — each would have failed an export run; first: %s", fails, opens, polls, first)
	}
}
