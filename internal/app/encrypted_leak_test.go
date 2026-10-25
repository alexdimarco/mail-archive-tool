package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
)

// cleartext2b are the files an encrypted archive still holds in cleartext until
// Phase 2b seals them (design-encrypted-archives-phase2a §7 item 5): the ONLY
// exemption the leak and tree tests allow. It is strict both ways: a leak outside it
// fails, and an entry that no longer leaks fails too (it outlived 2b), and every
// entry is listed in the MA-744 catalog row (TestTheCleartext2bListNeverGrows).
var cleartext2b = map[string]string{
	"search.db":                  "the search index, with its -wal/-shm/-journal companions: 2b seals it",
	".mailarchive-history.jsonl": "the go-back history (Microsoft 365 captures only): 2b seals it",
}

// cleartext2bOf names the cleartext2b entry a scanned path falls under, or "".
func cleartext2bOf(where string) string {
	top := strings.SplitN(strings.SplitN(where, "!", 2)[0], "/", 2)[0]
	for name := range cleartext2b {
		if top == name || strings.HasPrefix(top, name+"-") {
			return name
		}
	}
	return ""
}

// leakMIME is one message carrying a marker in every field a message has: an RFC
// 2047 subject, a sender and recipient, a Message-ID, a quoted-printable plain body
// and an entity-encoded HTML body, and an attachment with a percent-encoded name and
// base64 bytes — each with the non-ASCII decoration where the field allows it.
func leakMIME(ms leak.Markers) string {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	var qp strings.Builder
	w := quotedprintable.NewWriter(&qp)
	fmt.Fprintf(w, "%s %s, quoted-printable", ms[leak.Body], leak.Deco)
	w.Close()
	return "From: =?UTF-8?B?" + b64(leak.Deco+" "+ms[leak.Sender]) + "?= <" + ms[leak.Sender] + "@example.com>\r\n" +
		"To: <" + ms[leak.Recipient] + "@example.com>\r\n" +
		"Subject: =?UTF-8?B?" + b64(ms[leak.Subject]+" "+leak.Deco) + "?=\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\n" +
		"Message-ID: <" + ms[leak.MessageID] + "@example.org>\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"B1\"\r\n\r\n" +
		"--B1\r\nContent-Type: multipart/alternative; boundary=\"B2\"\r\n\r\n" +
		"--B2\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" + qp.String() + "\r\n" +
		"--B2\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>" + ms[leak.Body] + " Zo&#xEB; &Aring;ngstr&#246;m</p>\r\n--B2--\r\n" +
		"--B1\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename*=UTF-8''" +
		url.PathEscape(leak.Deco) + "-" + ms[leak.AttachName] + ".txt\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		b64("ATT "+ms[leak.AttachBytes]+" "+leak.Deco) + "\r\n--B1--\r\n"
}

// leakStore is a Thunderbird-style store whose folder, store and path all carry
// markers: <tmp>/<path marker>/<store marker>/<folder marker> (an mbox file holding
// leakMIME). It returns the store folder (the -input) and the mbox file.
func leakStore(t *testing.T, ms leak.Markers) (store, mbox string) {
	t.Helper()
	store = filepath.Join(tmpDir(t), "Zoë-"+ms[leak.SourcePath], "Ångström-"+ms[leak.Store])
	mbox = filepath.Join(store, ms[leak.Folder]+" ñ")
	p14Write(t, mbox, "From someone@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(leakMIME(ms), "\r\n", "\n")+"\n")
	return store, mbox
}

// leakGraph is a fake Microsoft 365 holding leakMIME in a folder named by a marker,
// in a mailbox whose address is a marker.
func leakGraph(t *testing.T, ms leak.Markers) GraphOptions {
	t.Helper()
	s := graphtest.NewServer()
	t.Cleanup(s.Close)
	upn := ms[leak.Mailbox] + "@example.com"
	s.AddFolder(upn, "F1", ms[leak.Folder]+" ñ", 10)
	s.AddMessage(upn, "F1", "M1")
	s.SetMid("M1", ms[leak.MessageID]+"@example.org")
	s.SetValue("M1", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, leakMIME(ms)) })
	return GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{upn}, BaseURL: s.URL, TokenURL: s.URL + "/token"}
}

// leakRuns archives the marked fixture three ways into fresh archives — a local
// store, the same store's mbox file with -copy-first, and a Microsoft 365 capture —
// with the search index, folder pages and originals on, encrypted when keyed. Each
// run is observed mid-run too (before every write and after the snapshot), and the
// observer sees every file then present. It returns the archive folders.
func leakRuns(t *testing.T, ms leak.Markers, keyed bool, observe func(stage, out string)) []string {
	t.Helper()
	store, mbox := leakStore(t, ms)
	orig, origCopy := exporterSetup, snapshotCopy
	t.Cleanup(func() { exporterSetup, snapshotCopy = orig, origCopy })
	var current string
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(string) error { observe("mid-run", current); return nil }
	}
	snapshotCopy = func(dst io.Writer, src io.Reader) (int64, error) {
		n, err := origCopy(dst, src)
		observe("after the snapshot", current)
		return n, err
	}
	var outs []string
	for _, run := range []func(out string, opts Options) error{
		func(out string, opts Options) error {
			opts.Inputs = []string{store}
			_, err := Run(context.Background(), opts, discard(), nil)
			return err
		},
		func(out string, opts Options) error {
			opts.Inputs, opts.CopyFirst = []string{mbox}, true
			_, err := Run(context.Background(), opts, discard(), nil)
			return err
		},
		func(out string, opts Options) error {
			_, err := RunGraph(context.Background(), leakGraph(t, ms), opts, discard())
			return err
		},
	} {
		out := tmpDir(t)
		opts := Options{Out: out, Index: true, Pages: true, KeepRaw: true}
		if keyed {
			out, opts.Key = encryptedOut(t)
			opts.Out = out
		}
		current = out
		if err := run(out, opts); err != nil {
			t.Fatalf("a run over the marked fixture into %s: %v", out, err)
		}
		outs = append(outs, out)
	}
	return outs
}

// covers: MA-742, R37
// The leak scan's positive twin, first: the marked fixture archived WITHOUT a key
// leaves every field's marker findable — subject, body, folder, sender, recipient,
// Message-ID, attachment name, attachment bytes (inside the zip), store and source
// path from the local runs, and the mailbox address from the Microsoft 365 capture —
// so "no hits" on an encrypted archive is a statement about the archive, not about a
// fixture or a scanner that sees nothing.
func TestTheLeakScanSeesEveryFieldInAPlaintextArchive(t *testing.T) {
	ms := leak.New()
	seen := map[leak.Field]bool{}
	inZip, inName := false, map[leak.Field]bool{}
	for _, out := range leakRuns(t, ms, false, func(string, string) {}) {
		hits, err := leak.ScanTree(out, ms)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			seen[h.Field] = true
			if h.Field == leak.AttachBytes && strings.Contains(h.Where, ".zip!") {
				inZip = true
			}
			if h.InName {
				inName[h.Field] = true
			}
		}
	}
	for _, f := range leak.Fields {
		if !seen[f] {
			t.Errorf("the plaintext archive of the marked fixture shows no %s marker: the fixture or the scanner is blind to it", f)
		}
	}
	// The scanner reads inside zip members and in file names: a plaintext archive
	// keeps the attachment only deflated in its zip, and names its folder tree after
	// the store and folder.
	if !inZip {
		t.Error("the scanner found no attachment bytes inside an attachment zip: it does not read zip members")
	}
	if !inName[leak.Folder] || !inName[leak.Store] {
		t.Errorf("the scanner found no folder or store marker in a file name (%v): it does not read names", inName)
	}
	assure.Reached(t, seen, "fields seen in a plaintext archive")
}

// covers: MA-742, R37
// The C2 property (P2a-C17): the same marked fixture archived WITH a key — a local
// store, a -copy-first mbox file and a Microsoft 365 capture, with the search index,
// folder pages and originals on — leaves no marker of any field in any file NAME or
// contents under the archive, in any encoding (plain, case-changed, UTF-16, hex,
// base64 at every alignment, inside zip and gzip members), neither at rest nor at
// any moment of the run (before every write, after the snapshot) — except in the
// files Phase 2b seals (cleartext2b), each of which must still be the one leaking.
func TestAnEncryptedArchiveHoldsNoCleartextOfItsMail(t *testing.T) {
	ms := leak.New()
	stray := map[string]bool{}
	still := map[string]bool{}
	observations := 0
	check := func(stage, out string) {
		observations++
		hits, err := leak.ScanTree(out, ms)
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		for _, h := range hits {
			if e := cleartext2bOf(h.Where); e != "" {
				still[e] = true
				continue
			}
			stray[stage+": "+h.String()] = true
		}
	}
	for _, out := range leakRuns(t, ms, true, check) {
		check("at rest", out)
	}
	assure.Reached(t, observations, "mid-run observations")
	if len(stray) > 0 {
		var list []string
		for s := range stray {
			list = append(list, s)
		}
		sort.Strings(list)
		t.Fatalf("an encrypted archive holds its mail in cleartext:\n%s", strings.Join(list, "\n"))
	}
	for name := range cleartext2b {
		if !still[name] {
			t.Errorf("%s no longer holds cleartext: remove it from cleartext2b and the MA-744 catalog row", name)
		}
	}
}

// covers: MA-744, R37
// The 2b list cannot grow quietly: every cleartext2b entry is named, in backticks,
// in the MA-744 row of the catalog of record, so adding one is an operator-visible
// catalog edit; and the catalog names no entry the list lacks.
func TestTheCleartext2bListNeverGrows(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(moduleRootDir(t), "docs", "scenario-catalog.md"))
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "| MA-744 |") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("the catalog has no MA-744 row")
	}
	listed := strings.SplitN(row, "2b list:", 2)
	if len(listed) != 2 {
		t.Fatalf("the MA-744 row does not name its 2b list (\"2b list: `name`, ...;\"): %s", row)
	}
	names := map[string]bool{}
	for _, part := range strings.Split(strings.SplitN(listed[1], ";", 2)[0], "`") {
		if p := strings.TrimSpace(part); p != "" && !strings.ContainsAny(p, ", ") {
			names[p] = true
		}
	}
	for name := range cleartext2b {
		if !names[name] {
			t.Errorf("cleartext2b holds %s, which the MA-744 catalog row does not name", name)
		}
	}
	for name := range names {
		if _, ok := cleartext2b[name]; !ok {
			t.Errorf("the MA-744 catalog row names %s, which cleartext2b does not hold", name)
		}
	}
	assure.Reached(t, names, "the 2b list in the catalog")
}

// moduleRootDir is the repository root: the nearest folder above this package
// holding go.mod.
func moduleRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the app package")
		}
		dir = parent
	}
}
