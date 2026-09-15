package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// moveGraphServer serves mailbox u1 with an Inbox (F_IN) and an Archive (F_AR)
// holding a single message M1. Which folder M1 is listed under is controlled by
// m1Folder, so a test can MOVE it between runs; $value fetches are counted so the
// test can prove the move is recorded WITHOUT re-downloading the body. A second
// message D (a DISTINCT message reusing M1's internetMessageId in a third run) is
// listed only when distinct is set, so one server drives both the move test and
// the id-reuse-split test.
type moveGraphServer struct {
	mu       sync.Mutex
	mimeHits map[string]int
	m1Folder string // "F_IN" or "F_AR"
	distinct bool   // when true, F_SE also lists D (same id <m1@x>, different subject)
	honor    bool   // when true, honor the immutable-id preference (emit Preference-Applied), so ref.PhysID = the message id
}

func newMoveGraphServer() (*moveGraphServer, *httptest.Server) {
	f := &moveGraphServer{mimeHits: map[string]int{}, m1Folder: "F_IN"}
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
			{"id":"F_AR","displayName":"Archive","childFolderCount":0},
			{"id":"F_SE","displayName":"Sent","childFolderCount":0}]}`)
	})
	// M1's listing entry, carrying the widened envelope $select so a signature is
	// computable pre-download. It is listed under whichever folder m1Folder names.
	m1 := `{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}`
	list := func(folderID string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			here := f.m1Folder == folderID
			distinct := f.distinct
			honor := f.honor
			f.mu.Unlock()
			if honor && strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "immutableid") {
				w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
			}
			var entries []string
			if here {
				entries = append(entries, m1)
			}
			// D reuses M1's internetMessageId but is a DIFFERENT message (its
			// subject differs), listed under Sent when distinct is on.
			if distinct && folderID == "F_SE" {
				entries = append(entries, `{"id":"D","internetMessageId":"<m1@x>","subject":"subj-D-distinct","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-09T09:00:00Z"}`)
			}
			j(w, `{"value":[`+strings.Join(entries, ",")+`]}`)
		}
	}
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", list("F_IN"))
	mux.HandleFunc("/users/u1/mailFolders/F_AR/messages", list("F_AR"))
	mux.HandleFunc("/users/u1/mailFolders/F_SE/messages", list("F_SE"))
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u1/messages/"), "/$value")
		f.mu.Lock()
		f.mimeHits[id]++
		f.mu.Unlock()
		// D and M1 share the internetMessageId but differ in subject (a distinct
		// message that reused the id).
		subj := "subj-M1"
		if id == "D" {
			subj = "subj-D-distinct"
		}
		w.Write([]byte("From: a@example.com\r\nSubject: " + subj +
			"\r\nMessage-ID: <m1@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody-" + id + "\r\n"))
	})
	return f, httptest.NewServer(mux)
}

func (f *moveGraphServer) hits(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mimeHits[id]
}

func (f *moveGraphServer) setM1Folder(folderID string) {
	f.mu.Lock()
	f.m1Folder = folderID
	f.mu.Unlock()
}

func (f *moveGraphServer) setHonorImmutable(b bool) {
	f.mu.Lock()
	f.honor = b
	f.mu.Unlock()
}

func (f *moveGraphServer) setDistinct(b bool) {
	f.mu.Lock()
	f.distinct = b
	f.mu.Unlock()
}

// loadManifest reads the archive manifest and returns the single record whose
// key carries the given identity substring (fails if not exactly one).
func recordForID(t *testing.T, out, idSubstr string) (string, state.Record) {
	t.Helper()
	m, err := state.Load(out + "/.mailarchive-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var key string
	var rec state.Record
	n := 0
	for k, r := range m.All() {
		if strings.Contains(k, idSubstr) {
			key, rec = k, r
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one record for %q, found %d", idSubstr, n)
	}
	return key, rec
}

// indexFolder returns the search index's folder facet for a single-message
// archive (the current folder its one docs row holds); "" if not exactly one.
func indexFolder(t *testing.T, out string) string {
	t.Helper()
	ix, err := index.OpenReadonly(out + "/search.db")
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	fcs, err := ix.Folders()
	if err != nil {
		t.Fatal(err)
	}
	if len(fcs) == 1 {
		return fcs[0].Folder
	}
	return ""
}

// covers: MA-202, R17, R3, R1, R5, S38
// An incremental Graph re-run over a message MOVED between folders records the
// move as a folder change on the ONE record (Folder follows, FirstFolder and the
// physical file stay put — R13), a history folder-assertion event, and a body-
// free index folder update — and downloads NO body (the envelope signature
// matches, so the fast-path knows it is the same message). One physical file, not
// two.
func TestGraphMoveDedupAndTimeline(t *testing.T) {
	out := tmpDir(t)
	f, srv := newMoveGraphServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)

	// Run 1: M1 captured in Inbox.
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Fatalf("run 1 wrote %d html files, want 1", n)
	}
	if f.hits("M1") != 1 {
		t.Fatalf("run 1 fetched M1 %d times, want 1", f.hits("M1"))
	}
	key1, rec1 := recordForID(t, out, "m1@x")
	if rec1.Folder != "Inbox" || rec1.FirstFolder != "Inbox" {
		t.Fatalf("run 1 record folders = %q/%q, want Inbox/Inbox", rec1.Folder, rec1.FirstFolder)
	}

	// Run 2: M1 has moved to Archive.
	f.setM1Folder("F_AR")
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 2: %v", err)
	}

	// ONE file, not re-downloaded.
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Errorf("after the move there are %d html files, want 1 (the message must not be re-archived)", n)
	}
	if f.hits("M1") != 1 {
		t.Errorf("M1 body was re-downloaded on the move (%d fetches), want 1 — a move costs no download (R17)", f.hits("M1"))
	}

	// The ONE record's current folder followed the move; FirstFolder and the
	// physical file stayed put (R13).
	key2, rec2 := recordForID(t, out, "m1@x")
	if key2 != key1 {
		t.Errorf("the move changed the record's key %q → %q (it must stay the mailbox-wide key)", key1, key2)
	}
	if rec2.Folder != "Archive" {
		t.Errorf("record current Folder = %q after the move, want Archive", rec2.Folder)
	}
	if rec2.FirstFolder != "Inbox" {
		t.Errorf("record FirstFolder = %q, want Inbox (the first-captured folder never moves, R13)", rec2.FirstFolder)
	}
	if rec2.Path != rec1.Path {
		t.Errorf("the physical file moved (%q → %q); a normal run never relocates a file (R13)", rec1.Path, rec2.Path)
	}
	if !strings.HasPrefix(rec2.Path, "u1/Inbox/") {
		t.Errorf("the file %q is not under its first-captured Inbox folder (R13)", rec2.Path)
	}

	// The search index's folder column followed the move (a body-free update).
	if fol := indexFolder(t, out); fol != "Archive" {
		t.Errorf("index folder column = %q after the move, want Archive", fol)
	}

	// The history log records the move as a folder-assertion event under Archive.
	events := assure.Reached(t, mustHistory(t, out), "history events")
	var sawMove bool
	for _, ev := range events {
		if ev.K == key2 && ev.Folder == "Archive" && !ev.Gone {
			sawMove = true
		}
	}
	if !sawMove {
		t.Errorf("no history folder-assertion recorded the move to Archive (events: %+v)", events)
	}
}

// covers: MA-203, R1, R3, R17, S38
// #8 CLOSED ON GRAPH (design closure rev-6.1 §4/§5) — was the deferred floor
// residual, now a PERMANENT GUARD. The live Graph path honors immutable ids, so a
// DISTINCT message reusing an already-archived Message-ID carries a DIFFERENT
// immutable id: the fast-path finds no sibling with that id, DOWNLOADS it, and the
// exporter's byte compare #fp-splits it under a PhysID-hash key — both survive
// (R1), while the unchanged original is skipped by its matched id (not
// re-downloaded, R17). This was a t.Skip XFAIL encoder under the option-D floor
// while EC6 was unconfirmed; EC6 passed 2026-09-14 and the closure is built, so the
// skip is REMOVED and this now asserts the capture. The same R1 split on the
// LOCAL/full path is TestMailboxWideIDReuseSplit.
func TestGraphDistinctIDMailboxWideSplit(t *testing.T) {
	out := tmpDir(t)
	f, srv := newMoveGraphServer()
	defer srv.Close()
	f.setHonorImmutable(true)

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)

	// Run 1: M1 captured in Inbox.
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}

	// Run 2: M1 still in Inbox (unchanged) AND a distinct message D reuses <m1@x>
	// in Sent.
	f.setDistinct(true)
	r2, err := RunGraph(context.Background(), g, opts, logger)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}

	// D was downloaded (the envelope mismatched); M1 was not re-downloaded.
	if f.hits("D") != 1 {
		t.Errorf("D fetched %d times, want 1 (a distinct id-reuse must be downloaded)", f.hits("D"))
	}
	if f.hits("M1") != 1 {
		t.Errorf("M1 re-downloaded (%d) while unchanged — its envelope matched, so it must skip", f.hits("M1"))
	}
	if r2.Stats.Exported != 1 {
		t.Errorf("run 2 exported %d, want 1 (only the distinct D)", r2.Stats.Exported)
	}

	// Both survive: two files, two records under the shared identity.
	if n := countFiles(t, out, ".html", "index.html"); n != 2 {
		t.Errorf("html files = %d, want 2 (M1 + the distinct D both kept, R1)", n)
	}
	m, err := state.Load(out + "/.mailarchive-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	refs := assure.Reached(t, m.KeysForIdentity("mid:m1@x"), "records for mid:m1@x")
	if len(refs) != 2 {
		t.Fatalf("identity mid:m1@x has %d records, want 2 (a distinct reuse must not be dropped, R1)", len(refs))
	}
	// One sits at the base mailbox-wide key, the other at a #fp-qualified sibling.
	base := 0
	for _, ref := range refs {
		if strings.Count(ref.Key, "\x00") == 1 {
			base++
		}
	}
	if base != 1 {
		t.Errorf("want exactly one base (unqualified) key among %v, got %d", refs, base)
	}
}

// covers: MA-204, R2, R3, R17, S38
// A FULL Graph run re-exports everything (R2) but the mailbox-wide keying
// re-materialises NO per-folder duplicate: the file/record counts are unchanged
// while every body is re-downloaded.
func TestGraphFullRunDedupsMailboxWide(t *testing.T) {
	out := tmpDir(t)
	f, srv := newFakeGraphServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	logger := log.New(io.Discard, "", 0)

	r1, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if r1.ManifestSize != 3 || countFiles(t, out, ".html", "index.html") != 3 {
		t.Fatalf("run 1 manifest=%d files=%d, want 3/3", r1.ManifestSize, countFiles(t, out, ".html", "index.html"))
	}
	firstHits := f.hits()

	// Full re-run: re-exports all (R2) but must not duplicate any record/file.
	r2, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Full, Index: true, Pages: true}, logger)
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	if r2.Stats.Exported != 3 {
		t.Errorf("full run exported %d, want 3 (full re-exports all, R2)", r2.Stats.Exported)
	}
	if r2.ManifestSize != 3 {
		t.Errorf("full run manifest = %d, want 3 (no mailbox-wide duplicate materialised)", r2.ManifestSize)
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 3 {
		t.Errorf("full run left %d html files, want 3 (no per-folder duplicate)", n)
	}
	// Full genuinely re-downloaded (R2), unlike an incremental skip.
	reDownloaded := false
	for id, n := range f.hits() {
		if n > firstHits[id] {
			reDownloaded = true
		}
	}
	if !reDownloaded {
		t.Error("a full run re-downloaded nothing — full must re-export all (R2)")
	}
}

// covers: MA-205, R5, S38
// The crash-safe durability order is history append+fsync → index flush →
// manifest.Save (the trailing anchor), the single definition commit wires the
// real stores to. commitInOrder runs the steps in that order; a nil step is
// skipped and the manifest still anchors last — so a one-shot local run (no
// history writer) still saves last, and on the live path the manifest never
// advances past the history events explaining it.
func TestCommitCrashOrder(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	var order []string
	commitInOrder(
		func() error { order = append(order, "history"); return nil },
		func() error { order = append(order, "index"); return nil },
		func() error { order = append(order, "manifest"); return nil },
		logger)
	if want := []string{"history", "index", "manifest"}; !reflect.DeepEqual(order, want) {
		t.Errorf("commit order = %v, want %v (manifest is the trailing anchor)", order, want)
	}
	if order[len(order)-1] != "manifest" {
		t.Errorf("manifest must be written LAST (the fold-to-now anchor), order was %v", order)
	}

	// A nil history step (a local run keeps no timeline) is skipped, and the
	// manifest still anchors last.
	var order2 []string
	commitInOrder(nil,
		func() error { order2 = append(order2, "index"); return nil },
		func() error { order2 = append(order2, "manifest"); return nil },
		logger)
	if want := []string{"index", "manifest"}; !reflect.DeepEqual(order2, want) {
		t.Errorf("with no history writer, order = %v, want %v", order2, want)
	}
}

func mustHistory(t *testing.T, out string) []state.HistoryEvent {
	t.Helper()
	ev, err := state.ReadHistory(out + "/" + state.HistoryName)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	return ev
}

// noMIDGraphServer serves mailbox u1 with an Inbox (F_IN) and an Archive (F_AR)
// holding a single message N1 that carries NO internetMessageId in the listing
// and NO Message-ID header in its MIME — so its identity is the post-download
// content hash, not a Message-ID. Which folder N1 is listed under is controlled
// by n1Folder so a test can MOVE it between runs; $value fetches are counted so
// the test can show a no-mid message is re-downloaded each run (the acknowledged
// price) yet still deduped and its move recorded at the exporter's skip. The MIME
// is identical whatever folder N1 sits in, so the content hash — and thus the
// dedup identity — is stable across runs.
type noMIDGraphServer struct {
	mu       sync.Mutex
	mimeHits int
	n1Folder string // "F_IN" or "F_AR"
}

func newNoMIDGraphServer() (*noMIDGraphServer, *httptest.Server) {
	f := &noMIDGraphServer{n1Folder: "F_IN"}
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
	// N1's listing entry carries the envelope $select but NO internetMessageId, so
	// the pre-download fast-path cannot recognise it — it is downloaded and deduped
	// on its content hash at the exporter.
	n1 := `{"id":"N1","subject":"subj-N1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}`
	list := func(folderID string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			here := f.n1Folder == folderID
			f.mu.Unlock()
			var entries []string
			if here {
				entries = append(entries, n1)
			}
			j(w, `{"value":[`+strings.Join(entries, ",")+`]}`)
		}
	}
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", list("F_IN"))
	mux.HandleFunc("/users/u1/mailFolders/F_AR/messages", list("F_AR"))
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.mimeHits++
		f.mu.Unlock()
		// No Message-ID header: the parsed message falls back to a content hash for
		// its identity. The bytes are identical every run so the hash is stable.
		w.Write([]byte("From: a@example.com\r\nSubject: subj-N1\r\n" +
			"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody-N1\r\n"))
	})
	return f, httptest.NewServer(mux)
}

func (f *noMIDGraphServer) hits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mimeHits
}

func (f *noMIDGraphServer) setFolder(folderID string) {
	f.mu.Lock()
	f.n1Folder = folderID
	f.mu.Unlock()
}

// covers: MA-206, R3, R5, S38
// A no-Internet-Message-ID Graph message is deduped mailbox-wide on its
// post-download content hash. Because its identity is only known AFTER download,
// it reaches the exporter's mailbox-wide skip rather than the pre-download
// fast-path — so it IS re-downloaded each run (the acknowledged no-mid price),
// but every observation, INCLUDING the write-nothing skip, stamps LastSeen=thisRun
// and, when the folder differs, records the move (Folder field + a history
// folder-assertion + a body-free index folder update). Without that stamp a
// still-present no-mid message's LastSeen would never advance (gone-detection
// would wrongly bury it) and a moved one's folder would never follow (§3.4/§3.6).
func TestGraphNoMessageIDMoveDedupAndTimeline(t *testing.T) {
	out := tmpDir(t)
	f, srv := newNoMIDGraphServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)

	// Run 1: N1 captured in Inbox (downloaded once, no Message-ID).
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Fatalf("run 1 wrote %d html files, want 1", n)
	}
	if f.hits() != 1 {
		t.Fatalf("run 1 fetched N1 %d times, want 1", f.hits())
	}
	key1, rec1 := recordForID(t, out, "sha:")
	if rec1.Folder != "Inbox" || rec1.FirstFolder != "Inbox" || !rec1.Present {
		t.Fatalf("run 1 record = Folder %q / FirstFolder %q / Present %v, want Inbox/Inbox/true", rec1.Folder, rec1.FirstFolder, rec1.Present)
	}

	// Run 2: N1 unchanged, still in Inbox. It is re-downloaded (no id to skip on)
	// and deduped at the exporter — ONE file still — and the write-nothing skip
	// stamps a FRESH LastSeen so gone-detection will see it as seen-this-run.
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Errorf("run 2 left %d html files, want 1 (a no-mid message must dedup, not duplicate)", n)
	}
	if f.hits() != 2 {
		t.Errorf("N1 fetched %d times after run 2, want 2 (a no-mid message is re-downloaded each run — its identity is only known post-download)", f.hits())
	}
	_, rec2 := recordForID(t, out, "sha:")
	if !rec2.LastSeen.After(rec1.LastSeen) {
		t.Errorf("run 2 did not advance LastSeen (%v → %v); a skip must still stamp LastSeen=thisRun or a still-present message is wrongly marked gone (§3.4)", rec1.LastSeen, rec2.LastSeen)
	}
	if rec2.Folder != "Inbox" || !rec2.Present {
		t.Errorf("run 2 record = Folder %q / Present %v, want Inbox/true", rec2.Folder, rec2.Present)
	}

	// Run 3: N1 has moved to Archive. Still one file at its first-captured Inbox
	// folder (R13); the skip records the move on the ONE record.
	f.setFolder("F_AR")
	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Errorf("after the move there are %d html files, want 1 (a no-mid move must not re-archive)", n)
	}
	key3, rec3 := recordForID(t, out, "sha:")
	if key3 != key1 {
		t.Errorf("the move changed the record's key %q → %q (it must stay the mailbox-wide key)", key1, key3)
	}
	if rec3.Folder != "Archive" {
		t.Errorf("record current Folder = %q after the no-mid move, want Archive (the skip must follow the move, §3.6)", rec3.Folder)
	}
	if rec3.FirstFolder != "Inbox" {
		t.Errorf("record FirstFolder = %q, want Inbox (the first-captured folder never moves, R13)", rec3.FirstFolder)
	}
	if rec3.Path != rec1.Path || !strings.HasPrefix(rec3.Path, "u1/Inbox/") {
		t.Errorf("the physical file moved (%q → %q); a normal run never relocates a file (R13)", rec1.Path, rec3.Path)
	}
	if fol := indexFolder(t, out); fol != "Archive" {
		t.Errorf("index folder column = %q after the no-mid move, want Archive", fol)
	}
	events := assure.Reached(t, mustHistory(t, out), "history events")
	var sawMove bool
	for _, ev := range events {
		if ev.K == key3 && ev.Folder == "Archive" && !ev.Gone {
			sawMove = true
		}
	}
	if !sawMove {
		t.Errorf("no history folder-assertion recorded the no-mid move to Archive (events: %+v)", events)
	}
}

// covers: MA-204, R2, R3, S38
// A FULL Graph run over a message that has already MOVED re-materialises no
// duplicate: it re-downloads and re-writes the ONE file at its first-captured
// folder (R13, the exporter's writeFolderPath = FirstFolder branch) while the
// record's current folder stays the moved-to folder and the index folder follows.
func TestGraphFullRunOverMovedMessage(t *testing.T) {
	out := tmpDir(t)
	f, srv := newMoveGraphServer()
	defer srv.Close()

	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	logger := log.New(io.Discard, "", 0)

	// Run 1 (incremental): M1 captured in Inbox.
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	// Run 2 (incremental): M1 moved to Archive — recorded as a move, one file kept.
	f.setM1Folder("F_AR")
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	_, recMoved := recordForID(t, out, "m1@x")
	if recMoved.Folder != "Archive" || recMoved.FirstFolder != "Inbox" {
		t.Fatalf("after the move: Folder %q / FirstFolder %q, want Archive/Inbox", recMoved.Folder, recMoved.FirstFolder)
	}
	movedHits := f.hits("M1")

	// Run 3 (FULL): M1 still in Archive. Full re-exports (R2) but must not
	// re-materialise a per-folder duplicate; the file is rewritten at its
	// first-captured Inbox folder while the current folder stays Archive.
	r3, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Full, Index: true, Pages: true}, logger)
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	if r3.Stats.Exported != 1 {
		t.Errorf("full run exported %d, want 1 (full re-exports the moved message, R2)", r3.Stats.Exported)
	}
	if f.hits("M1") <= movedHits {
		t.Errorf("full run re-downloaded nothing (%d then %d) — full must re-export (R2)", movedHits, f.hits("M1"))
	}
	if n := countFiles(t, out, ".html", "index.html"); n != 1 {
		t.Errorf("full run over a moved message left %d html files, want 1 (no per-folder duplicate)", n)
	}
	key3, rec3 := recordForID(t, out, "m1@x")
	if rec3.Folder != "Archive" {
		t.Errorf("full run reset the current folder to %q, want Archive (the move must survive a full re-export)", rec3.Folder)
	}
	if rec3.FirstFolder != "Inbox" || !strings.HasPrefix(rec3.Path, "u1/Inbox/") {
		t.Errorf("full run relocated the file: FirstFolder %q, path %q, want Inbox / under u1/Inbox/ (R13)", rec3.FirstFolder, rec3.Path)
	}
	if fol := indexFolder(t, out); fol != "Archive" {
		t.Errorf("index folder column = %q after the full run, want Archive", fol)
	}
	_ = key3
}
