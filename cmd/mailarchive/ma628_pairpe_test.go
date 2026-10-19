package main

import (
	"debug/buildinfo"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// Windows PE image subsystem values (winnt.h IMAGE_SUBSYSTEM_*).
const (
	peSubsystemCUI = 3 // WINDOWS_CUI — a console build
	peSubsystemGUI = 2 // WINDOWS_GUI — a no-console build (-ldflags -H=windowsgui)
)

// covers: MA-628, R33, S59
// The Windows pair is one program built two ways. The two GOOS=windows go build
// lines READ FROM build-release.sh, run into a tempdir, produce a console build
// (PE subsystem 3, mailarchive.exe) and a no-console build (PE subsystem 2,
// mailarchivew.exe) — the only difference between the lines being the -o name and
// -ldflags -H=windowsgui. The build-info EQUALITY (same path / main module / deps /
// settings except -ldflags) is SE2's SameBuild (§4.6.0), a per-case skip until then.
func TestMA628_PairBuildSubsystem(t *testing.T) {
	root := moduleRoot(t)
	br, err := os.ReadFile(filepath.Join(root, "packaging", "build-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(br)

	// Resolve $ONE (the one program's package) and the two GOOS=windows build lines.
	one := "./cmd/mailarchive"
	if m := regexp.MustCompile(`(?m)^ONE=(\S+)`).FindStringSubmatch(script); m != nil {
		one = m[1]
	}
	lines := regexp.MustCompile(`(?m)^\s*GOOS=windows GOARCH=amd64 go build (.*)\$ONE.*$`).FindAllStringSubmatch(script, -1)
	if n := len(assure.Reached(t, lines, "GOOS=windows build lines in build-release.sh")); n != 2 {
		t.Fatalf("want exactly 2 GOOS=windows go build $ONE lines, got %d", n)
	}

	tmp := t.TempDir()
	built := map[string]int{} // install name -> PE subsystem
	for _, m := range lines {
		mid := m[1] // the text between "go build " and "$ONE": the -o target and any -ldflags
		on := regexp.MustCompile(`-o "\$DIST/([^"]+)"`).FindStringSubmatch(mid)
		if on == nil {
			t.Fatalf("no -o \"$DIST/NAME\" in build line: %s", strings.TrimSpace(m[0]))
		}
		name := on[1]
		args := []string{"build"}
		if strings.Contains(mid, "-ldflags -H=windowsgui") {
			args = append(args, "-ldflags", "-H=windowsgui")
		}
		outPath := filepath.Join(tmp, name)
		args = append(args, "-o", outPath, one)
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64")
		if b, berr := cmd.CombinedOutput(); berr != nil {
			t.Fatalf("building %s failed: %v\n%s", name, berr, b)
		}
		built[name] = peSubsystem(t, outPath)
	}

	if got := built["mailarchive.exe"]; got != peSubsystemCUI {
		t.Errorf("mailarchive.exe PE subsystem = %d, want %d (console build)", got, peSubsystemCUI)
	}
	if got := built["mailarchivew.exe"]; got != peSubsystemGUI {
		t.Errorf("mailarchivew.exe PE subsystem = %d, want %d (no-console build)", got, peSubsystemGUI)
	}

	// The two lines differ only in the -o name and -ldflags -H=windowsgui.
	l0, l1 := normalizeBuildLine(lines[0][1]), normalizeBuildLine(lines[1][1])
	if l0 != l1 {
		t.Errorf("the two GOOS=windows build lines differ by more than -o name and -ldflags -H=windowsgui:\n  %q\n  %q", lines[0][1], lines[1][1])
	}

	// The pair is the same build of the one program — equal but for -ldflags (SC7,
	// §4.6.0). SameBuild's false cases are proven by the unit table in
	// internal/schedule; here it runs on the two real binaries just built.
	ic, cerr := buildinfo.ReadFile(filepath.Join(tmp, "mailarchive.exe"))
	ig, gerr := buildinfo.ReadFile(filepath.Join(tmp, "mailarchivew.exe"))
	if cerr != nil || gerr != nil {
		t.Fatalf("reading the pair's build info: console=%v gui=%v", cerr, gerr)
	}
	if !schedule.SameBuild(ic, ig) {
		t.Error("the built pair is not the same build except -ldflags (SC7, MA-628)")
	}
}

// normalizeBuildLine strips the -o target and -ldflags -H=windowsgui so the two
// build lines compare equal when they differ only in those (SC23).
func normalizeBuildLine(mid string) string {
	s := regexp.MustCompile(`-o "\$DIST/[^"]+"`).ReplaceAllString(mid, "")
	s = strings.ReplaceAll(s, "-ldflags -H=windowsgui", "")
	return strings.Join(strings.Fields(s), " ")
}

func peSubsystem(t *testing.T, path string) int {
	t.Helper()
	f, err := pe.Open(path)
	if err != nil {
		t.Fatalf("pe.Open %s: %v", path, err)
	}
	defer f.Close()
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		return int(oh.Subsystem)
	case *pe.OptionalHeader32:
		return int(oh.Subsystem)
	}
	t.Fatalf("%s has no PE optional header", path)
	return -1
}
