// Package testhome keeps a test package away from this computer's real MailArchive settings.
//
// Tests must never reach the owner's real settings folder, not even to read it: what a test finds
// there would make its result depend on the owner's own state, and a test that writes there has
// leaked into it before (MA-894). Every test package whose tests can reach a lookup in that folder
// (the backup engine's tidy-up wait, the Gmail answer, job files) points this computer's home,
// settings, cache and state folders at a throw-away folder for the whole package in its TestMain,
// and runs its tests through Run, which checks with Contained that MailArchive's settings folder
// (job.Dir) resolves inside that folder, before and after the tests. Contained resolves the path;
// it never opens it. assureblock's meta-test fails when such a package's TestMain does not use
// this package. It computes no folder of its own: the settings folder is job.Dir's.
package testhome

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
)

// Contained returns an error unless MailArchive's settings folder, as this process resolves it
// now (job.Dir), lies inside root. It reads no file.
func Contained(root string) error {
	dir, err := job.Dir()
	if err != nil {
		return fmt.Errorf("the settings folder cannot be resolved (%v)", err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(abs, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("MailArchive's settings folder resolves to %s, outside the throw-away folder %s: the tests would reach this computer's real settings", dir, abs)
	}
	return nil
}

// Run is for a TestMain that has pointed this computer's folders inside root: it checks
// Contained, runs the tests only when it passed, checks again after them, and returns the exit
// code (non-zero when a check failed).
func Run(root string, m interface{ Run() int }) int {
	if code := Check(root, 0); code != 0 {
		return code
	}
	return Check(root, m.Run())
}

// Check reports an error and makes code non-zero when the settings folder does not resolve
// inside root.
func Check(root string, code int) int {
	if err := Contained(root); err != nil {
		fmt.Fprintln(os.Stderr, "testhome:", err)
		return 1
	}
	return code
}
