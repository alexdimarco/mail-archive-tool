package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

var surfaceRun = time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC)

// surfaceManifest is a manifest in out with two stores (a Microsoft 365 mailbox
// and a .pst) and a last-run record whose start is surfaceRun, so a gap observed
// at surfaceRun is attempted and any other is not (FC14).
func surfaceManifest(t *testing.T, out string) (m *state.Manifest, mailbox, pst string) {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	mailbox = m.Token("mailbox:ann@contoso.com", "ann@contoso.com")
	pst = m.Token("path:/data/old.pst", "Old")
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: surfaceRun, Finished: surfaceRun.Add(time.Minute), PID: 1}); err != nil {
		t.Fatal(err)
	}
	return m, mailbox, pst
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// covers: MA-327, R1, R7, R19, S48
// The report has one row per ledger gap, after the record rows: kind = reason,
// class, folder, date (the item's, else the day the gap was first seen), subject,
// detail = locator · detail · runs since first (an item count for a folder or
// overflow row, "not attempted since" for a gap its store's last run did not
// attempt) and, in the path column, the store's SOURCE ID (the mailbox address or
// the input path). The order is source, folder, reason, locator; every field is
// control-free so a row is one line; a ledger-only archive still gets its report.
func TestReportHasOneRowPerGapWithItsSourceID(t *testing.T) {
	out := tmpDir(t)
	m, mailbox, pst := surfaceManifest(t, out)
	first := surfaceRun.Add(-48 * time.Hour)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:A1", Reason: "fetch-failed", Class: state.ClassFillable,
		Subject: "Quarterly\treport\nforged\x1b[2J", Received: "2026-03-02T09:00:00Z", Detail: "500 ErrorMimeContentConversionFailed"}, first)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:A1", Reason: "fetch-failed", Class: state.ClassFillable,
		Detail: "500 ErrorMimeContentConversionFailed"}, surfaceRun)
	m.ObserveGap(state.CaptureGap{Token: pst, Folder: "Projects/2009", Scope: state.GapScopeFolder, Locator: "folder:Projects/2009", Reason: "folder-unreadable", Class: state.ClassTerminal, Skipped: 7}, surfaceRun)
	m.ObserveGap(state.CaptureGap{Token: pst, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "pst-nid:u/9", Reason: "pst-message-unreadable", Class: state.ClassTerminal, Detail: "node unreadable"}, surfaceRun.Add(-72*time.Hour))
	m.ObserveGap(state.CaptureGap{Token: pst, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "ident:x", Reason: "refresh-failed", Class: state.ClassInfo}, surfaceRun)

	path, rows := writeReport(out, m, false, log.New(io.Discard, "", 0))
	if path == "" || rows != 4 {
		t.Fatalf("writeReport on a ledger-only archive = (%q, %d rows); want the report written with its 4 gap rows (FC24)", path, rows)
	}
	lines := readLines(t, path)
	if lines[0] != "kind\tclass\tfolder\tdate\tsubject\tdetail\tpath" || len(lines) != 5 {
		t.Fatalf("report header/length: %q (%d lines)", lines[0], len(lines))
	}
	var cols [][]string
	for _, l := range lines[1:] {
		f := strings.Split(l, "\t")
		if len(f) != 7 {
			t.Fatalf("a report row has %d fields, want 7 (a control character split it): %q", len(f), l)
		}
		cols = append(cols, f)
	}
	// Order: source, then folder, then reason, then locator. "/data/old.pst" sorts
	// before "ann@contoso.com"; in its Inbox "pst-message-unreadable" before
	// "refresh-failed".
	var got []string
	for _, f := range cols {
		got = append(got, f[6]+"|"+f[2]+"|"+f[0])
	}
	want := []string{
		"/data/old.pst|Inbox|pst-message-unreadable",
		"/data/old.pst|Inbox|refresh-failed",
		"/data/old.pst|Projects/2009|folder-unreadable",
		"ann@contoso.com|Inbox|fetch-failed",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rows in order (path|folder|kind): %v; want %v", got, want)
	}
	byKind := map[string][]string{}
	for _, f := range cols {
		byKind[f[0]] = f
	}
	ff := assure.Reached(t, byKind["fetch-failed"], "the fetch-failed row")
	if ff[1] != "fillable" || ff[3] != "2026-03-02T09:00:00Z" || !strings.HasPrefix(ff[4], "Quarterlyreportforged") || strings.ContainsAny(ff[4], "\x1b\t\n") {
		t.Errorf("fetch-failed row: class %q date %q subject %q", ff[1], ff[3], ff[4])
	}
	if !strings.Contains(ff[5], "graph:A1 · 500 ErrorMimeContentConversionFailed · 2 run(s) since 2026-08-30") {
		t.Errorf("fetch-failed detail %q lacks the locator, the error and the run count", ff[5])
	}
	fu := byKind["folder-unreadable"]
	if fu[1] != "terminal" || !strings.Contains(fu[5], "7 item(s) in this folder not archived") || fu[3] != surfaceRun.Format(time.RFC3339) {
		t.Errorf("folder row: class %q date %q detail %q; want terminal, its first-seen date and the item count", fu[1], fu[3], fu[5])
	}
	na := byKind["pst-message-unreadable"]
	if !strings.Contains(na[5], "not attempted since "+surfaceRun.Add(-72*time.Hour).Format(time.RFC3339)) {
		t.Errorf("a gap its store's last run did not attempt must say so: %q", na[5])
	}
	if byKind["refresh-failed"][1] != "info" {
		t.Errorf("an info gap's class is %q", byKind["refresh-failed"][1])
	}

	// Nothing to report at all removes the report, as before.
	empty := tmpDir(t)
	em, _ := state.Load(filepath.Join(empty, ".mailarchive-manifest.json"))
	if err := os.WriteFile(filepath.Join(empty, reportName), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, n := writeReport(empty, em, false, log.New(io.Discard, "", 0)); p != "" || n != 0 {
		t.Errorf("an archive with no issues and an empty ledger: report (%q, %d); want none", p, n)
	}
	if _, err := os.Stat(filepath.Join(empty, reportName)); err == nil {
		t.Error("the stale report was not removed")
	}
}

// covers: MA-327, R1, R7, S48
// Record gap labels in the report: the partly-read labels, unparsed and
// superseded have kinds of their own and the class GapClass gives them. Every
// record row's detail is its bare code followed by a plain-words why and what to do
// (owner ruling §13.11, ESC-16), the same register as a ledger row.
func TestReportKindsForReadGapLabels(t *testing.T) {
	out := tmpDir(t)
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	m.Add("k1", state.Record{Path: "s/a.html", Folder: "Inbox", Subject: "A", Missing: []string{"unreadable:body", "unreadable:attachments", "body"}})
	m.Add("k2", state.Record{Path: "s/b.html", Folder: "Inbox", Subject: "B", Terminal: []string{"unparsed", "superseded", "x.pdf"}})
	m.Add("k3", state.Record{Path: "s/c.html", Folder: "Inbox", Subject: "C", Missing: []string{state.UnknownSentinel}})
	m.Add("k4", state.Record{Path: "s/d.html", Folder: "Inbox", Subject: "D", Unresolved: []string{"logo@x"}, BodyFallback: "unparseable"})
	m.Add("k5", state.Record{Path: "s/e.html", Folder: "Inbox", Subject: "E", Terminal: []string{"body"}})
	path, rows := writeReport(out, m, false, log.New(io.Discard, "", 0))
	if rows != 10 {
		t.Fatalf("%d report rows, want 10", rows)
	}
	type want struct{ class, detail string }
	got := map[string]want{}
	for _, l := range readLines(t, path)[1:] {
		f := strings.Split(l, "\t")
		got[f[0]+"/"+f[1]] = want{f[1], f[5]}
	}
	const orig = "the original is kept"
	for k, w := range map[string]want{
		"unreadable-body/fillable":        {"fillable", "unreadable:body · why: the message body could only be partly read from the source · what to do: open the message in your mail app to check it, then run again; " + orig},
		"unreadable-attachments/fillable": {"fillable", "unreadable:attachments · why: the attachments could only be partly read from the source · what to do: open the message in your mail app to check it, then run again; " + orig},
		"missing-body/fillable":           {"fillable", "(message body) · why: the message was archived without a body · what to do: download the message in your mail app, then run again; " + orig},
		"missing-body/terminal":           {"terminal", "(message body) · why: the message was archived without a body · what to do: nothing to do: the source has no body to give; " + orig},
		"unparsed-message/terminal":       {"terminal", "unparsed · why: the message could not be parsed, so its page shows only what could be read · what to do: nothing to do: " + orig},
		"superseded/info":                 {"info", "superseded · why: a complete copy of this message was archived later, so this partly-read page no longer misses anything · what to do: nothing to do: the complete copy is kept"},
		"empty-attachment/terminal":       {"terminal", "x.pdf · why: the attachment came out empty (nothing was downloaded, or its stream failed) · what to do: nothing to do: the source cannot deliver it; " + orig},
		"unknown/fillable":                {"fillable", "unknown · why: this message was archived by an older version that did not track whether anything was missing · what to do: run again: it is checked once and this row closes; " + orig},
		"unresolved-inline-image/info":    {"info", "logo@x · why: the message's HTML shows an inline image that is not in the message (common in replies and forwards) · what to do: nothing to do: it is usually not recoverable; " + orig},
		"html-shown-as-source/info":       {"info", "unparseable · why: the message's HTML could not be shown safely as a page, so the page shows it as text · what to do: nothing to do: the words are kept and searchable; " + orig},
	} {
		g, ok := got[k]
		if !ok {
			t.Errorf("no %q row (rows: %v)", k, got)
			continue
		}
		if g != w {
			t.Errorf("row %q: class %q detail %q\nwant class %q detail %q", k, g.class, g.detail, w.class, w.detail)
		}
	}
}

// covers: MA-327, R1, R7, R19, S48
// README.txt explains the gap vocabulary to someone with no tool (FC23(5)): the
// report's wider scope, the "(unreadable message)" and partly-read pages, and what
// fillable, terminal and info mean, including that with no one running the tool a
// fillable or terminal row is missing for good.
func TestReadmeExplainsGapClasses(t *testing.T) {
	for _, want := range []string{
		"messages, folders, attachments or bodies that could not be captured",
		"(unreadable message)",
		"could only be partly read",
		"fillable = a later run of the tool against the original source may capture it",
		"terminal = the source itself would not deliver it as it stands",
		"info = nothing missing",
		"If no one runs the tool against the source anymore, every fillable or terminal row is missing from this archive.",
		"or finished with items it could not capture",
	} {
		// The README is hard-wrapped: compare with the line breaks folded.
		if !strings.Contains(strings.Join(strings.Fields(archiveReadme), " "), want) {
			t.Errorf("README.txt lacks %q", want)
		}
	}
}

// covers: MA-327, R1, R7, R19, S48
// The root index.html carries a script-free banner when the archive has gaps: a
// count (the ledger's fillable and terminal entries, partly-read records and
// unparsed records), never untrusted text, stamped in a data attribute so a re-run
// that changes the count regenerates the page and one that does not leaves it
// untouched (nas-02). Every pages.Generate caller passes the manifest-derived
// count, so a reindex and a rebuild keep the banner (FC24); a healthy archive has
// none.
func TestRootBannerAppearsFromEveryCaller(t *testing.T) {
	const banner = "Some items from the original mail source may not be in this archive — see attachments-report.tsv"
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}
	run := func() {
		t.Helper()
		if _, err := Run(context.Background(), opts, logger, nil); err != nil {
			t.Fatal(err)
		}
	}
	rootPage := func() string {
		b, err := os.ReadFile(filepath.Join(out, "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	run()
	if strings.Contains(rootPage(), "may not be in this archive") {
		t.Fatal("a healthy archive's root page carries the banner")
	}
	healthyMetas := metaTags(rootPage())
	assure.Reached(t, healthyMetas, "the root page's meta tags")

	// A ledger-only gap (hostile text in it): the next run, which exports nothing,
	// still regenerates the page because the count changed.
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var tok string
	for _, tk := range m.Stores {
		tok = tk
	}
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:Inbox/x", Reason: "file-unreadable", Class: state.ClassFillable,
		Subject: "<script>alert(1)</script>", Detail: "<img src=x onerror=alert(2)>"}, time.Now().UTC().Add(-time.Hour))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	run()
	page := rootPage()
	if !strings.Contains(page, banner) || !strings.Contains(page, `data-gaps="1"`) {
		t.Fatalf("a re-run over an archive with a ledger gap: the root page lacks the banner and its count stamp")
	}
	for _, bad := range []string{"<script", "alert(", "onerror"} {
		if strings.Contains(page, bad) {
			t.Errorf("the root page carries untrusted text or a script: %q", bad)
		}
	}
	// The page's policy and head metas are intact (R19): the banner adds a line of
	// body text, never a meta, and drops none.
	if got := metaTags(page); strings.Join(got, "\n") != strings.Join(healthyMetas, "\n") {
		t.Errorf("the banner changed the root page's meta tags:\n got  %q\n want %q", got, healthyMetas)
	}
	stamp := mtime(t, filepath.Join(out, "index.html"))
	time.Sleep(20 * time.Millisecond)
	run()
	if !mtime(t, filepath.Join(out, "index.html")).Equal(stamp) {
		t.Error("a re-run with the same gap count rewrote the root page")
	}

	// reindex and reindex -rebuild regenerate the pages through the same path.
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("<html>stale</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Reindex(out, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootPage(), banner) {
		t.Error("reindex dropped the banner")
	}
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("<html>stale</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(out, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootPage(), banner) {
		t.Error("reindex -rebuild dropped the banner")
	}
}

// metaTags returns every <meta ...> tag of a page, in order.
func metaTags(page string) []string {
	var out []string
	for rest := page; ; {
		i := strings.Index(strings.ToLower(rest), "<meta")
		if i < 0 {
			return out
		}
		rest = rest[i:]
		j := strings.IndexByte(rest, '>')
		if j < 0 {
			return out
		}
		out = append(out, rest[:j+1])
		rest = rest[j+1:]
	}
}

func mtime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

// covers: MA-327, R1, R7, R19, S48
// verify prints an informational line about what the source could not deliver and
// keeps its verdict: the line counts items and folders (an overflow entry for the
// items it absorbed; info gaps are nothing missing), says it does not affect
// attestation, and the exit code of an attested archive stays 0.
func TestVerifyPrintsTheNotInThisArchiveLine(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental}, logger, nil); err != nil {
		t.Fatal(err)
	}
	before, err := Verify(out, VerifyOptions{}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range VerifySummary(before) {
		if strings.Contains(l, "Not in this archive") {
			t.Fatalf("a healthy archive's verify names missing items: %q", l)
		}
	}

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, _ := state.Load(mpath)
	var tok string
	for _, tk := range m.Stores {
		tok = tk
	}
	run := time.Now().UTC().Add(-time.Hour)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:a", Reason: "file-unreadable", Class: state.ClassFillable}, run)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:b", Reason: "file-unreadable", Class: state.ClassTerminal}, run)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Old", Scope: state.GapScopeFolder, Locator: "folder:Old", Reason: "folder-unreadable", Class: state.ClassTerminal}, run)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "ident:i", Reason: "refresh-failed", Class: state.ClassInfo}, run)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(out, VerifyOptions{}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode() != 0 || !rep.Attested() {
		t.Fatalf("gaps changed verify's verdict: exit %d attested %v; the exit code is unchanged", rep.ExitCode(), rep.Attested())
	}
	joined := strings.Join(VerifySummary(rep), "\n")
	want := "Not in this archive: 2 item(s), 1 folder(s) from the source could not be captured (see attachments-report.tsv) — this does not affect attestation"
	if !strings.Contains(joined, want) {
		t.Errorf("verify's summary lacks %q:\n%s", want, joined)
	}
}

// covers: MA-327, R1, R7, R19, S48
// An archive whose store has a partly-read or unparsed record counts it in the
// banner too (the banner count is ledger entries plus those records).
func TestBannerCountIncludesPartlyReadAndUnparsedRecords(t *testing.T) {
	out := tmpDir(t)
	m, mailbox, _ := surfaceManifest(t, out)
	m.Add("k1", state.Record{Path: "s/a.html", Missing: []string{"unreadable:body"}})
	m.Add("k2", state.Record{Path: "s/b.html", Terminal: []string{"unparsed"}})
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:1", Reason: "fetch-failed", Class: state.ClassFillable}, surfaceRun)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "ident:i", Reason: "refresh-failed", Class: state.ClassInfo}, surfaceRun)
	if got := gapBannerCount(m); got != 3 {
		t.Errorf("banner count = %d, want 3 (one gap, one partly-read, one unparsed; info is nothing missing)", got)
	}
}
