package tbrestore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1139, R40, S74
// The post-write check (design §5.4 step 7, RT-12): after every file is written and
// before the parent file is made, each is re-read by its final name and re-parsed; a
// file truncated, removed (antivirus taking it away), one byte changed, a message
// added or a message missing gives W-CHECKFAIL naming the file and folder, no parent
// file, and the one cleanup by the made-list: Local Folders as it was, "ended" with
// the result. With a foreign entry also in the container (a file antivirus left in
// place of the one it took), nothing is removed and W-LEFT's words replace "the part
// already written was removed". For an encrypted archive the words name neither file
// nor folder. Positive twin: an untouched tree passes and the parent file is made.
func TestRestorePostWriteCheck(t *testing.T) {
	in, msgs := sampleFolders()
	extra := "From x Mon Jan  1 00:00:00 2026\nSubject: added\n\nx\n\n"
	tamper := map[string]func(p string){
		"truncated": func(p string) { b, _ := os.ReadFile(p); _ = os.WriteFile(p, b[:len(b)/2], 0o600) },
		"removed":   func(p string) { _ = os.Remove(p) },
		"one byte": func(p string) {
			b, _ := os.ReadFile(p)
			i := bytes.Index(b, []byte("end"))
			b[i] = 'E'
			_ = os.WriteFile(p, b, 0o600)
		},
		"a message added": func(p string) {
			b, _ := os.ReadFile(p)
			_ = os.WriteFile(p, append(b, extra...), 0o600)
		},
		"a message missing": func(p string) {
			b, _ := os.ReadFile(p)
			i := bytes.LastIndex(b, []byte("\nFrom "))
			_ = os.WriteFile(p, b[:i+1], 0o600)
		},
	}
	for name, tm := range tamper {
		for _, withForeign := range []bool{false, true} {
			t.Run(name+map[bool]string{true: ", with a foreign entry"}[withForeign], func(t *testing.T) {
				fx := newFixture(t, in, msgs)
				before := fx.local()
				var tampered string
				done := false
				f := &faultFS{inner: newRootFS(fx.root), hook: func(op, n string) error {
					if op == "open" && strings.HasSuffix(n, "/Inbox") && !done {
						done = true
						c := filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd")
						tm(filepath.Join(c, "Inbox"))
						if withForeign {
							writeFile(t, filepath.Join(c, "Inbox.quarantined"), "left by antivirus")
						}
						tampered = fx.local()
					}
					return nil
				}}
				res, id := fx.write(context.Background(), f)
				if res.Outcome != OutCheckFail || fileExists(filepath.Join(fx.lf.Dir, "Restored — Work mail")) {
					t.Fatalf("%s: %s %v", name, res.Outcome, res.Err)
				}
				mustContain(t, res.Words, "checked every message", "Restored — Work mail.sbd/Inbox", "the folder Inbox", "Nothing was added to Thunderbird")
				if withForeign {
					if fx.local() != tampered || lastEv(fx.events(id)) != EvAbandoned {
						t.Fatal("a foreign entry: nothing removed, abandoned")
					}
					mustContain(t, res.Words, "found 1 entry it did not make", "Inbox.quarantined", "removed nothing there")
					if strings.Contains(res.Words, "The part already written was removed") {
						t.Fatal("the removed sentence is not said when nothing was removed")
					}
					return
				}
				if fx.local() != before || dump(fx.events(id)) != "[started ended]" {
					t.Fatalf("the one cleanup by the made-list: %v", fx.events(id))
				}
				mustContain(t, res.Words, "The part already written was removed")
			})
		}
	}

	t.Run("encrypted: counts only", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.enc = true
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, n string) error {
			if op == "open" && strings.HasSuffix(n, "/Sent") {
				_ = os.Remove(filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", "Sent"))
			}
			return nil
		}}
		res, _ := fx.write(context.Background(), f)
		if res.Outcome != OutCheckFail || strings.Contains(res.Words, "Sent") || !strings.Contains(res.Words, "one of the restored files") {
			t.Fatalf("encrypted W-CHECKFAIL names no file or folder: %q", res.Words)
		}
	})

	t.Run("positive twin", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		res, _ := fx.write(context.Background(), nil)
		if res.Outcome != OutDone || !fileExists(filepath.Join(fx.lf.Dir, "Restored — Work mail")) {
			t.Fatalf("an untouched tree passes: %s %v", res.Outcome, res.Err)
		}
	})
}
