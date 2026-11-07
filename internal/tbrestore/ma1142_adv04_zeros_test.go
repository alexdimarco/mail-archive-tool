package tbrestore

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1142, R40, S68
// ADV-04 (the R3–R5 adversarial pass, settled 2026-11-07): a power cut can leave a file
// longer than what reached the disk, its tail zero bytes (NTFS and XFS can). A line of
// zero bytes, or a cut-off line followed by zero bytes, is a torn line like any cut-off
// line, wherever it sits — so the log stays readable, before and after the next append
// starts on a line of its own, and the skip set built from it is readable. A whole line
// whose line break alone was lost to zeros is still read. But a zero byte inside a line
// that goes on afterwards is not a power cut's tail: W-LOGBAD.
func TestRestoresLogPassesOverAZeroFilledTail(t *testing.T) {
	for name, tail := range map[string]string{
		"a cut line then zero bytes": `{"at":"2026-11-07T00:00:00Z","event":"ended","id":"a","res` + "\x00\x00\x00\x00",
		"zero bytes only":            "\x00\x00\x00\x00\x00\x00",
		"zero bytes and spaces":      "\x00\x00 \x00\r",
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), LogName)
			l := NewLog(p)
			if err := l.Append(LogLine{Event: EvStarted, ID: "a", Profile: "k"}); err != nil {
				t.Fatal(err)
			}
			appendRaw(t, p, tail)
			lines, err := l.Read()
			if err != nil || len(lines) != 1 {
				t.Fatalf("read before the next line: %v %v", lines, err)
			}
			if err := l.Append(LogLine{Event: EvStarted, ID: "b", Profile: "k"}); err != nil {
				t.Fatal(err)
			}
			lines, err = l.Read()
			if err != nil || len(lines) != 2 || lines[1].ID != "b" {
				t.Fatalf("read after the next line: %v %v", lines, err)
			}
			if s := LoadSkipSet(p); s.LogUnreadable() {
				t.Fatal("the skip set reads the log")
			}
		})
	}

	t.Run("a whole line whose line break became zeros", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), LogName)
		appendRaw(t, p, `{"at":"x","event":"started","id":"a","profile":"k"}`+"\x00\x00")
		lines, err := NewLog(p).Read()
		if err != nil || len(lines) != 1 || lines[0].ID != "a" {
			t.Fatalf("the whole line is read: %v %v", lines, err)
		}
	})

	t.Run("a zero byte inside a line that goes on", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), LogName)
		appendRaw(t, p, "{\"event\":\"started\",\x00\"id\":\"a\"}\n")
		_, err := NewLog(p).Read()
		refusedAs(t, err, "W-LOGBAD", nil, "restores.log", "can't be read")
	})
}

func appendRaw(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
