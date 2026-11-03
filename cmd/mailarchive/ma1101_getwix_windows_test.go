package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// getWiXRun runs a copy of packaging/mailarchive-desktop/Get-WiX.ps1, with pin as the
// wix314-binaries.sha256 beside it, on the local zip (-ZipPath: nothing is downloaded),
// expanding into dest. It returns the exit code and everything the script printed.
func getWiXRun(t *testing.T, pin, zipPath, dest string) (int, string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), "packaging", "mailarchive-desktop", "Get-WiX.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "Get-WiX.ps1")
	if err := os.WriteFile(script, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wix314-binaries.sha256"), []byte(pin), 0o644); err != nil {
		t.Fatal(err)
	}
	// The script's own throw is caught and its message written on one line: PowerShell's
	// error view would wrap it at the window width and echo the throw's source line.
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	cmd := "$ErrorActionPreference = 'Stop'; try { & " + q(script) + " -Destination " + q(dest) +
		" -ZipPath " + q(zipPath) + "; exit 0 } catch { [Console]::Out.WriteLine('REFUSED: ' + $_.Exception.Message); exit 1 }"
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", cmd).CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	default:
		t.Fatalf("run powershell.exe: %v", err)
		return -1, ""
	}
}

// emptyOrAbsent reports whether dir does not exist or holds nothing.
func emptyOrAbsent(dir string) bool {
	es, err := os.ReadDir(dir)
	return errors.Is(err, os.ErrNotExist) || (err == nil && len(es) == 0)
}

// covers: MA-1101
// Get-WiX.ps1 run for real (Windows only; ci.yml's windows-latest test run), on a
// stand-in wix314-binaries.zip given with -ZipPath, so nothing is downloaded. Positive
// twin first: a pin equal to the zip's SHA-256 (written in lower case) expands it and
// candle.exe and light.exe are there. Then each refusal exits 1, names the pin file or
// the mismatch, and leaves nothing expanded: the placeholder the pin file ships with; a
// sha256sum-style line (hash and file name); a well-formed pin of another file.
//
// Prove-fail: neutralise the hash comparison (`if ($actual -ne $pinned -and $false) {`)
// and the wrong-hash case expands the zip and reddens; neutralise the pin-format check
// the same way and the placeholder case reaches the hash comparison, whose message does
// not name the pin's form, and reddens.
func TestMA1101_GetWiXRefusesAnUncheckedZipOnWindows(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skipf("no powershell.exe on this Windows host: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "wix314-binaries.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"candle.exe", "light.exe", "WixUtilExtension.dll"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("stand-in for " + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	good := hex.EncodeToString(sum[:])
	const header = "# a comment line, as the real pin file has\n"

	// Positive twin: the pinned zip is expanded.
	dest := filepath.Join(t.TempDir(), "wix")
	rc, out := getWiXRun(t, header+good+"\n", zipPath, dest)
	if rc != 0 {
		t.Fatalf("positive twin: Get-WiX.ps1 refused the pinned zip (exit %d):\n%s", rc, out)
	}
	for _, name := range []string{"candle.exe", "light.exe"} {
		if _, err := os.Stat(filepath.Join(dest, name)); err != nil {
			t.Fatalf("positive twin: %s not expanded: %v\n%s", name, err, out)
		}
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("Get-WiX.ps1 deleted the -ZipPath it was given: %v", err)
	}

	other := sha256.Sum256([]byte("another file"))
	for _, c := range []struct {
		name, pin string
		names     []string
	}{
		{"the shipped placeholder", header + "PLACEHOLDER-NOT-A-SHA256-REPLACE-WITH-THE-CHECKED-VALUE\n",
			[]string{"REFUSED: wix314-binaries.sha256 does not hold a SHA-256", "Refusing to download and use WiX unchecked"}},
		{"a sha256sum-style line", header + good + "  wix314-binaries.zip\n",
			[]string{"REFUSED: wix314-binaries.sha256 does not hold a SHA-256", "Refusing to download and use WiX unchecked"}},
		{"a pin of another file", header + strings.ToUpper(hex.EncodeToString(other[:])) + "\n",
			[]string{"REFUSED: The downloaded wix314-binaries.zip has SHA-256 " + strings.ToUpper(good), "Refusing to use it"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "wix")
			rc, out := getWiXRun(t, c.pin, zipPath, dest)
			assure.Refused(t, rc, out, assure.Code(1), assure.Names(c.names...),
				assure.NoSideEffect(func() bool { return emptyOrAbsent(dest) }))
		})
	}
}
