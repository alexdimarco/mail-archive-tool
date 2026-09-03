package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// buildVerifyArchive writes a small archive with recorded fixity (one plain
// message and one with an attachment) so the built binary's `verify` has real
// files and digests to check.
func buildVerifyArchive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	m, err := state.Load(filepath.Join(dir, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	exp := &export.Exporter{OutDir: dir, Manifest: m, Log: log.New(io.Discard, "", 0)}
	plain := &model.Message{Subject: "plain", SenderEmail: "a@x", Received: time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC), InternetMessageID: "<p@x>", HTMLBody: "<p>plain</p>"}
	withAtt := &model.Message{Subject: "attach", SenderEmail: "a@x", Received: time.Date(2025, 3, 3, 9, 1, 0, 0, time.UTC), InternetMessageID: "<a@x>", HTMLBody: "<p>attach</p>",
		Attachments: []model.Attachment{{Filename: "a.bin", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("bytes")); return int64(n), nil }}}}
	for _, msg := range []*model.Message{plain, withAtt} {
		if _, err := exp.Export("store", []string{"Inbox"}, msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func firstHTML(t *testing.T, dir string) string {
	t.Helper()
	var found string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".html") && d.Name() != "index.html" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatal("no archived html to damage")
	}
	return found
}

// covers: MA-135, MA-136, MA-140, R12, S31
// The real binary's `verify` exit code is the verdict: 0 attested on a fresh
// archive, 2 not attested after a byte is flipped, 1 on refusal (no manifest, or
// a locked archive naming the lock and the holder verb). `-json` emits the
// coverage document on stdout. Positive twin: a fresh archive verifies clean.
func TestVerifyCLIExitCodes(t *testing.T) {
	dir := buildVerifyArchive(t)

	// Attested → exit 0.
	if code, stderr := runCLI("verify", "-out", dir); code != 0 {
		t.Fatalf("fresh archive did not verify clean (exit %d): %s", code, stderr)
	}

	// -json: the coverage document goes to stdout, exit still 0.
	code, stdout, _ := runCLIOut(t, "verify", "-out", dir, "-json")
	if code != 0 {
		t.Fatalf("verify -json on a clean archive exited %d", code)
	}
	for _, key := range []string{`"records"`, `"with_fixity"`, `"checked"`, `"problems"`} {
		if !strings.Contains(stdout, key) {
			t.Errorf("verify -json stdout lacks %s:\n%s", key, stdout)
		}
	}

	// Refusal on a directory with no manifest → exit 1, named.
	empty := t.TempDir()
	rc, stderr := runCLI("verify", "-out", empty)
	assure.Refused(t, rc, stderr, assure.Code(1), assure.Names("no archive", "manifest"))

	// A locked archive → exit 1 naming the lock and the holder verb; no report.
	held, err := lockfile.AcquireAs(filepath.Join(dir, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	rc, stderr = runCLI("verify", "-out", dir)
	assure.Refused(t, rc, stderr, assure.Code(1), assure.Names("in use", lockfile.Name, "export"))
	held.Release()

	// Flip a byte in an archived file → not attested → exit 2.
	victim := firstHTML(t, dir)
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(victim, data, 0o644); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(dir, victim)
	code, stdout, _ = runCLIOut(t, "verify", "-out", dir)
	if code != 2 {
		t.Fatalf("a modified file must exit 2, got %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "modified") || !strings.Contains(stdout, filepath.ToSlash(rel)) {
		t.Errorf("verify did not name the modified file on stdout:\n%s", stdout)
	}
}
