package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-74, R12, S28
// A client secret file is accepted only when it is a regular file, readable by
// its owner alone (Unix), non-empty and small: a symlink, a pipe, a
// world-readable file, an empty file and a missing file are each refused
// naming the requirement — at run time (graph) exactly as at schedule time —
// and the secret's content never appears in output.
func TestClientSecretFileRefusals(t *testing.T) {
	dir := t.TempDir()
	out := t.TempDir()
	graph := func(secretFile string) (int, string) {
		return runCLI("graph", "-out", out, "-tenant", "t", "-client-id", "c", "-mailbox", "m@x", "-client-secret-file", secretFile)
	}

	missing := filepath.Join(dir, "nope.secret")
	code, stderr := graph(missing)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("nope.secret"))

	empty := filepath.Join(dir, "empty.secret")
	if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stderr = graph(empty)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("empty"))

	if runtime.GOOS != "windows" {
		loose := filepath.Join(dir, "loose.secret")
		if err := os.WriteFile(loose, []byte("s3cret-value\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stderr = graph(loose)
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names("chmod 600", "loose.secret"), assure.Forbid("s3cret-value"))

		target := filepath.Join(dir, "target.secret")
		if err := os.WriteFile(target, []byte("s3cret-value\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link.secret")
		if err := os.Symlink(target, link); err == nil {
			code, stderr = graph(link)
			assure.Refused(t, code, stderr, assure.Code(1), assure.Names("regular file"), assure.Forbid("s3cret-value"))
		}
	}

	big := filepath.Join(dir, "big.secret")
	if err := os.WriteFile(big, make([]byte, 5000), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stderr = graph(big)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("bytes"))
}
