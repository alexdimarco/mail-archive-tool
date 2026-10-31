package main

import (
	"flag"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	jobfile "mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// posixWords splits a command the way a POSIX shell would for the quoting status prints
// (health's shellQuoteToken): blanks separate words, '…' is literal, and a backslash
// outside quotes takes the next character as it is.
func posixWords(t *testing.T, s string) []string {
	t.Helper()
	var words []string
	var cur strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted:
			if c == '\'' {
				quoted = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			quoted, inWord = true, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quoted {
		t.Fatalf("an unclosed quote in %q", s)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// statusHandCommand is the command status's 3-day WARN tells the person to type, or "".
func statusHandCommand(t *testing.T, in health.Input) string {
	t.Helper()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	rep := health.Assess(in, now)
	for i, c := range rep.Codes {
		if c != "presync_not_synced" {
			continue
		}
		msg := rep.Reasons[i]
		const lead = "by hand with `"
		at := strings.Index(msg, lead)
		if at < 0 {
			t.Fatalf("the WARN names no command: %q", msg)
		}
		rest := msg[at+len(lead):]
		end := strings.IndexByte(rest, '`')
		if end < 0 {
			t.Fatalf("the command is not closed: %q", msg)
		}
		return rest[:end]
	}
	t.Fatalf("no presync_not_synced WARN: %v", rep.Reasons)
	return ""
}

// staleSyncInput is an archive whose schedule syncs Thunderbird and has missed it for 3
// days, read by status at a terminal, in POSIX words.
func staleSyncInput(out string) health.Input {
	t0 := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	s := state.SyncStreak{}
	for d := 0; d <= 3; d++ {
		s = s.Miss("cannot-start", t0.Add(time.Duration(d)*24*time.Hour))
	}
	return health.Input{Out: out, GOOS: "darwin", HasDescriptor: true, SyncStreak: s,
		Desc:     schedule.Descriptor{Name: "mailarchive-1234abcd", Interval: "daily", At: "02:00"},
		PIDAlive: func(int) bool { return true }}
}

// covers: MA-909, MA-926
// The whole command status prints for backing up an archive by hand with its sync (the
// friction review's O2, applied by the lead 2026-10-31) is a command this program takes,
// checked by a dry parse — no backup is run. For a command-line schedule, made the way
// `mailarchive schedule` records it (parseJob's canonical job, -thunderbird-sync added as
// parseJob adds it on macOS and Windows), the printed words route to the export, and the
// export's own flags parse them to this archive's folder, its mail folder and the sync, with
// nothing left over. For a Desktop- or wizard-made schedule, they route to run-job with that
// job file, which carries the archive and the sync.
func TestTheStatusSyncCommandParses(t *testing.T) {
	dir := t.TempDir()
	out, mail := filepath.Join(dir, "archive"), filepath.Join(dir, "Thunderbird", "Local Folders")
	j, err := parseJob([]string{"-input", mail, "-out", out})
	if err != nil {
		t.Fatal(err)
	}
	args := append(j.command(), "-thunderbird-sync")
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		synced, err := parseJob([]string{"-input", mail, "-out", out, "-thunderbird-sync"})
		if err != nil || strings.Join(synced.command(), "\x00") != strings.Join(args, "\x00") {
			t.Fatalf("parseJob with the sync: %v, %q; want %q", err, synced.command(), args)
		}
	}
	in := staleSyncInput(out)
	in.Desc.Job = args
	in.Desc.Presync = &schedule.PresyncDesc{Kind: "thunderbird"}
	words := posixWords(t, statusHandCommand(t, in))
	if len(words) < 2 || words[0] != "mailarchive" {
		t.Fatalf("the command %q does not start with the program", words)
	}
	if r := resolveInvocation("mailarchive", words[1:]); r.verb != "" {
		t.Fatalf("the command routes to %q, want the export", r.verb)
	}
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := exportFlags(fs)
	if err := fs.Parse(words[1:]); err != nil {
		t.Fatalf("the export's flags refuse the printed command %q: %v", words, err)
	}
	if *o.out != out || !*o.tbSync || !*o.unattended || len(o.inputs) != 1 || o.inputs[0] != mail || fs.NArg() != 0 {
		t.Errorf("the printed command parses to out %q, sync %v, unattended %v, inputs %q, %d left over; want %q, true, true, [%q], 0",
			*o.out, *o.tbSync, *o.unattended, o.inputs, fs.NArg(), out, mail)
	}

	// A Desktop- or wizard-made schedule: run-job with its job file.
	jp := filepath.Join(dir, "jobs", "mailarchive-1234abcd.json")
	if err := jobfile.Write(jp, jobfile.Job{Name: "mailarchive-1234abcd", Inputs: []string{mail}, Out: out, Mode: "incremental", ThunderbirdSync: &jobfile.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	rj := staleSyncInput(out)
	rj.Desc.Job = []string{"run-job", jp}
	rj.KindEv = schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{schedule.KindRunJob: true}, JobPath: jp}
	words = posixWords(t, statusHandCommand(t, rj))
	if len(words) < 2 || words[0] != "mailarchive" {
		t.Fatalf("the run-job command %q does not start with the program", words)
	}
	if r := resolveInvocation("mailarchive", words[1:]); r.verb != "run-job" || len(r.args) != 1 || r.args[0] != jp {
		t.Errorf("the run-job command routes to %q %q, want run-job [%q]", r.verb, r.args, jp)
	}
	if got, err := jobfile.Read(jp); err != nil || got.Out != out || got.ThunderbirdSync == nil {
		t.Errorf("the job file it names: %+v, %v; want this archive with the sync", got, err)
	}
}
