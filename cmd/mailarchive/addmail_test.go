package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/state"
)

// covers: MA-548, R2, R19
// The command the desktop shows for adding new mail to a Thunderbird-origin archive
// (desktop.AddMailArgv, desktop.AddMailAutoArgv; also in README.md and
// docs/trying-the-extraction-workspace.md) is parsed by this command's own flag parser into the
// folder, the same archive folder and the default incremental mode, and, run for real on a
// Thunderbird-style mbox folder into an existing archive, adds only the message that is new.
func TestDocumentedAddMailCommandParsesAndAddsOnlyNewMail(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Mail", "Local Folders")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "My Archive")
	msg := func(id string) string {
		return "From - Mon Mar  3 09:00:00 2025\nFrom: a@example.com\nTo: b@example.com\nSubject: " + id +
			"\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + id + "@ex>\n\nbody " + id + "\n\n"
	}
	inbox := filepath.Join(root, "Inbox")
	if err := os.WriteFile(inbox, []byte(msg("one")), 0o644); err != nil {
		t.Fatal(err)
	}

	argv := desktop.AddMailArgv(root, out)
	if argv[0] != "mailarchive" {
		t.Fatalf("the documented command starts with %q", argv[0])
	}
	// the parse: the command's own export flag set, the same one runExport builds
	fs := flag.NewFlagSet("mailarchive", flag.ContinueOnError)
	o := exportFlags(fs)
	if err := fs.Parse(argv[1:]); err != nil {
		t.Fatalf("the documented command does not parse: %v", err)
	}
	if len(o.inputs) != 1 || o.inputs[0] != root || *o.out != out || *o.mode != "incremental" || fs.NArg() != 0 {
		t.Errorf("parsed inputs=%v out=%q mode=%q extra=%v", o.inputs, *o.out, *o.mode, fs.Args())
	}
	afs := flag.NewFlagSet("mailarchive", flag.ContinueOnError)
	ao := exportFlags(afs)
	if err := afs.Parse(desktop.AddMailAutoArgv(out)[1:]); err != nil || !*ao.auto || *ao.out != out || afs.NArg() != 0 {
		t.Errorf("the documented -auto command: err=%v auto=%v out=%q extra=%v", err, *ao.auto, *ao.out, afs.Args())
	}

	// the run: first archive, then new mail arrives in Thunderbird, then the same command again
	if err := runExport(argv[1:]); err != nil {
		t.Fatalf("first run: %v", err)
	}
	count := func() int {
		m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		return m.Len()
	}
	if n := count(); n != 1 {
		t.Fatalf("after the first run the archive holds %d messages, want 1", n)
	}
	if err := os.WriteFile(inbox, []byte(msg("one")+msg("two")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runExport(argv[1:]); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := count(); n != 2 {
		t.Errorf("after the same command again the archive holds %d messages, want 2 (one new, none copied twice)", n)
	}
}
