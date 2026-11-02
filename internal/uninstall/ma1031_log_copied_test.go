package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1031, R36
// `uninstall -yes` asks CheckLogCopied before it removes anything (the 1.5.3 friction review's
// D2, the main session's decision): a deletion log holding any line — even one MailArchive
// cannot parse — refuses with LogNotCopiedError, naming the log, where to copy it and the
// command that goes on (-log-copied), and the log is left as it was. Positive twin first: no
// log, an absent log, a log of blank lines, and a log holding lines with -log-copied each go
// ahead.
func TestUninstallYesRefusesUntilTheDeletionLogIsCopied(t *testing.T) {
	cfg := t.TempDir()
	log := filepath.Join(cfg, archdelete.LogName)

	// Positive twins.
	if err := CheckLogCopied("", false); err != nil {
		t.Fatalf("no deletion log: %v", err)
	}
	if err := CheckLogCopied(log, false); err != nil {
		t.Fatalf("an absent deletion log holds no line: %v", err)
	}
	writeFile(t, log, "\n  \n")
	if err := CheckLogCopied(log, false); err != nil {
		t.Fatalf("a deletion log of blank lines holds no line: %v", err)
	}
	for _, body := range []string{`{"event":"finished","run":"r1"}` + "\n", "not a line MailArchive wrote\n"} {
		writeFile(t, log, body)
		if err := CheckLogCopied(log, true); err != nil {
			t.Fatalf("with -log-copied a log holding lines goes ahead: %v", err)
		}
	}

	// Refused: any line, whether or not it parses.
	for _, body := range []string{`{"event":"finished","run":"r1"}` + "\n", "not a line MailArchive wrote\n"} {
		writeFile(t, log, body)
		err := CheckLogCopied(log, false)
		var nc *LogNotCopiedError
		code := 0
		if errors.As(err, &nc) {
			code = 1
		}
		assure.Refused(t, code, errText(err), assure.Code(1),
			assure.Names(log, "so nothing was removed", "Copy it somewhere you keep records", "then run mailarchive uninstall -yes -log-copied."),
			assure.NoSideEffect(func() bool { b, _ := os.ReadFile(log); return string(b) == body }))
	}
}
