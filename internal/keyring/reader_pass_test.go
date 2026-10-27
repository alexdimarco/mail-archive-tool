package keyring

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The P3-2a adversarial pass's fixes (MA-798).

// covers: MA-798, MA-796, R37
// Close — what serve and search call as they end — removes a scratch index's
// plaintext file before it returns, so nothing of it outlives the process (the
// P3-2a pass, LEAK-01).
func TestCloseRemovesTheScratchIndexBeforeItReturns(t *testing.T) {
	orig := readerIndexBudget
	readerIndexBudget = 1
	defer func() { readerIndexBudget = orig }()
	_, _, r, _ := readerFor(t, ReaderOptions{})
	files := scratchFiles(t)
	if len(files) != 1 {
		t.Fatalf("fixture: %v", files)
	}
	r.Close()
	assure.Reached(t, !exists(files[0]) && !exists(files[0]+".lock"), "the scratch index and its lock file gone when Close returns")
}

// covers: MA-798, MA-796, R37
// Locking the open the reader reads through drops what the reader held — its
// view, rendered pages and scratch file — as its own lock does (LEAK-02, KEY-02).
func TestLockingTheOpenDropsTheReadersView(t *testing.T) {
	orig := readerIndexBudget
	readerIndexBudget = 1
	defer func() { readerIndexBudget = orig }()
	_, u, r, ts := readerFor(t, ReaderOptions{})
	if code, _, _ := get(t, ts, "/files/"); code != 200 {
		t.Fatalf("positive twin: %d", code)
	}
	files := scratchFiles(t)
	u.Lock()
	if code, _, _ := get(t, ts, "/files/"); code != http.StatusServiceUnavailable {
		t.Fatalf("after the open's lock: %d", code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for (r.take() != nil || exists(files[0])) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	assure.Reached(t, !exists(files[0]), "the view and its scratch file dropped")
}

// covers: MA-798, MA-796, R37
// Lock never waits on a load in progress, and a load that finishes after it is
// closed, never served (KEY-03).
func TestLockNeverWaitsForALoad(t *testing.T) {
	_, _, r, _ := readerFor(t, ReaderOptions{})
	gate, entered := make(chan struct{}), make(chan struct{})
	orig := readerLoad
	readerLoad = func() {
		close(entered)
		<-gate
	}
	defer func() { readerLoad = orig }()
	loadAgain(r)
	result := make(chan error, 1)
	go func() { _, err := r.ensure(); result <- err }()
	<-entered
	locked := make(chan struct{})
	go func() { r.Lock(); close(locked) }()
	select {
	case <-locked:
	case <-time.After(3 * time.Second):
		t.Fatal("Lock waited for a load")
	}
	close(gate)
	err := <-result
	assure.Reached(t, errors.Is(err, server.ErrLocked) && r.take() == nil, "the late load locked, its view not served")
}

// covers: MA-798, MA-796, R37
// A folder named without its slash, asked for after a lock, gets the locked page,
// not a missing page (KEY-04).
func TestALockedFolderLinkGetsTheLockedPage(t *testing.T) {
	_, _, r, ts := readerFor(t, ReaderOptions{LockedNote: "start serve again"})
	rec := aMessage(t, r)
	folder := rec.Path[:strings.LastIndex(rec.Path, "/")]
	if code, _, _ := get(t, ts, "/files/"+folder); code != http.StatusMovedPermanently {
		t.Fatalf("positive twin: %d", code)
	}
	r.Lock()
	code, _, body := get(t, ts, "/files/"+folder)
	assure.Reached(t, code == http.StatusServiceUnavailable && strings.Contains(body, "start serve again"), "the locked page")
}

// attachmentMbox is a message with an attachment of n random bytes.
func attachmentMbox(n int) (mbox string, attachment []byte) {
	attachment = make([]byte, n)
	rand.Read(attachment)
	var b strings.Builder
	b.WriteString("From - Mon Jan 01 00:00:00 2024\r\nFrom: Bob <bob@example.com>\r\nSubject: The report\r\n" +
		"Date: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <rep@example.com>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n--b1\r\nContent-Type: text/plain\r\n\r\nsee attached\r\n" +
		"--b1\r\nContent-Type: application/octet-stream; name=\"report.bin\"\r\nContent-Disposition: attachment; filename=\"report.bin\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString(attachment)
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n--b1--\r\n")
	return b.String(), attachment
}

// readerWithAttachment backs up a message with an n-byte attachment and opens a
// reader on it.
func readerWithAttachment(t *testing.T, n int, o ReaderOptions) (*Unlocked, *Reader, string, string) {
	t.Helper()
	freshRecord(t)
	out, _, w := made(t, nil)
	mbox, _ := attachmentMbox(n)
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(mbox), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, Index: true, KeepRaw: true}
	done, err := Apply(&opts, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), opts, quiet, nil); err != nil {
		t.Fatal(err)
	}
	done()
	r, err := ReaderSource(w, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	v, _ := r.ensure()
	for _, rec := range v.m.All() {
		if rec.Sealed != nil && rec.Sealed.Zip != nil {
			return w, r, out, strings.TrimSuffix(rec.Path, ".html") + "-attachments.zip"
		}
	}
	t.Fatal("fixture: no attachment zip")
	return nil, nil, "", ""
}

// covers: MA-798, MA-796, R37
// An attachment zip is served from its own unit: a zip, application/zip, holding
// the attachment (§7.2; FID-07).
func TestTheReaderServesTheAttachmentZip(t *testing.T) {
	_, r, _, zip := readerWithAttachment(t, 4<<10, ReaderOptions{})
	ts := newTestServer(t, r)
	code, h, body := get(t, ts, "/files/"+zip)
	assure.Reached(t, code == 200 && h.Get("Content-Type") == "application/zip" && strings.HasPrefix(body, "PK") && strings.Contains(body, "report.bin"),
		"the attachment zip")
}

// covers: MA-798, MA-796, R37
// A client that stops reading an attachment gives back its memory budget within
// the write timeout: an unlock's derivation is not held behind it, and Lock does
// not wait (§3.2's owed test; KEY-05).
func TestAStalledDownloadDelaysNeitherLockNorAnUnlock(t *testing.T) {
	g := memguard.New(1 << 30)
	_, r, _, zip := readerWithAttachment(t, 24<<20, ReaderOptions{Guard: g, WriteTimeout: 300 * time.Millisecond})
	ts := newTestServer(t, r)
	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("GET /files/" + zip + " HTTP/1.1\r\nHost: x\r\n\r\n"))
	one := make([]byte, 1)
	io.ReadFull(conn, one) // the response started; then nothing more is read
	derived := make(chan struct{})
	go func() {
		d := g.Derivations()
		d.Lock()
		d.Unlock()
		close(derived)
	}()
	select {
	case <-derived:
	case <-time.After(15 * time.Second):
		t.Fatal("an unlock's derivation waited behind a stalled download")
	}
	start := time.Now()
	r.Lock()
	assure.Reached(t, time.Since(start) < time.Second, "Lock did not wait")
}

func newTestServer(t *testing.T, r *Reader) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(server.Serve(r))
	t.Cleanup(ts.Close)
	return ts
}

// covers: MA-798, MA-796, R37
// An index chunk gone while the record holds still leaves the reader open: pages
// that need no index and every message are served; search says it is unavailable
// because part of the index is missing, never that anything is damaged (§7.3, §7.4;
// OUT-A, FID-01).
func TestAMissingIndexChunkOnlyTakesSearchAway(t *testing.T) {
	out, u, r, _ := readerFor(t, ReaderOptions{})
	rec := aMessage(t, r)
	v, _ := r.ensure()
	chunks := v.store.Chunks(vault.ContainerIndex)
	r.Close()
	os.Remove(filepath.Join(out, chunks[len(chunks)-1]))
	r2, err := ReaderSource(u, ReaderOptions{})
	if err != nil {
		t.Fatalf("one index chunk gone refused the reader: %v", err)
	}
	t.Cleanup(r2.Close)
	ts := newTestServer(t, r2)
	if code, _, _ := get(t, ts, "/files/"+rec.Path); code != 200 {
		t.Fatalf("a message: %d", code)
	}
	code, _, body := get(t, ts, "/api/search?q=hello")
	assure.Reached(t, code == http.StatusServiceUnavailable && strings.Contains(body, "search is unavailable") &&
		strings.Contains(body, "missing") && !strings.Contains(body, "damaged"), "search unavailable, never damaged")
}

// covers: MA-798, MA-796, R37
// With no search index — an export without one — the reader still serves every
// message; the folder pages and search say why they are unavailable and name the
// remedy, never "could not be read" (FID-02).
func TestAnArchiveWithNoIndexSaysWhyBrowsingIsUnavailable(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(sampleMbox), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, KeepRaw: true}
	done, err := Apply(&opts, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), opts, quiet, nil); err != nil {
		t.Fatal(err)
	}
	done()
	r, err := ReaderSource(w, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ts := newTestServer(t, r)
	rec := aMessage(t, r)
	if code, _, _ := get(t, ts, "/files/"+rec.Path); code != 200 {
		t.Fatalf("a message by its link: %d", code)
	}
	for _, p := range []string{"/files/", "/api/search?q=x"} {
		code, _, body := get(t, ts, p)
		if code != http.StatusServiceUnavailable || !strings.Contains(body, "search is unavailable") || !strings.Contains(body, "reindex -rebuild") || strings.Contains(body, "could not be read") {
			t.Fatalf("%s: %d %s", p, code, body)
		}
	}
	assure.Reached(t, true, "both told why")
}

// commitHistory writes a sealed timeline for the archive's records — a run
// header and each record's folder — through a keyed save.
func commitHistory(t *testing.T, out string, u *Unlocked) {
	t.Helper()
	k, _, err := u.acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer u.release()
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: k, RunStart: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	tl := s.Timeline()
	if err := tl.WriteRunHeader(1, time.Date(2024, 1, 3, 9, 0, 0, 0, time.UTC), []string{"a@example.com"}); err != nil {
		t.Fatal(err)
	}
	for key, rec := range m.All() {
		tl.WriteFolder(key, rec.Folder)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-798, MA-796, R21, R37
// Go-back reads an encrypted archive's sealed history: the timeline available and a
// past date served; a damaged history chunk, and one gone while the record holds
// still, are a partial timeline — 200, never an error (§7.3; OUT-B, FID-06).
func TestGoBackReadsTheSealedHistory(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	commitHistory(t, out, w)
	r, err := ReaderSource(w, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	ts := newTestServer(t, r)
	code, _, body := get(t, ts, "/goback")
	if code != 200 || !strings.Contains(body, "Go-back is available") {
		t.Fatalf("available: %d\n%s", code, body)
	}
	if code, _, body := get(t, ts, "/goback?at=2024-01-03"); code != 200 || !strings.Contains(body, "As of 2024-01-03") {
		t.Fatalf("a past date: %d\n%s", code, body)
	}
	v, _ := r.ensure()
	chunk := filepath.Join(out, v.store.Chunks(vault.ContainerHistory)[0])
	raw, _ := os.ReadFile(chunk)
	for name, harm := range map[string]func(){
		"damaged": func() { b := bytes.Clone(raw); b[len(b)-1] ^= 1; os.WriteFile(chunk, b, 0o600) },
		"gone":    func() { os.Remove(chunk) },
	} {
		harm()
		loadAgain(r)
		code, _, body := get(t, ts, "/goback")
		if code != 200 || !strings.Contains(body, "Go-back is partial") {
			t.Fatalf("a history chunk %s: %d\n%s", name, code, body)
		}
		os.WriteFile(chunk, raw, 0o600)
	}
}

// covers: MA-798, MA-796, R37
// A load that fails while the record changes under it is loaded again, never
// reported (§7.3's owed test; FID-03).
func TestALoadThatFailsAsARecordChangesIsLoadedAgain(t *testing.T) {
	out, _, r, _ := readerFor(t, ReaderOptions{})
	calls := 0
	orig := readerLoadManifest
	readerLoadManifest = func(o string, opts container.LoadOptions) (*state.Manifest, *container.Store, error) {
		if calls++; calls == 1 {
			w, err := Open(out, Password(testPassword), OpenOptions{Write: true})
			if err != nil {
				t.Fatal(err)
			}
			readBackup(t, w)
			w.Destroy()
			return nil, nil, errors.New("container: a chunk vanished under the load")
		}
		return orig(o, opts)
	}
	defer func() { readerLoadManifest = orig }()
	loadAgain(r)
	_, err := r.ensure()
	assure.Reached(t, err == nil && calls == 2, "loaded again and served")
}

// covers: MA-798, MA-796, R37
// A lock landing between a unit's ciphertext read and its decryption, or during a
// load, is the locked page — never damage (FID-04).
func TestALockInTheReadersOtherWindowsIsTheLockedPage(t *testing.T) {
	_, u, r, ts := readerFor(t, ReaderOptions{LockedNote: "start serve again"})
	rec := aMessage(t, r)
	orig := readerBetween
	readerBetween = func() { u.Lock() }
	code, _, body := get(t, ts, "/files/"+rec.Path)
	readerBetween = orig
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "start serve again") {
		t.Fatalf("a lock between read and open: %d %s", code, body)
	}

	_, u2, r2, _ := readerFor(t, ReaderOptions{})
	origLoad := readerLoadManifest
	readerLoadManifest = func(o string, opts container.LoadOptions) (*state.Manifest, *container.Store, error) {
		u2.Lock()
		return origLoad(o, opts)
	}
	defer func() { readerLoadManifest = origLoad }()
	loadAgain(r2)
	_, err := r2.ensure()
	assure.Reached(t, errors.Is(err, server.ErrLocked), "a lock during a load")
}

// covers: MA-798, MA-796, R37
// An index the record marks behind is told on the search page (§7.4; FID-05).
func TestAnIndexBehindIsToldOnTheSearchPage(t *testing.T) {
	out, u, r, _ := readerFor(t, ReaderOptions{})
	r.Close()
	k, _, _ := u.acquire()
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: k, RunStart: time.Now()})
	if err == nil {
		s.MarkIndexBehind()
		err = m.Save()
	}
	u.release()
	if err != nil {
		t.Fatal(err)
	}
	r2, err := ReaderSource(u, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r2.Close)
	_, _, body := get(t, newTestServer(t, r2), "/api/facets")
	assure.Reached(t, strings.Contains(body, "search may be missing messages"), "the note")
}

// covers: MA-798, MA-794, R37
// A message too large for the budget is named by its own size, not the reading's
// (FID-12).
func TestTooLargeNamesTheMessagesOwnSize(t *testing.T) {
	_, r, _, zip := readerWithAttachment(t, 3<<20, ReaderOptions{Guard: memguard.New(1 << 20)})
	v, _ := r.ensure()
	_, _, s, _ := lookupUnit(v.m, zip)
	want := (s.Size + (1 << 20) - 1) >> 20
	_, _, body := get(t, newTestServer(t, r), "/files/"+zip)
	assure.Reached(t, strings.Contains(body, "too large to open on this computer ("+strconv.FormatInt(want, 10)+" MB)"), "named by the message's size")
}

// covers: MA-798, MA-796, R37
// The reader reads a folder it cannot write — a read-only medium — serving pages
// and messages (§7.3; FID-11).
func TestTheReaderReadsAFolderItCannotWrite(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a folder without write permission needs a Unix user other than root")
	}
	out, u, r, _ := readerFor(t, ReaderOptions{})
	rec := aMessage(t, r)
	r.Close()
	os.Chmod(out, 0o555)
	defer os.Chmod(out, 0o755)
	r2, err := ReaderSource(u, ReaderOptions{})
	if err != nil {
		t.Fatalf("a read-only folder: %v", err)
	}
	t.Cleanup(r2.Close)
	ts := newTestServer(t, r2)
	c1, _, _ := get(t, ts, "/files/")
	c2, _, _ := get(t, ts, "/files/"+rec.Path)
	assure.Reached(t, c1 == 200 && c2 == 200, "pages and a message served")
}

// covers: MA-798, MA-796, R37
// A link planted at the primary record pointer does not stop the reader while the
// mirror loads the record (OUT-F).
func TestAPlantedPrimaryPointerDoesNotStopTheReader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	out, u, r, _ := readerFor(t, ReaderOptions{})
	rec := aMessage(t, r)
	r.Close()
	ptr := filepath.Join(out, container.PointerName)
	os.Remove(ptr)
	if err := os.Symlink(filepath.Join(out, container.MirrorName), ptr); err != nil {
		t.Fatal(err)
	}
	r2, err := ReaderSource(u, ReaderOptions{})
	if err != nil {
		t.Fatalf("a planted primary pointer refused the reader: %v", err)
	}
	t.Cleanup(r2.Close)
	code, _, _ := get(t, newTestServer(t, r2), "/files/"+rec.Path)
	assure.Reached(t, code == 200, "served from the mirror's record")
}
