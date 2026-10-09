package main

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// remedyArgs extracts the backticked `mailarchive …` command from a log line
// and returns its arguments after "mailarchive", unquoting any Go-quoted token.
func remedyArgs(t *testing.T, line string) []string {
	t.Helper()
	m := regexp.MustCompile("`mailarchive ([^`]*)`").FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("no `mailarchive …` remedy in %q", line)
	}
	var args []string
	for _, tok := range strings.Fields(m[1]) {
		if strings.HasPrefix(tok, `"`) {
			u, err := strconv.Unquote(tok)
			if err != nil {
				t.Fatalf("remedy token %s: %v", tok, err)
			}
			tok = u
		}
		args = append(args, tok)
	}
	return args
}

// covers: MA-390, R8, S33
// When a run exports messages it could not index, the run summary's
// INDEX-ERRORS warning names a remedy that actually restores them (F50): run
// end to end through the CLI, following the printed command leaves every
// manifest record in the index. Plain `reindex` cannot (it only prunes), so the
// printed remedy is `reindex -rebuild`. The index error is real: a trigger
// planted in the archive's search.db refuses one message's insert (fault
// injection only).
func TestRunSummaryIndexErrorRemedyHeals(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Inbox")
	for i, subj := range []string{"alpha", "bravo", "poison"} {
		writeMaildirMsg(t, src, strconv.Itoa(i)+".eml", "From: a@example.com\r\nTo: b@example.com\r\nSubject: "+subj+
			"\r\nDate: Mon, 03 Mar 2025 09:0"+strconv.Itoa(i)+":00 +0000\r\nMessage-ID: <"+subj+"@ex>\r\n\r\nbody "+subj+"\r\n")
	}

	// Positive twin: a healthy export indexes everything and warns nothing.
	okOut := t.TempDir()
	code, stderr := runCLI("-out", okOut, "-input", src)
	if code != 0 || strings.Contains(stderr, "INDEX-ERRORS") {
		t.Fatalf("healthy export: code=%d\n%s", code, stderr)
	}

	out := t.TempDir()
	idxPath := filepath.Join(out, "search.db")
	ix, err := index.Open(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	db, err := sql.Open("sqlite", idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER inject_fault BEFORE INSERT ON docs WHEN NEW.subject = 'poison'
		BEGIN SELECT RAISE(ABORT, 'injected index fault'); END`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	code, stderr = runCLI("-out", out, "-input", src)
	if code != 0 || !strings.Contains(stderr, "INDEX-ERRORS=1") {
		t.Fatalf("faulted export: code=%d, want 0 with INDEX-ERRORS=1\n%s", code, stderr)
	}
	var warning string
	for _, l := range strings.Split(stderr, "\n") {
		if strings.Contains(l, "could not be indexed") {
			warning = l
		}
	}
	assure.Reached(t, warning, "the run summary's index-error WARNING line")

	args := remedyArgs(t, warning)
	if rc, rerr := runCLI(args...); rc != 0 {
		t.Fatalf("the printed remedy `mailarchive %s` failed (exit %d):\n%s", strings.Join(args, " "), rc, rerr)
	}
	ro, err := index.OpenReadonly(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if n, _ := ro.Count(); n != 3 {
		t.Fatalf("after following the summary's remedy `mailarchive %s` the index holds %d of 3 messages", strings.Join(args, " "), n)
	}
}
