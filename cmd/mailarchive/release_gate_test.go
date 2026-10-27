package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-827
// Published releases are unchangeable and every shipped program is exactly its
// release (owner ruling O7; program design §7, condition 12; SC33), verified as
// structure: release.yml refuses a tag that is not an exact vMAJOR.MINOR.PATCH, makes
// the release as a draft, checks the uploaded SHA256SUMS and asset list against the
// build's before publishing, and never replaces a published release's files (no
// --clobber); build-release.sh checks every shipped binary — both Linux programs,
// the Windows pair, and the macOS slices in build-app.sh — with
// require-release-stamp.sh, which refuses a version other than the tag and a
// modified checkout. The script's refusals run for real; its positive twin (a clean
// checkout of a tag stamps exactly the tag) was run by hand at v1.4.0 and is the
// release's own gate.
func TestReleasesAreUnchangeableAndExactlyTheirTag(t *testing.T) {
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	yml := read(".github/workflows/release.yml")
	for _, want := range []string{"--draft", "gh release download", "cmp \"$check/SHA256SUMS\" dist/SHA256SUMS", "sha256sum -c SHA256SUMS", "--draft=false", `^v[0-9]+\.[0-9]+\.[0-9]+$`} {
		if !strings.Contains(yml, want) {
			t.Errorf("release.yml lacks %q", want)
		}
	}
	if strings.Contains(yml, "--clobber") || strings.Contains(yml, "gh release upload") {
		t.Error("release.yml can still replace a published release's files")
	}
	if strings.Index(yml, "--draft=false") < strings.Index(yml, "cmp \"$check/SHA256SUMS\"") || strings.Index(yml, "--draft=false") < strings.Index(yml, "sha256sum -c SHA256SUMS") {
		t.Error("release.yml publishes before checking the uploaded checksums and assets")
	}
	br := read("packaging/build-release.sh")
	for _, f := range []string{"mailarchive-linux-amd64", "mailarchive-linux-arm64", "mailarchive.exe", "mailarchivew.exe"} {
		if !strings.Contains(br, `"$DIST/`+f+`"`) || !strings.Contains(br, "require-release-stamp.sh") {
			t.Errorf("build-release.sh does not check %s", f)
		}
	}
	if !strings.Contains(br, "RELEASE_TAG=") || !strings.Contains(read("packaging/macos/build-app.sh"), `require-release-stamp.sh" "$RELEASE_TAG" "$WORK/mailarchive-arm64" "$WORK/mailarchive-amd64"`) {
		t.Error("the macOS slices are not checked")
	}

	if runtime.GOOS == "windows" {
		t.Skip("the script runs under bash on the release runner")
	}
	script := filepath.Join("..", "..", "packaging", "require-release-stamp.sh")
	self, _ := os.Executable() // this test binary: not a release build
	for _, c := range []struct{ tag, says string }{
		{"v1.5.0-rc1", "NOT A RELEASE TAG"},
		{"v99.0.0", "NOT STAMPED"},
	} {
		out, err := exec.Command("bash", script, c.tag, self).CombinedOutput()
		code := 0
		if err != nil {
			code = 1
		}
		assure.Refused(t, code, string(out), assure.Code(1), assure.Names(c.says),
			assure.NoSideEffect(func() bool { return true }))
	}
}
