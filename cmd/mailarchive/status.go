package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"mail-archive-tool/internal/health"
)

// runStatus is the legibility surface (X6): the archive's completeness, last
// run and schedule, judged GREEN/WARN/RED with every WARN/RED naming its
// remedy (internal/health). stdout carries the answer (X8) and the exit is 0
// whenever it reports; a directory with neither manifest nor descriptor is
// refused.
func runStatus(args []string) error {
	fs := flag.NewFlagSet("mailarchive status", flag.ContinueOnError)
	fs.Usage = statusUsage(fs)
	out := fs.String("out", "", "archive directory (required)")
	name := fs.String("name", "", "schedule name to check (default: the archive's recorded schedule)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to report on)")
	}
	in := health.Gather(abspath(*out), *name)
	if !in.HasManifest && !in.HasDescriptor {
		return fmt.Errorf("no archive at %s: no manifest and no schedule descriptor (run an export into it first, or check the path)", in.Out)
	}
	for _, line := range health.Summary(in, health.Assess(in, time.Now())) {
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
