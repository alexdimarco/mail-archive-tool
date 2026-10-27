package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/vault"
)

// TestMain points MailArchive's scratch folder at a throwaway folder for the whole
// package, so a keyed -copy-first run never writes into the developer's real cache
// folder (job.ScratchDir). useScratchRoot still overrides it per test. It does the
// same for this computer's record of encrypted archives, which every keyless
// writer reads. Its tests make keys at the cheap test cost (MA-776).
func TestMain(m *testing.M) {
	vault.UseTestCost()
	root, err := os.MkdirTemp("", "mailarchive-app-scratch-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: create the scratch folder:", err)
		os.Exit(1)
	}
	scratchRoot = func() (string, error) { return root, nil }
	// The copy's plan reads the archives this computer knows from the real config
	// folder; the package's tests know none unless one says so.
	knownArchives = func() []string { return nil }
	// The program copies (program design R22): a throwaway cache, checked untouched at
	// the end, and a small file standing in for this program, so a test's self-copy is
	// not the whole test binary.
	cacheDone := progcache.UseTestRoot(filepath.Join(root, "program-cache"))
	stand := filepath.Join(root, "mailarchive-test-program")
	os.WriteFile(stand, []byte("#!/bin/sh\necho mailarchive test program\n"), 0o755)
	standFI, _ := os.Stat(stand)
	selfExecutable = func() (string, os.FileInfo) { return stand, standFI }
	restore := encrecord.SetDirForTest(filepath.Join(root, "record"))
	code := m.Run()
	restore()
	if err := cacheDone(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		code = 1
	}
	os.RemoveAll(root)
	os.Exit(code)
}
