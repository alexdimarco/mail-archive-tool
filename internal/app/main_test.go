package app

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points MailArchive's scratch folder at a throwaway folder for the whole
// package, so a keyed -copy-first run never writes into the developer's real cache
// folder (job.ScratchDir). useScratchRoot still overrides it per test.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mailarchive-app-scratch-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: create the scratch folder:", err)
		os.Exit(1)
	}
	scratchRoot = func() (string, error) { return root, nil }
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}
