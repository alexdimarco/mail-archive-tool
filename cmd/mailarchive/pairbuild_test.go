package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// MA-614 — the one-program pair build and packaging, verified as STRUCTURE (the
// catalog's "build and packaging structure, parsed"). The actual MSI build and the
// install/uninstall are the windows-msi-lab's gate (§12 SG3), not this test: this
// pins that the scripts, the WiX source and the RMM helpers describe the pair
// (mailarchive.exe + mailarchivew.exe) and the two former-name copies of
// mailarchivew.exe, the tag-bound ProductVersion, the shortcuts and the stop/detect
// rules the design requires (SC23, SC20, SC34, SC35, SC37, SC11, SC7).

// mkVars reads a Makefile's `VAR := value` definitions and expands $(VAR) in s.
func mkVars(mk, s string) string {
	vars := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^(\w+)\s*:=\s*(.*)$`).FindAllStringSubmatch(mk, -1) {
		vars[m[1]] = strings.TrimSpace(m[2])
	}
	for i := 0; i < 10; i++ { // a few passes resolve vars that reference vars
		changed := false
		for k, v := range vars {
			n := strings.ReplaceAll(s, "$("+k+")", v)
			if n != s {
				s, changed = n, true
			}
		}
		if !changed {
			break
		}
	}
	return s
}

// buildWindowsRecipe returns the body of the Makefile's build-windows target.
func buildWindowsRecipe(t *testing.T, mk string) string {
	t.Helper()
	lines := strings.Split(mk, "\n")
	var body []string
	in := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "build-windows:") {
			in = true
			continue
		}
		if in {
			if ln == "" || (!strings.HasPrefix(ln, "\t") && !strings.HasPrefix(ln, " ")) {
				break
			}
			body = append(body, ln)
		}
	}
	return assure.Reached(t, strings.Join(body, "\n"), "Makefile build-windows recipe")
}

// covers: MA-614, R33, S59
// Prove-fail (§9.4 idiom): point util:CloseApplication at mailarchivew.exe, or drop
// a former-name copy from build-release.sh, or restore Build-MSI.ps1's 1.2.0
// default, and the matching case below reddens.
func TestMA614_PairBuildAndPackaging(t *testing.T) {
	root := moduleRoot(t)
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return assure.Reached(t, string(b), rel)
	}
	pkg := "packaging/mailarchive-desktop/"

	// --- SC23: the Windows pair is two GOOS=windows builds of ./cmd/mailarchive ----
	mk := read("Makefile")
	bw := mkVars(mk, buildWindowsRecipe(t, mk))
	winBuilds := regexp.MustCompile(`(?m)^\s*GOOS=windows .*\bgo build\b.*\./cmd/mailarchive\b.*$`).FindAllString(bw, -1)
	if len(winBuilds) != 2 {
		t.Errorf("Makefile build-windows: want exactly 2 GOOS=windows go build ./cmd/mailarchive lines, got %d:\n%s", len(winBuilds), bw)
	}
	gui, con := 0, 0
	for _, b := range winBuilds {
		if strings.Contains(b, "-H=windowsgui") && strings.Contains(b, "mailarchivew.exe") {
			gui++
		} else if !strings.Contains(b, "-H=windowsgui") && regexp.MustCompile(`mailarchive\.exe`).MatchString(b) {
			con++
		}
	}
	if gui != 1 || con != 1 {
		t.Errorf("Makefile build-windows: want one console (mailarchive.exe) and one -H=windowsgui (mailarchivew.exe) line; got console=%d gui=%d", con, gui)
	}

	// build-release.sh: the same pair of ./cmd/mailarchive, the two former-name
	// copies of mailarchivew.exe, PRODUCT-VERSION.txt, the stamp check, and the zip.
	br := read("packaging/build-release.sh")
	rbWin := regexp.MustCompile(`(?m)^\s*GOOS=windows .*\bgo build\b.*\$ONE\b.*$`).FindAllString(br, -1)
	if len(rbWin) != 2 {
		t.Errorf("build-release.sh: want exactly 2 GOOS=windows go build $ONE lines, got %d", len(rbWin))
	}
	for _, want := range []string{
		`cp "$DIST/mailarchivew.exe" "$PAY/mailarchive-desktop.exe"`,
		`cp "$DIST/mailarchivew.exe" "$PAY/mailarchive-gui.exe"`,
		`PRODUCT-VERSION.txt`,
		`vcs.revision=`, `vcs.modified=false`, // SC7/SC11 stamp check (SC33 "(devel)" is an open condition, not enforced)
		`mailarchive-windows-amd64.zip`,
	} {
		if !strings.Contains(br, want) {
			t.Errorf("build-release.sh is missing %q (MA-614)", want)
		}
	}
	// The payload carries the pair AND the two former-name copies (SC23/§7.1).
	for _, name := range []string{"mailarchive.exe", "mailarchivew.exe", "mailarchive-desktop.exe", "mailarchive-gui.exe"} {
		if !strings.Contains(br, `"$PAY/`+name+`"`) {
			t.Errorf("build-release.sh payload is missing %q", name)
		}
	}
	// The Windows zip members are the install names, flat (SC11).
	zipLine := regexp.MustCompile(`(?m)^.*mailarchive-windows-amd64\.zip.*mailarchive\.exe mailarchivew\.exe .READ ME\.txt.*$`)
	if !zipLine.MatchString(br) {
		t.Error("build-release.sh: the Windows zip must contain exactly mailarchive.exe, mailarchivew.exe and READ ME.txt as flat members (SC11)")
	}

	// --- Product.wxs (parsed): the File set, the shortcuts, CloseApplication -------
	wxs := read(pkg + "Product.wxs")
	files, shortcuts, closeTargets, wizardSeen := parseWxs(t, wxs)
	for _, want := range []string{"mailarchive.exe", "mailarchivew.exe", "mailarchive-desktop.exe", "mailarchive-gui.exe"} {
		if !files[want] {
			t.Errorf("Product.wxs: no File component sources %q (want the pair + two former-name copies)", want)
		}
	}
	// Shortcuts target mailarchivew.exe (the no-console build), never a former name.
	for id, sc := range assure.Reached(t, shortcuts, "Product.wxs shortcuts") {
		if sc.target == "" || strings.Contains(sc.target, "msiexec") {
			continue // the Uninstall shortcut targets msiexec
		}
		if !strings.HasSuffix(sc.target, "mailarchivew.exe") {
			t.Errorf("Product.wxs shortcut %s targets %q, want …mailarchivew.exe (SC20)", id, sc.target)
		}
	}
	// The three desktop arguments and the wizard shortcut (SC20).
	gotArgs := map[string]bool{}
	for _, sc := range shortcuts {
		if strings.HasSuffix(sc.target, "mailarchivew.exe") {
			gotArgs[sc.args] = true
		}
	}
	for _, want := range []string{"desktop", "desktop -no-browser", "desktop -copy-link", "wizard"} {
		if !gotArgs[want] {
			t.Errorf("Product.wxs: no shortcut runs mailarchivew.exe with arguments %q (SC20)", want)
		}
	}
	if !wizardSeen {
		t.Error("Product.wxs: the MailArchive wizard shortcut (Name \"MailArchive wizard\") is missing (SC20)")
	}
	// util:CloseApplication targets ONLY the former name mailarchive-desktop.exe,
	// never mailarchivew.exe (SC34).
	if len(closeTargets) == 0 {
		t.Error("Product.wxs: no util:CloseApplication found")
	}
	for _, tgt := range closeTargets {
		if tgt != "mailarchive-desktop.exe" {
			t.Errorf("util:CloseApplication targets %q, want only mailarchive-desktop.exe (SC34)", tgt)
		}
	}

	// --- Build-MSI.ps1: the four exes, the tag-bound version (SC23, SC37) ----------
	msi := read(pkg + "Build-MSI.ps1")
	for _, name := range []string{"mailarchive.exe", "mailarchivew.exe", "mailarchive-desktop.exe", "mailarchive-gui.exe"} {
		if !regexp.MustCompile(`\$exes\s*=\s*@\([^)]*'` + regexp.QuoteMeta(name) + `'`).MatchString(msi) {
			t.Errorf("Build-MSI.ps1 $exes does not name %q (SC23)", name)
		}
	}
	if regexp.MustCompile(`\$Version\s*=\s*'1\.2\.0'`).MatchString(msi) {
		t.Error("Build-MSI.ps1 still has the 1.2.0 -Version default; the version must come from the payload (SC37)")
	}
	for _, want := range []string{"PRODUCT-VERSION.txt", "differs from the payload"} {
		if !strings.Contains(msi, want) {
			t.Errorf("Build-MSI.ps1 is missing %q (version bound to the tag, refuses a differing -Version; SC37)", want)
		}
	}

	// --- Install-RMM: former-name unconditional + mailarchivew dashboard-only ------
	inst := read(pkg + "Install-RMM.ps1")
	if !regexp.MustCompile(`Get-Process mailarchive-desktop\b`).MatchString(inst) {
		t.Error("Install-RMM.ps1: the unconditional former-name (mailarchive-desktop) stop is missing (SC34)")
	}
	for _, want := range []string{"mailarchivew.exe", "CommandLine", "-capture", "desktop"} {
		if !strings.Contains(inst, want) {
			t.Errorf("Install-RMM.ps1 is missing %q (the dashboard-only mailarchivew stop; SC34)", want)
		}
	}
	if strings.Contains(inst, "Get-Process mailarchive-gui") || regexp.MustCompile(`Name = 'mailarchive-gui`).MatchString(inst) {
		t.Error("Install-RMM.ps1 must NOT stop mailarchive-gui (SC34)")
	}

	// --- Uninstall-RMM + Cleanup: GUI-subsystem + former names; stop before remove -
	for _, rel := range []string{pkg + "Uninstall-RMM.ps1", pkg + "Cleanup-CurrentUser.ps1"} {
		s := read(rel)
		if !regexp.MustCompile(`Get-Process[^\n]*\bmailarchivew\b`).MatchString(s) ||
			!strings.Contains(s, "mailarchive-desktop") || !strings.Contains(s, "mailarchive-gui") {
			t.Errorf("%s: the stop list must cover mailarchivew + the former names mailarchive-desktop and mailarchive-gui (SC34)", rel)
		}
	}
	clean := read(pkg + "Cleanup-CurrentUser.ps1")
	if i, j := strings.Index(clean, "Stop-Process"), strings.Index(clean, "Remove-Item"); i < 0 || j < 0 || i > j {
		t.Error("Cleanup-CurrentUser.ps1: the Stop-Process must precede the Remove-Item (SC34)")
	}

	// --- Detect-RMM: the pair, no former name (SC35) -------------------------------
	det := read(pkg + "Detect-RMM.ps1")
	code := stripComments(det)
	for _, want := range []string{"mailarchive.exe", "mailarchivew.exe"} {
		if !strings.Contains(code, want) {
			t.Errorf("Detect-RMM.ps1 must require %q present (SC35)", want)
		}
	}
	if regexp.MustCompile(`mailarchive-(gui|desktop)`).MatchString(code) {
		t.Error("Detect-RMM.ps1 code must name no former name (SC35)")
	}

	// --- build-app.sh: the symlink and -y (SC11) -----------------------------------
	app := read("packaging/macos/build-app.sh")
	if !regexp.MustCompile(`ln -s(f)? mailarchive\b[^\n]*mailarchive-gui`).MatchString(app) {
		t.Error("build-app.sh: Contents/MacOS/mailarchive-gui must be a symlink to mailarchive (SC11)")
	}
	if !regexp.MustCompile(`zip\b[^\n]*-y`).MatchString(app) {
		t.Error("build-app.sh: the app zip must use -y to store symlinks (SC11)")
	}
}

type wxsShortcut struct{ target, args string }

// parseWxs returns the File @Source basenames, the shortcuts by id, the
// util:CloseApplication targets, and whether a "MailArchive wizard" shortcut exists.
func parseWxs(t *testing.T, src string) (files map[string]bool, shortcuts map[string]wxsShortcut, closeTargets []string, wizardSeen bool) {
	t.Helper()
	files = map[string]bool{}
	shortcuts = map[string]wxsShortcut{}
	dec := xml.NewDecoder(strings.NewReader(src))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		attr := func(name string) string {
			for _, a := range se.Attr {
				if a.Name.Local == name {
					return a.Value
				}
			}
			return ""
		}
		switch se.Name.Local {
		case "File":
			if src := attr("Source"); src != "" {
				files[filepath.Base(strings.ReplaceAll(src, `\`, "/"))] = true
			}
		case "Shortcut":
			id := attr("Id")
			shortcuts[id] = wxsShortcut{target: filepath.Base(strings.ReplaceAll(attr("Target"), `\`, "/")), args: attr("Arguments")}
			if attr("Name") == "MailArchive wizard" && attr("Arguments") == "wizard" {
				wizardSeen = true
			}
		case "CloseApplication":
			if tgt := attr("Target"); tgt != "" {
				closeTargets = append(closeTargets, tgt)
			}
		}
	}
	assure.Reached(t, files, "Product.wxs File sources")
	return files, shortcuts, closeTargets, wizardSeen
}

// stripComments removes PowerShell `#` line comments so a former-name mention in a
// comment does not fail a code-only check.
func stripComments(ps string) string {
	var b strings.Builder
	for _, ln := range strings.Split(ps, "\n") {
		if i := strings.Index(ln, "#"); i >= 0 {
			ln = ln[:i]
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}
