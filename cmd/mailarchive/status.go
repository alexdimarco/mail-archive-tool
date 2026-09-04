package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// runStatus is the legibility surface (X6): the archive's completeness, last
// run and schedule, judged GREEN/WARN/RED with every WARN/RED naming its
// remedy (internal/health). stdout carries the answer (X8) and the exit is 0
// whenever it reports; a directory with neither manifest, descriptor nor
// last-run record is refused (a failed first run, recorded only in the last-run
// record, is still reportable).
func runStatus(args []string) error {
	fs := flag.NewFlagSet("mailarchive status", flag.ContinueOnError)
	fs.Usage = statusUsage(fs)
	out := fs.String("out", "", "archive directory (required)")
	name := fs.String("name", "", "schedule name to check (default: the archive's recorded schedule)")
	asJSON := fs.Bool("json", false, "print a typed, versioned JSON document instead of the text report (exit stays 0)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to report on)")
	}
	in := health.Gather(abspath(*out), *name)
	// A manifest FILE that exists but does not load (newer format, corrupt) is
	// an archive with a problem, not "no archive": let Assess RED it rather than
	// giving the wrong "run an export first" advice (friction #7a).
	if !in.HasManifestFile && !in.HasDescriptor && in.LastRunState == state.LastRunAbsent {
		return fmt.Errorf("no archive at %s: no manifest, schedule descriptor or last-run record (run an export into it first, or check the path)", in.Out)
	}
	// Extractability (design K4): compute the count of records with a preserved
	// original (.eml) present on disk via the shared app.ExtractableCount helper
	// — the SAME file-presence signal `extract` drains and `verify` counts, so
	// the three surfaces cannot diverge (QC1). health must not import app, so
	// status computes it here and passes it in. File presence only: it Lstats
	// each record's .eml, never hashing and never reading message bytes; posture
	// is unchanged (informational).
	if in.HasManifest {
		if m, lerr := state.Load(filepath.Join(in.Out, ".mailarchive-manifest.json")); lerr == nil {
			withEML, total := app.ExtractableCount(in.Out, m)
			in.Extractable = health.Extractable{WithEML: withEML, Total: total}
		}
	}
	rep := health.Assess(in, time.Now())
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(health.JSON(in, rep))
	}
	for _, line := range health.Summary(in, rep) {
		fmt.Println(line)
	}
	return nil
}

func statusUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive status - report an archive's completeness, last run and schedule

Usage:
  mailarchive status -out DIR [-name NAME]

Prints a GREEN / WARN / RED posture; every WARN or RED names its remedy. The
exit code is 0 whenever a report is produced (the posture is the answer).

Flags:
`)
		fs.PrintDefaults()
	}
}
