package keyring

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The encrypted archive's reader (design-encrypted-archives-phase3 §7; P3-2a-2,
// MA-796).

// treeOf is every file under out, by path, with its bytes.
func treeOf(t *testing.T, out string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(out, p)
			tree[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return tree
}

// sampleMbox is two messages, kept with their originals.
const sampleMbox = "From - Mon Jan 01 00:00:00 2024\r\n" +
	"From: Bob <bob@example.com>\r\nSubject: Hello HTML\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n" +
	"Message-ID: <abc@example.com>\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
	"<p>Hello <b>world</b> invoice</p>\r\n\r\n" +
	"From - Tue Jan 02 00:00:00 2024\r\n" +
	"From: Carol <carol@example.com>\r\nSubject: Plain note\r\nDate: Tue, 02 Jan 2024 09:00:00 +0000\r\n" +
	"Message-ID: <def@example.com>\r\n\r\njust plain text here\r\n"

// readBackup runs a keyed backup of the sample mailbox into u's archive, with its
// search index and the originals.
func readBackup(t *testing.T, u *Unlocked) {
	t.Helper()
	box := filepath.Join(t.TempDir(), "Inbox")
	if err := os.WriteFile(box, []byte(sampleMbox), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := app.Options{Inputs: []string{box}, Out: u.Out(), Index: true, KeepRaw: true}
	done, err := Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, err := app.Run(context.Background(), opts, quiet, nil); err != nil {
		t.Fatal(err)
	}
}

// readerFor backs up into a new encrypted archive and opens it to read, returning
// the folder, the read open and a test server over its reader.
func readerFor(t *testing.T, o ReaderOptions) (string, *Unlocked, *Reader, *httptest.Server) {
	t.Helper()
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Destroy)
	if o.LockedNote == "" {
		o.LockedNote = "start mailarchive serve again to unlock it"
	}
	r, err := ReaderSource(u, o)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Serve(r))
	t.Cleanup(ts.Close)
	return out, u, r, ts
}

// loadAgain makes the reader's next ensure load the record again.
func loadAgain(r *Reader) {
	r.mu.Lock()
	r.stale = true
	r.mu.Unlock()
}

// get fetches path without following a redirect.
func get(t *testing.T, ts *httptest.Server, path string) (int, http.Header, string) {
	t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

// aMessage is one record's page path in the reader's view.
func aMessage(t *testing.T, r *Reader) state.Record {
	t.Helper()
	v, err := r.ensure()
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range v.m.All() {
		if rec.Sealed != nil && rec.Sealed.HTML != nil && rec.Sealed.EML != nil {
			return rec
		}
	}
	t.Fatal("fixture: no record with a page and an original")
	return state.Record{}
}

// covers: MA-796, R37
// The reader serves an encrypted archive from its sealed record, writing nothing:
// the root and folder pages rendered in memory and worded for an encrypted archive,
// a message's page, its original pinned as message/rfc822, search, facets and
// go-back — every file in the folder left as it was, no page written to disk.
func TestTheReaderServesAnEncryptedArchiveWritingNothing(t *testing.T) {
	out, _, r, ts := readerFor(t, ReaderOptions{})
	before := treeOf(t, out)

	code, _, root := get(t, ts, "/files/")
	if code != 200 || !strings.Contains(root, "An encrypted archive") || strings.Contains(root, "README.txt") {
		t.Fatalf("the root page: %d\n%s", code, root)
	}
	if code, h, _ := get(t, ts, "/files/index.html"); code != http.StatusMovedPermanently || h.Get("Location") != "./" {
		t.Fatalf("index.html is sent to its folder: %d %v", code, h)
	}
	link := regexp.MustCompile(`href="([^"]+/index\.html)"`).FindStringSubmatch(root)
	if link == nil {
		t.Fatalf("no folder link on the root page:\n%s", root)
	}
	folder := strings.TrimSuffix(link[1], "index.html")
	if code, _, page := get(t, ts, "/files/"+folder); code != 200 || !strings.Contains(page, ".html") {
		t.Fatalf("the folder page %s: %d", folder, code)
	}
	if code, h, _ := get(t, ts, "/files/"+strings.TrimSuffix(folder, "/")); code != http.StatusMovedPermanently || !strings.HasSuffix(h.Get("Location"), "/") {
		t.Fatalf("a folder named without its slash is sent to it: %d %v", code, h)
	}

	rec := aMessage(t, r)
	code, h, page := get(t, ts, "/files/"+rec.Path)
	if code != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/html") || len(page) == 0 {
		t.Fatalf("a message page: %d %v", code, h)
	}
	eml := strings.TrimSuffix(rec.Path, ".html") + ".eml"
	if code, h, body := get(t, ts, "/files/"+eml); code != 200 || h.Get("Content-Type") != "message/rfc822" || !strings.Contains(body, "Message-ID: <") {
		t.Fatalf("an original: %d %v\n%s", code, h, body)
	}
	if code, _, body := get(t, ts, "/api/facets"); code != 200 || !strings.Contains(body, `"total"`) {
		t.Fatalf("facets: %d %s", code, body)
	}
	if code, _, body := get(t, ts, "/api/search?q="); code != 200 || !strings.Contains(body, `"results"`) {
		t.Fatalf("search: %d %s", code, body)
	}
	if code, _, body := get(t, ts, "/goback"); code != 200 || !strings.Contains(body, "Go back in time") {
		t.Fatalf("go-back: %d", code)
	}
	assure.Reached(t, reflect.DeepEqual(treeOf(t, out), before), "nothing written in the archive folder")
}

// covers: MA-796, R37
// The reader refuses what a plaintext reader refuses, and serves nothing of the
// sealed store: dotfiles, the key files, the unit store, the record's chunks, a
// path that names no record; and it only reads — any other method is 405 (§7.1).
// (README.txt is the note, served since P3-5: TestTheReaderServesTheProgramsOwnNote.)
func TestTheReaderServesNothingButTheArchivesMessages(t *testing.T) {
	out, _, _, ts := readerFor(t, ReaderOptions{})
	var unit string
	filepath.WalkDir(filepath.Join(out, "units"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && unit == "" {
			unit, _ = filepath.Rel(out, p)
		}
		return nil
	})
	for _, p := range []string{"/files/.mailarchive-manifest.json", "/files/" + vault.FileName, "/files/" + filepath.ToSlash(unit),
		"/files/units/", "/files/chunks/", "/files/search.db", "/files/Inbox/nothing.html", "/files/a/b.txt"} {
		if code, _, _ := get(t, ts, p); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}
	resp, err := http.Post(ts.URL+"/api/search", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	assure.Reached(t, resp.StatusCode == http.StatusMethodNotAllowed, "a POST refused")
}

// covers: MA-796, R37
// Once locked — the reader, or the open it reads through — every request gets the
// locked page, saying how to unlock it, never a damage message: the search page,
// a message, go-back, and the API's JSON (§3.2, §7.1).
func TestALockedReaderShowsTheLockedPage(t *testing.T) {
	for name, lock := range map[string]func(*Unlocked, *Reader){
		"the reader": func(_ *Unlocked, r *Reader) { r.Lock() },
		"the open":   func(u *Unlocked, _ *Reader) { u.Lock() },
	} {
		t.Run(name, func(t *testing.T) {
			_, u, r, ts := readerFor(t, ReaderOptions{LockedNote: "start serve again"})
			rec := aMessage(t, r)
			if code, _, _ := get(t, ts, "/files/"+rec.Path); code != 200 {
				t.Fatalf("positive twin: %d", code)
			}
			lock(u, r)
			for _, p := range []string{"/", "/files/" + rec.Path, "/files/", "/goback"} {
				code, _, body := get(t, ts, p)
				if code != http.StatusServiceUnavailable || !strings.Contains(body, "locked") || !strings.Contains(body, "start serve again") || strings.Contains(body, "damaged") {
					t.Errorf("%s: %d\n%s", p, code, body)
				}
			}
			code, _, body := get(t, ts, "/api/search?q=x")
			assure.Reached(t, code == http.StatusServiceUnavailable && strings.Contains(body, `"locked":true`), "the API says locked")
		})
	}
}

// covers: MA-796, R37
// A lock landing while requests are in flight never reads as damage: each answer is
// the content or the locked page, and the key is destroyed once the last decrypt
// ends; run under the race detector in CI (§3.2's owed test).
func TestALockDuringRequestsNeverReadsAsDamage(t *testing.T) {
	_, u, r, ts := readerFor(t, ReaderOptions{})
	rec := aMessage(t, r)
	paths := []string{"/files/" + rec.Path, "/files/" + strings.TrimSuffix(rec.Path, ".html") + ".eml", "/api/search?q=hello", "/goback", "/files/"}
	stop := make(chan struct{})
	bad := make(chan string, 64)
	done := make(chan struct{})
	for i := 0; i < 6; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				p := paths[(i+n)%len(paths)]
				resp, err := http.Get(ts.URL + p)
				if err != nil {
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 && !(resp.StatusCode == http.StatusServiceUnavailable && strings.Contains(string(body), "locked")) {
					bad <- p + ": " + resp.Status + " " + string(body)
				}
			}
		}(i)
	}
	time.Sleep(150 * time.Millisecond)
	u.Lock()
	time.Sleep(150 * time.Millisecond)
	close(stop)
	for i := 0; i < 6; i++ {
		<-done
	}
	close(bad)
	for b := range bad {
		t.Error(b)
	}
	code, _, _ := get(t, ts, "/files/"+rec.Path)
	assure.Reached(t, code == http.StatusServiceUnavailable, "locked after the lock")
}

// covers: MA-796, R37
// A lock landing between a request's view and its unit read is the locked page,
// not a damage message: the key's loss is told as a lock wherever it is met (§3.2).
func TestALockMidRequestIsTheLockedPage(t *testing.T) {
	_, u, r, ts := readerFor(t, ReaderOptions{LockedNote: "start serve again"})
	rec := aMessage(t, r)
	orig := readerBefore
	readerBefore = func() { u.Lock() }
	defer func() { readerBefore = orig }()
	code, _, body := get(t, ts, "/files/"+rec.Path)
	assure.Reached(t, code == http.StatusServiceUnavailable && strings.Contains(body, "start serve again") && !strings.Contains(body, "could not be read"),
		"the locked page")
}

// covers: MA-796, R21
// Go-back on an encrypted archive: a record is present when it has a page unit — a
// stub or a gap has none and shows at no date; the timeline is told unavailable
// when the record holds no history chunk, in encrypted wording (§7.2).
func TestGoBackOnAnEncryptedArchive(t *testing.T) {
	_, _, r, ts := readerFor(t, ReaderOptions{})
	rec := aMessage(t, r)
	stub := rec
	stub.Sealed = &state.SealedSet{}
	if !sealedPresent(rec) || sealedPresent(stub) || sealedPresent(state.Record{}) {
		t.Fatal("presence is not a page unit")
	}
	g, err := r.GoBack()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := r.ensure()
	_, _, body := get(t, ts, "/goback")
	if len(v.store.Chunks(vault.ContainerHistory)) == 0 {
		if g.Timeline != server.TimelineUnavailable || !strings.Contains(body, "Go back in time is not available for this archive yet.") || strings.Contains(body, ".jsonl") {
			t.Fatalf("no history: timeline %v\n%s", g.Timeline, body)
		}
	} else if g.Timeline == server.TimelineUnavailable {
		t.Fatal("a history told unavailable")
	}
	assure.Reached(t, g.Encrypted && g.Records != nil, "go-back read the sealed record")
}

// covers: MA-796, R37
// A message the memory budget could never hold is refused in plain words, holding
// nothing (§7.4).
func TestAMessageTooLargeForTheBudgetIsRefusedInPlainWords(t *testing.T) {
	g := memguard.New(1)
	_, _, r, ts := readerFor(t, ReaderOptions{Guard: g})
	rec := aMessage(t, r)
	code, _, body := get(t, ts, "/files/"+rec.Path)
	assure.Refused(t, map[bool]int{true: 1}[code == http.StatusServiceUnavailable], body, assure.Code(1),
		assure.Names("This message is too large to open on this computer", "MB"),
		assure.NoSideEffect(func() bool { return g.Used() == 0 }))
}

// covers: MA-796, R37
// A unit that is gone is reported as not found, never as damage; it is looked for
// once more in a fresh load only when the record moved since the view was loaded —
// asking again for a unit gone from a record that holds still, or for one whose
// bytes are not its record's, loads nothing (§7.3; the P3-2a pass, OUT-D).
func TestAMissingUnitIsNotFoundNotDamage(t *testing.T) {
	out, u, r, ts := readerFor(t, ReaderOptions{})
	rec := aMessage(t, r)
	k, _, err := u.acquire()
	if err != nil {
		t.Fatal(err)
	}
	units := &export.Units{Root: out, Key: k}
	pageRel, err := units.Locate(rec.Unit, vault.UnitMessage, *rec.Sealed.HTML)
	emlRel, err2 := units.Locate(rec.Unit, vault.UnitOriginal, *rec.Sealed.EML)
	u.release()
	if err != nil || err2 != nil {
		t.Fatal(err, err2)
	}
	loads := 0
	orig := readerLoad
	readerLoad = func() { loads++ }
	defer func() { readerLoad = orig }()

	os.Remove(filepath.Join(out, pageRel))
	for i := 0; i < 3; i++ {
		if code, _, body := get(t, ts, "/files/"+rec.Path); code != http.StatusNotFound || strings.Contains(body, "damaged") {
			t.Fatalf("a unit gone: %d %s", code, body)
		}
	}
	b, _ := os.ReadFile(filepath.Join(out, emlRel))
	b[len(b)-1] ^= 1
	os.WriteFile(filepath.Join(out, emlRel), b, 0o600)
	eml := strings.TrimSuffix(rec.Path, ".html") + ".eml"
	if code, _, _ := get(t, ts, "/files/"+eml); code != http.StatusInternalServerError || loads != 0 {
		t.Fatalf("a unit whose bytes are not its record's: %d, %d loads", code, loads)
	}

	// The record moved since the view was loaded: looked for once more.
	w, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	readBackup(t, w)
	w.Destroy()
	code, _, _ := get(t, ts, "/files/"+rec.Path)
	assure.Reached(t, code == http.StatusNotFound && loads == 1, "loaded once more, then not found")
}

// covers: MA-796, R37
// The reader sees a backup's commit: a peek after two seconds finds it and the
// record loads again; a commit landing during a load makes it load once more, and a
// second one in a row is "a backup is running; try again in a moment" (§7.3).
func TestTheReaderSeesABackupsCommit(t *testing.T) {
	out, _, r, _ := readerFor(t, ReaderOptions{})
	now := time.Now()
	origNow := readerNow
	readerNow = func() time.Time { return now }
	defer func() { readerNow = origNow }()
	v1, err := r.ensure()
	if err != nil {
		t.Fatal(err)
	}
	commit := func() {
		w, err := Open(out, Password(testPassword), OpenOptions{Write: true})
		if err != nil {
			t.Fatal(err)
		}
		defer w.Destroy()
		readBackup(t, w)
	}
	commit()
	if v, _ := r.ensure(); v != v1 {
		t.Fatal("the record loaded again before two seconds passed")
	}
	now = now.Add(3 * time.Second)
	v2, err := r.ensure()
	if err != nil || v2 == v1 || v2.peek == v1.peek {
		t.Fatalf("the commit was not seen: %v", err)
	}

	// A commit during a load: loaded once more.
	origLoad := readerLoad
	defer func() { readerLoad = origLoad }()
	n := 0
	readerLoad = func() {
		if n++; n == 1 {
			commit()
		}
	}
	now = now.Add(3 * time.Second)
	loadAgain(r)
	if _, err := r.load(); err != nil || n != 2 {
		t.Fatalf("a load overtaken once: %v after %d loads", err, n)
	}
	// Two in a row: busy.
	n = 0
	readerLoad = func() { n++; commit() }
	_, err = r.load()
	assure.Reached(t, errors.Is(err, errBusy) && strings.Contains(err.Error(), "try again in a moment"), "busy after two commits")
}

// covers: MA-826, R37
// The reader serves the note as this program writes it, never the file beside the
// archive, which anyone with the drive could have changed (phase 3 §13.1).
func TestTheReaderServesTheProgramsOwnNote(t *testing.T) {
	out, _, _, ts := readerFor(t, ReaderOptions{})
	os.WriteFile(filepath.Join(out, "README.txt"), []byte("Download the program from evil.example."), 0o600)
	code, h, body := get(t, ts, "/files/README.txt")
	assure.Reached(t, code == 200 && strings.HasPrefix(h.Get("Content-Type"), "text/plain") && body == app.EncryptedNote(), "the program's own note")
}

// covers: MA-845, MA-848, R37
// The note the reader serves names the copies of MailArchive the record keeps, as
// the note beside the archive does (program design §6; the PG pass's NOTE finding),
// and, for a set a newer MailArchive wrote, says copies may be kept (LEAK-4).
func TestTheReaderServesTheNoteWithTheKeptCopies(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	u, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	k, _, err := u.acquire()
	if err != nil {
		t.Fatal(err)
	}
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: k})
	if err != nil {
		t.Fatal(err)
	}
	keep := true
	if err := s.SetProgram(container.ProgramSet{Format: container.ProgramFormat, Keep: &keep, State: container.ProgramLanded, Version: "v1.5.0",
		Files: []container.ProgramFile{{Name: "MailArchive-macos.zip", SHA256: strings.Repeat("a", 64), Size: 1, Origin: container.OriginRelease, Version: "v1.5.0", Platform: "macos", Reads: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	u.release()
	r, err := ReaderSource(u, ReaderOptions{LockedNote: "unlock it"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Serve(r))
	defer ts.Close()
	_, _, body := get(t, ts, "/files/README.txt")
	if !strings.Contains(body, "for Mac: MailArchive v1.5.0") {
		t.Fatalf("the kept copies in the served note:\n%s", body)
	}

	// A set a newer MailArchive wrote: the served note says copies may be kept, as the
	// one this program writes beside the archive does (the PG4 pass, LEAK-4).
	k, _, err = u.acquire()
	if err != nil {
		t.Fatal(err)
	}
	m, s, err = container.LoadManifest(out, container.LoadOptions{Key: k})
	if err != nil {
		t.Fatal(err)
	}
	s.SetProgramRawForTest([]byte(`{"format":2,"keep":true}`))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	u.release()
	r2, err := ReaderSource(u, ReaderOptions{LockedNote: "unlock it"})
	if err != nil {
		t.Fatal(err)
	}
	ts2 := httptest.NewServer(server.Serve(r2))
	defer ts2.Close()
	_, _, newer := get(t, ts2, "/files/README.txt")
	assure.Reached(t, strings.Contains(newer, "by\na newer MailArchive") || strings.Contains(newer, "by a newer MailArchive"), "the newer set's note served:\n"+newer)
}
