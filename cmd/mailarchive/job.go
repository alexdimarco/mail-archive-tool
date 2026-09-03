package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/outlookcom"
)

// The flag sets of the archiving verbs are built by these functions so that
// `schedule` can dry-parse a job through exactly the definitions the job will
// run with: a typo, a missing -out, or a flag that cannot run unattended is
// refused at schedule time, not at 02:00 (design-schedule-v2 P2).

type exportOpts struct {
	inputs          stringSlice
	out, mode       *string
	since, manifest *string
	log             *string
	copyFirst, auto *bool
	outlook         *bool
	outlookSyncWait *time.Duration
	index, pages    *bool
	keepRaw         *bool
	enableOffline   *bool
	syncWait        *bool
}

func exportFlags(fs *flag.FlagSet) *exportOpts {
	o := &exportOpts{}
	fs.Var(&o.inputs, "input", "PST/OST file or a directory to scan (repeatable, comma-separated)")
	o.out = fs.String("out", "", "output directory (required)")
	o.mode = fs.String("mode", "incremental", "export mode: incremental|full")
	o.since = fs.String("since", "", "only export items newer than this (e.g. 30d, 4w, 720h, 2026-07-01)")
	o.manifest = fs.String("manifest", "", "manifest path (default <out>/.mailarchive-manifest.json)")
	o.log = fs.String("log", "", "write the run log to this file (size-capped, rotated) instead of stderr — what scheduled jobs use")
	o.copyFirst = fs.Bool("copy-first", false, "copy each data file to a temp snapshot before reading (avoids locks when Outlook is open)")
	o.auto = fs.Bool("auto", false, "auto-discover mail stores (Outlook on Windows; Thunderbird and Evolution on any OS)")
	o.outlook = fs.Bool("outlook", false, "Windows + classic Outlook: have Outlook export each account to a .pst first, then archive that (use when a .ost can't be read directly)")
	o.outlookSyncWait = fs.Duration("outlook-sync-wait", 5*time.Minute, "with -outlook: run Send/Receive and wait up to this long for downloads before creating the PST (0 to skip)")
	o.index = fs.Bool("index", true, "build/update the full-text search index (search.db)")
	o.pages = fs.Bool("pages", true, "generate browsable folder index.html pages")
	o.keepRaw = fs.Bool("raw", false, "also keep each message's original RFC 822 bytes as <name>.eml beside the html (mbox/maildir/Graph sources; a .pst item has none)")
	o.enableOffline = fs.Bool("enable-offline", false, "Thunderbird IMAP: enable offline download in prefs.js so all mail can be synced (Thunderbird must be closed)")
	o.syncWait = fs.Bool("sync-wait", false, "Thunderbird IMAP: pause and wait for Download/Sync to finish before exporting")
	return o
}

type graphOpts struct {
	mailboxes             stringSlice
	out, tenant, clientID *string
	secretEnv, secretFile *string
	mode, since, log      *string
	index, pages, keepRaw *bool
}

func graphFlags(fs *flag.FlagSet) *graphOpts {
	o := &graphOpts{}
	o.out = fs.String("out", "", "output directory (required)")
	o.tenant = fs.String("tenant", "", "Microsoft 365 tenant id or domain (required)")
	o.clientID = fs.String("client-id", "", "Entra app (client) id (required)")
	o.secretEnv = fs.String("client-secret-env", "MAILARCHIVE_GRAPH_SECRET", "environment variable holding the app client secret")
	o.secretFile = fs.String("client-secret-file", "", "file holding the app client secret (a regular file readable only by you); takes precedence over the environment variable, and is what scheduled jobs must use")
	fs.Var(&o.mailboxes, "mailbox", "mailbox UPN to archive (repeatable, comma-separated) (required)")
	o.mode = fs.String("mode", "incremental", "export mode: incremental|full")
	o.since = fs.String("since", "", "only export items newer than this (e.g. 30d, 2026-07-01)")
	o.log = fs.String("log", "", "write the run log to this file (size-capped, rotated) instead of stderr")
	o.index = fs.Bool("index", true, "build/update the full-text search index (search.db)")
	o.pages = fs.Bool("pages", true, "generate browsable folder index.html pages")
	o.keepRaw = fs.Bool("raw", false, "also keep each message's original RFC 822 bytes as <name>.eml beside the html")
	return o
}

type reindexOpts struct {
	out, log *string
}

func reindexFlags(fs *flag.FlagSet) *reindexOpts {
	o := &reindexOpts{}
	o.out = fs.String("out", "", "export directory to reconcile (contains search.db) (required)")
	o.log = fs.String("log", "", "write the run log to this file (size-capped, rotated) instead of stderr")
	return o
}

// job is a validated, canonical backup job for the scheduler.
type job struct {
	verb string   // "" (export), "graph", or "reindex"
	args []string // the job's canonical flags, paths absolute
	out  string   // the archive directory
}

// command is the full argument vector the scheduled program receives.
func (j job) command() []string {
	if j.verb == "" {
		return j.args
	}
	return append([]string{j.verb}, j.args...)
}

var knownVerbs = map[string]bool{"serve": true, "search": true, "reindex": true, "schedule": true, "graph": true, "status": true}

// parseJob validates a job (the arguments after `--`, or the flat form
// re-assembled by schedule) through the real flag definitions and returns it in
// canonical, absolute form. Refusals name the problem and the remedy (R12).
func parseJob(args []string) (job, error) {
	verb := ""
	rest := args
	if len(args) > 0 && knownVerbs[args[0]] {
		verb, rest = args[0], args[1:]
	}
	switch verb {
	case "serve":
		return job{}, errors.New("serve is a long-running web server and cannot be a scheduled backup job; schedule an export, graph or reindex job")
	case "search", "status", "schedule":
		return job{}, fmt.Errorf("%s is a query, not a backup job, and cannot be scheduled; schedule an export, graph or reindex job", verb)
	}

	fs := flag.NewFlagSet("job", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	switch verb {
	case "":
		o := exportFlags(fs)
		if err := fs.Parse(rest); err != nil {
			return job{}, fmt.Errorf("job: %v (a scheduled job runs exactly these flags; fix them now)", err)
		}
		o.inputs = append(o.inputs, fs.Args()...)
		if *o.enableOffline || *o.syncWait {
			return job{}, errors.New("-enable-offline / -sync-wait are interactive (they wait for you and for Thunderbird) and cannot run unattended: do the one-time offline prep by hand (README → IMAP), then schedule the export without them")
		}
		if *o.outlook && runtime.GOOS != "windows" {
			return job{}, outlookcom.ErrUnsupported
		}
		if *o.out == "" {
			return job{}, errors.New("-out is required (the directory the scheduled backup writes to)")
		}
		if _, err := parseMode(*o.mode); err != nil {
			return job{}, err
		}
		if *o.since != "" {
			if _, err := parseSince(*o.since); err != nil {
				return job{}, err
			}
		}
		j := job{out: abspath(*o.out)}
		j.args = []string{"-out", j.out, "-mode", strings.ToLower(strings.TrimSpace(*o.mode))}
		if *o.since != "" {
			j.args = append(j.args, "-since", *o.since)
		}
		if *o.auto {
			j.args = append(j.args, "-auto")
		}
		for _, in := range o.inputs {
			j.args = append(j.args, "-input", abspath(in))
		}
		if *o.copyFirst {
			j.args = append(j.args, "-copy-first")
		}
		if *o.outlook {
			j.args = append(j.args, "-outlook", "-outlook-sync-wait", o.outlookSyncWait.String())
		}
		if !*o.index {
			j.args = append(j.args, "-index=false")
		}
		if !*o.pages {
			j.args = append(j.args, "-pages=false")
		}
		if *o.keepRaw {
			j.args = append(j.args, "-raw")
		}
		if *o.manifest != "" {
			j.args = append(j.args, "-manifest", abspath(*o.manifest))
		}
		if *o.log != "" {
			j.args = append(j.args, "-log", abspath(*o.log))
		}
		return j, nil

	case "graph":
		o := graphFlags(fs)
		if err := fs.Parse(rest); err != nil {
			return job{}, fmt.Errorf("graph job: %v (a scheduled job runs exactly these flags; fix them now)", err)
		}
		o.mailboxes = append(o.mailboxes, fs.Args()...)
		switch {
		case *o.out == "":
			return job{}, errors.New("-out is required (the output directory)")
		case *o.tenant == "":
			return job{}, errors.New("-tenant is required (the Microsoft 365 tenant id or domain)")
		case *o.clientID == "":
			return job{}, errors.New("-client-id is required (the Entra app id)")
		case len(o.mailboxes) == 0:
			return job{}, errors.New("-mailbox is required (at least one mailbox UPN to archive)")
		case *o.secretFile == "":
			return job{}, errors.New("a scheduled graph job has no environment to carry the secret: pass -client-secret-file PATH (a file readable only by you)")
		}
		if _, err := readSecret(*o.secretFile); err != nil {
			return job{}, err
		}
		if _, err := parseMode(*o.mode); err != nil {
			return job{}, err
		}
		if *o.since != "" {
			if _, err := parseSince(*o.since); err != nil {
				return job{}, err
			}
		}
		j := job{verb: "graph", out: abspath(*o.out)}
		j.args = []string{"-out", j.out, "-tenant", *o.tenant, "-client-id", *o.clientID, "-client-secret-file", abspath(*o.secretFile), "-mode", strings.ToLower(strings.TrimSpace(*o.mode))}
		for _, m := range o.mailboxes {
			j.args = append(j.args, "-mailbox", m)
		}
		if *o.since != "" {
			j.args = append(j.args, "-since", *o.since)
		}
		if !*o.index {
			j.args = append(j.args, "-index=false")
		}
		if !*o.pages {
			j.args = append(j.args, "-pages=false")
		}
		if *o.keepRaw {
			j.args = append(j.args, "-raw")
		}
		if *o.log != "" {
			j.args = append(j.args, "-log", abspath(*o.log))
		}
		return j, nil

	case "reindex":
		o := reindexFlags(fs)
		if err := fs.Parse(rest); err != nil {
			return job{}, fmt.Errorf("reindex job: %v", err)
		}
		if *o.out == "" {
			return job{}, errors.New("-out is required (the export directory to reconcile)")
		}
		j := job{verb: "reindex", out: abspath(*o.out)}
		j.args = []string{"-out", j.out}
		if *o.log != "" {
			j.args = append(j.args, "-log", abspath(*o.log))
		}
		return j, nil
	}
	return job{}, fmt.Errorf("unknown job %q", verb)
}

// readSecret reads an app client secret from a file the way both `graph` (at
// run time) and `schedule` (at schedule time) must: a regular file only (no
// symlink, FIFO or device — a FIFO would hang an unattended job forever), not
// readable by group/others on Unix, at most 4 KB, non-empty after trimming.
func readSecret(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("client secret file %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("client secret file %s must be a regular file (not a symlink, pipe or device)", path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("client secret file %s is readable by other users (mode %04o): run `chmod 600 %s`", path, fi.Mode().Perm(), path)
	}
	if fi.Size() > 4096 {
		return "", fmt.Errorf("client secret file %s is %d bytes; a client secret is far smaller — is this the right file?", path, fi.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("client secret file %s: %w", path, err)
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("client secret file %s is empty", path)
	}
	return secret, nil
}

// parseSince validates a -since value against the current time.
func parseSince(s string) (time.Time, error) {
	return parseSinceAt(s, time.Now())
}

// jobLogPath is where a scheduled job's operator log lives: beside the archive,
// named after the schedule.
func jobLogPath(out, name string) string {
	return filepath.Join(out, name+".log")
}
