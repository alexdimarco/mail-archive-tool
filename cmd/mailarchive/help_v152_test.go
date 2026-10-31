package main

import (
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// covers: MA-939, S59
// The command line's help says what this version does (the wording pass before 1.5.2).
// Positive twin first: the root usage still lists the verbs it listed. It now also lists
// pause, resume, uninstall and setup, which the program runs; the "-out is required"
// error names every verb; graph's help names device sign-in beside app sign-in; the
// wizard's help offers a daily or weekly schedule; and -thunderbird-sync's help says a
// run typed at a terminal skips the sync (the start check skips it while the keyboard or
// mouse was used in the last 2 minutes), so it is meant for scheduled runs.
func TestTheHelpSaysWhatThisVersionDoes(t *testing.T) {
	usage := captureExportUsage(t)
	for _, want := range []string{"mailarchive serve ", "mailarchive desktop ", "mailarchive wizard ", "mailarchive run-job "} {
		if !strings.Contains(usage, want) {
			t.Fatalf("positive twin: the root usage lost %q", want)
		}
	}
	for _, want := range []string{
		"  mailarchive pause    -out DIR [-reason TEXT]           stop backups and make the archive read-only\n",
		"  mailarchive resume   -out DIR                          make a paused archive writable and restore its schedule\n",
		"  mailarchive uninstall -dry-run | -yes                  remove MailArchive's schedules, settings and sign-in (archives are kept)\n",
		"  mailarchive setup                                      set up a Microsoft 365 connection in your browser\n",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("the root usage lacks %q", want)
		}
	}

	err := runExport(nil)
	if err == nil || err.Error() != "-out is required (or use a subcommand: desktop, wizard, run-job, setup, serve, search, reindex, schedule, graph, status, verify, extract, decrypt, pause, resume, uninstall, change-password, new-recovery-key, restore-key-file, forget-key, forget-archive, repair-record, encrypt)" {
		t.Errorf("no -out: %v", err)
	}

	graph := captureStderr(t, func() { graphUsage(flag.NewFlagSet("mailarchive graph", flag.ContinueOnError))() })
	for _, want := range []string{
		"mailarchive graph - archive Microsoft 365 mailboxes via Graph\n",
		"  mailarchive graph -auth device -out DIR -tenant TENANT -client-id APPID   (sign in as yourself, your own mailbox)\n",
		"device sign-in needs only delegated Mail.Read for your own mailbox.",
	} {
		if !strings.Contains(graph, want) {
			t.Errorf("graph's help lacks %q:\n%s", want, graph)
		}
	}
	if strings.Contains(graph, "(app-only)") {
		t.Errorf("graph's help still calls the verb app-only:\n%s", graph)
	}

	if !strings.Contains(wizardVerbHelp, "(optionally) a daily or weekly backup schedule") || strings.Contains(wizardVerbHelp, "a weekly\nbackup schedule") {
		t.Errorf("the wizard's help: %q", wizardVerbHelp)
	}

	fs := flag.NewFlagSet("mailarchive", flag.ContinueOnError)
	exportFlags(fs)
	sync := fs.Lookup("thunderbird-sync").Usage
	for _, want := range []string{
		"It starts Thunderbird only when no one has used the keyboard or mouse for 2 minutes, so it is meant for scheduled runs: typed at a terminal it skips the sync and archives the mail already on this computer.",
		"allowed on any schedule",
		"a sync problem never stops the archive",
	} {
		if !strings.Contains(sync, want) {
			t.Errorf("-thunderbird-sync's help lacks %q: %q", want, sync)
		}
	}
}

// captureStderr runs f and returns what it wrote to stderr.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	f()
	os.Stderr = old
	_ = w.Close()
	b, _ := io.ReadAll(r)
	_ = r.Close()
	return string(b)
}
