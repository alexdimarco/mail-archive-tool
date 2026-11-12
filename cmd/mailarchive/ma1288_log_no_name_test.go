package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/tbcomplete"
)

// The C3 account-name pass's N-04 (docs/review-thunderbird-completeness-c3-name-adversarial.md),
// the lead's call of 2026-11-09, fail-private: the command line's summary never writes the name
// Thunderbird shows for an account (usually the email address) into a log file: a `-log` (a
// scheduled run's, a custom one outside the archive, or a keyed run's into an encrypted archive),
// or a stderr sent to a file. A log says the folder words. The terminal still shows the name.

func behindNamed() app.Result {
	return app.Result{Thunderbird: []tbcomplete.Result{{Account: "imap.qz.example", Name: "qzsam@qz.example", StoreID: "id",
		Verdict: tbcomplete.Behind, OnDisk: 3, Listed: 40}}}
}

const qzFolderWords = "in the Thunderbird account kept in the folder imap.qz.example so far"

// covers: MA-1288
// The run's -log, opened as the command opens it (newRunLogger), outside the archive: no address,
// the folder words instead. Positive twin first: on the terminal the summary says the address.
func TestTheSummaryNeverWritesTheNameToALog(t *testing.T) {
	var screen bytes.Buffer
	onScreen := summaryOnScreen
	summaryOnScreen = func(*log.Logger) bool { return true }
	printSummaryFor(log.New(&screen, "", 0), behindNamed(), false, false, t.TempDir(), "Done.", "darwin")
	summaryOnScreen = onScreen
	if !strings.Contains(screen.String(), "in qzsam@qz.example so far") {
		t.Fatalf("positive twin: the terminal does not show the address:\n%s", screen.String())
	}

	logPath := filepath.Join(t.TempDir(), "elsewhere.log")
	logger, closeLog, err := newRunLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	printSummaryFor(logger, behindNamed(), false, false, t.TempDir(), "Done.", "darwin")
	closeLog()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "qzsam") || !strings.Contains(string(data), qzFolderWords) {
		t.Errorf("the -log file:\n%s", data)
	}
}

// covers: MA-1288
// The attack pass's ATK-7, brought in: a logger on any plain file (a keyed run's -log outside an
// encrypted archive, stderr sent to a file) or any other writer is not the terminal, so it gets the
// folder words.
func TestOnlyATerminalIsTheScreen(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "stderr.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for what, w := range map[string]io.Writer{"a plain file": f, "a buffer": &bytes.Buffer{}, "io.Discard": io.Discard} {
		if summaryOnScreen(log.New(w, "", 0)) {
			t.Errorf("%s is taken for the terminal", what)
		}
	}
	printSummaryFor(log.New(f, "", 0), behindNamed(), false, false, t.TempDir(), "Done.", "darwin")
	data, _ := os.ReadFile(f.Name())
	if strings.Contains(string(data), "qzsam@qz.example") || !strings.Contains(string(data), qzFolderWords) {
		t.Errorf("the file holds the address:\n%s", data)
	}
}
