package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// goneEvent reports whether the history log carries a {k:key, gone:true} event.
func goneEvent(events []state.HistoryEvent, key string) bool {
	for _, ev := range events {
		if ev.K == key && ev.Gone {
			return true
		}
	}
	return false
}

// covers: MA-207, R5, R1, S38, S37
// gone-detection by full reconciliation (§3.4): a message present last run but
// absent from THIS run's full, lock-held walk of every folder is marked gone
// AFTER the walk — a {k,gone} history event and manifest Present=false — while
// its first-captured file is KEPT on disk (a timeline event, not a redaction —
// R13). The gone conclusion is drawn once at the walk's end (with one message no
// checkpoint fires; the partial-run test proves an incomplete walk marks
// nothing). The record keeps its last-known Folder so a past view still shows
// where it lived.
func TestGraphGoneAfterFullWalk(t *testing.T) {
	out := tmpDir(t)
	f, srv := newMoveGraphServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)

	// Run 1: M1 captured in Inbox, Present.
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	key, rec1 := recordForID(t, out, "m1@x")
	if !rec1.Present {
		t.Fatalf("run 1 record is not Present")
	}
	if goneEvent(mustHistory(t, out), key) {
		t.Fatalf("run 1 wrote a gone event for a message that was present")
	}

	// Run 2: M1 has been deleted online — absent from every walked folder.
	f.setM1Folder("none")
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 2: %v", err)
	}

	// The record is marked gone, its file KEPT on disk (R13).
	_, rec2 := recordForID(t, out, "m1@x")
	if rec2.Present {
		t.Errorf("after the deleted-online walk the record is still Present, want gone")
	}
	if rec2.Folder != "Inbox" || rec2.FirstFolder != "Inbox" {
		t.Errorf("the gone record lost its last-known folder: Folder %q / FirstFolder %q, want Inbox/Inbox", rec2.Folder, rec2.FirstFolder)
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Errorf("gone-detection removed the file (%d html files, want 1) — a gone message is a KEPT file, R13", n)
	}
	// The timeline records the departure exactly once.
	events := assure.Reached(t, mustHistory(t, out), "history events")
	if !goneEvent(events, key) {
		t.Errorf("no {k,gone} history event recorded the departure (events: %+v)", events)
	}
	var goneCount int
	for _, ev := range events {
		if ev.K == key && ev.Gone {
			goneCount++
		}
	}
	if goneCount != 1 {
		t.Errorf("gone event recorded %d times, want 1", goneCount)
	}
}

// covers: MA-209, R1, R5, S38, S37
// A message marked gone that RE-APPEARS in a later run emits a present-again
// folder-assertion (the fast-path records an assertion when the pre-observation
// record was not Present, even though the folder is unchanged) and the record
// returns to Present=true — with NO body re-download (its envelope matches, R17).
// The folded timeline then reads: present, then gone, then present again.
func TestGraphPresentAgainRestores(t *testing.T) {
	out := tmpDir(t)
	f, srv := newMoveGraphServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)

	// Run 1: M1 in Inbox.
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	key, _ := recordForID(t, out, "m1@x")

	// Run 2: M1 deleted online → marked gone.
	f.setM1Folder("none")
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	_, recGone := recordForID(t, out, "m1@x")
	if recGone.Present {
		t.Fatalf("run 2 did not mark the deleted message gone")
	}
	if !goneEvent(mustHistory(t, out), key) {
		t.Fatalf("run 2 did not record a gone event")
	}
	goneHits := f.hits("M1")

	// Run 3: M1 re-appears in Inbox → present-again.
	f.setM1Folder("F_IN")
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 3: %v", err)
	}

	// The record is Present again, under its observed folder — no re-download
	// (the envelope matched, so the fast-path recognised it; R17).
	_, rec3 := recordForID(t, out, "m1@x")
	if !rec3.Present {
		t.Errorf("run 3 did not restore Present after re-appearance")
	}
	if rec3.Folder != "Inbox" {
		t.Errorf("re-appeared record Folder = %q, want Inbox", rec3.Folder)
	}
	if f.hits("M1") != goneHits {
		t.Errorf("present-again re-downloaded the body (%d → %d), want none (R17)", goneHits, f.hits("M1"))
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Errorf("present-again re-materialised a file (%d html files, want 1)", n)
	}

	// The timeline is present → gone → present-again: after the gone event there
	// is a later folder-assertion for the key, so a fold to now shows it present.
	events := assure.Reached(t, mustHistory(t, out), "history events")
	sawGone, presentAgain := false, false
	for _, ev := range events {
		switch {
		case ev.K == key && ev.Gone:
			sawGone = true
		case ev.K == key && !ev.Gone && ev.Folder == "Inbox" && sawGone:
			presentAgain = true // a folder-assertion AFTER the gone event
		}
	}
	if !presentAgain {
		t.Errorf("no present-again folder-assertion recorded after the gone event (events: %+v)", events)
	}
	// The fold to now (T2/T4) reflects present-again: present under Inbox.
	fold, err := state.FoldHistory(filepath.Join(out, state.HistoryName), rec3.LastSeen.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if fs, ok := fold[key]; !ok || !fs.Present || fs.Folder != "Inbox" {
		t.Errorf("folded state = %+v (ok=%v), want present under Inbox after present-again", fold[key], ok)
	}
}

// partialWalkServer serves mailbox u1 with an Inbox (F_IN) and an Archive (F_AR).
// Phase 1: Inbox holds M1. Phase 2: M1 is gone (deleted online) and a NEW message
// A1 sits in Inbox; when A1's body is fetched the server DELETES the archive lock
// file, so the checkpoint after A1's export finds the lock lost — the run aborts
// mid-walk before the gone sweep. This proves a partial/aborted run marks nothing
// gone (the deleted M1 must stay Present).
type partialWalkServer struct {
	mu       sync.Mutex
	phase    int
	lockPath string
	deleted  bool
}

func newPartialWalkServer(lockPath string) (*partialWalkServer, *httptest.Server) {
	f := &partialWalkServer{phase: 1, lockPath: lockPath}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[
			{"id":"F_IN","displayName":"Inbox","childFolderCount":0},
			{"id":"F_AR","displayName":"Archive","childFolderCount":0}]}`)
	})
	m1 := `{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}`
	a1 := `{"id":"A1","internetMessageId":"<a1@x>","subject":"subj-A1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-02T09:00:00Z"}`
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		phase := f.phase
		f.mu.Unlock()
		if phase == 1 {
			j(w, `{"value":[`+m1+`]}`)
		} else {
			j(w, `{"value":[`+a1+`]}`) // M1 gone; A1 is new (drives a checkpoint)
		}
	})
	mux.HandleFunc("/users/u1/mailFolders/F_AR/messages", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[]}`)
	})
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u1/messages/"), "/$value")
		// Deleting A1's body triggers the mid-walk lock loss: the checkpoint after
		// A1's export finds the lock gone and the run aborts before the sweep.
		if id == "A1" {
			f.mu.Lock()
			if !f.deleted {
				os.Remove(f.lockPath)
				f.deleted = true
			}
			f.mu.Unlock()
		}
		mid := "<m1@x>"
		subj := "subj-M1"
		if id == "A1" {
			mid, subj = "<a1@x>", "subj-A1"
		}
		w.Write([]byte("From: a@example.com\r\nSubject: " + subj +
			"\r\nMessage-ID: " + mid + "\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody-" + id + "\r\n"))
	})
	return f, httptest.NewServer(mux)
}

func (f *partialWalkServer) setPhase(p int) {
	f.mu.Lock()
	f.phase = p
	f.mu.Unlock()
}

// covers: MA-208, R5, R1, S38, S37
// A partial/aborted run marks NOTHING gone: when the archive lock is lost
// mid-walk (a checkpoint finds it removed), the run returns BEFORE the gone sweep
// (re-checked via abort at the walk's end), so a message deleted online in the
// same run is left Present — an incomplete walk can never distinguish "deleted"
// from "in a not-yet-walked folder", so it concludes nothing (§3.4).
func TestGraphPartialRunMarksNothingGone(t *testing.T) {
	out := tmpDir(t)
	lockPath := filepath.Join(out, lockfile.Name)
	f, srv := newPartialWalkServer(lockPath)
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	// CheckpointEvery=1 so the first new export in run 2 reaches a checkpoint,
	// where the removed lock is detected.
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true, CheckpointEvery: 1}
	logger := log.New(io.Discard, "", 0)

	// Run 1: M1 captured in Inbox, Present.
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	key, rec1 := recordForID(t, out, "m1@x")
	if !rec1.Present {
		t.Fatalf("run 1 record is not Present")
	}

	// Run 2: M1 deleted online; A1 new in Inbox; the lock is removed during A1's
	// fetch, so the run aborts mid-walk.
	f.setPhase(2)
	_, err := RunGraph(context.Background(), g, opts, logger)
	if err == nil {
		t.Fatal("run 2 did not fail though the lock was lost mid-walk")
	}
	if !strings.Contains(err.Error(), lockfile.Name) {
		t.Errorf("the abort error does not name the lock: %v", err)
	}

	// The deleted-online M1 was NOT marked gone: a partial walk concludes nothing.
	_, rec2 := recordForID(t, out, "m1@x")
	if !rec2.Present {
		t.Errorf("a partial/aborted run marked a message gone — an incomplete walk must conclude nothing (§3.4)")
	}
	if goneEvent(mustHistory(t, out), key) {
		t.Errorf("a partial/aborted run wrote a gone event — no shared conclusion may be drawn from an incomplete walk")
	}
}
