package app

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
)

// covers: MA-388, MA-389, R8, R18, S29
// Distinct errors are each logged (up to a cap, then counted in one line), so a
// run whose messages fail in different ways is never summed into one story.
func TestIndexWarningsCapDistinctErrors(t *testing.T) {
	var buf bytes.Buffer
	w := newIndexWarnings(log.New(&buf, "", 0))
	for i := 0; i < maxIndexWarnings+3; i++ {
		w.add(fmt.Errorf("distinct failure %d", i))
	}
	w.add(fmt.Errorf("distinct failure 0"))
	w.flush()
	w.flush() // a second flush reports nothing new
	got := buf.String()
	for i := 0; i < maxIndexWarnings; i++ {
		if !strings.Contains(got, fmt.Sprintf("distinct failure %d", i)) {
			t.Fatalf("distinct failure %d not logged:\n%s", i, got)
		}
	}
	if strings.Contains(got, fmt.Sprintf("distinct failure %d", maxIndexWarnings)) {
		t.Fatalf("a failure past the cap was logged in full:\n%s", got)
	}
	if !strings.Contains(got, "repeated for 1 more message(s): distinct failure 0") || !strings.Contains(got, "3 more message(s) failed with other errors") {
		t.Fatalf("closing lines missing:\n%s", got)
	}
	if strings.Count(got, "repeated for") != 1 {
		t.Fatalf("a second flush repeated itself:\n%s", got)
	}
}

// covers: MA-388, MA-389, R8, R18, S29
// A failure that starts mid-run is logged once, with its count (g7 friction
// re-verification of F7's residual). Every message the index refuses used to
// print its own identical "warning: index: …" line (40 walked, thousands on a
// real run). Now the first occurrence of each distinct error is logged, and one
// closing line says how many more messages failed the same way; INDEX-ERRORS and
// the rebuild WARNING still count every one. Fault injection only: a trigger
// that aborts every docs insert, the index, exporter and run are the real ones.
// Positive twin: a healthy run logs no index warning.
func TestRepeatedIndexFailuresAreLoggedOnceWithACount(t *testing.T) {
	const n = 12
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, n)

	healthyLog := &bytes.Buffer{}
	okOpts := Options{Inputs: []string{src}, Out: tmpDir(t), Mode: export.Incremental, Index: true}
	if _, err := Run(context.Background(), okOpts, log.New(healthyLog, "", 0), nil); err != nil || strings.Contains(healthyLog.String(), "warning: index") {
		t.Fatalf("positive twin: a healthy run logged index warnings (err %v):\n%s", err, healthyLog)
	}

	out := tmpDir(t)
	path := filepath.Join(out, "search.db")
	ix, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER inject_fault BEFORE INSERT ON docs BEGIN SELECT RAISE(ABORT, 'injected index fault'); END`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	logs := &bytes.Buffer{}
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, log.New(logs, "", 0), nil)
	if err != nil {
		t.Fatalf("a run whose index refuses every message still finishes: %v", err)
	}
	assure.Reached(t, res.IndexErrors, "messages the index refused")
	if res.IndexErrors != n {
		t.Fatalf("every refusal is still counted: IndexErrors=%d, want %d", res.IndexErrors, n)
	}
	if got := strings.Count(logs.String(), "injected index fault"); got != 2 {
		t.Fatalf("the error should appear once, plus one closing count line, not %d times:\n%s", got, logs)
	}
	if want := "repeated for 11 more message(s)"; !strings.Contains(logs.String(), want) {
		t.Fatalf("the closing line lacks %q:\n%s", want, logs)
	}
}
