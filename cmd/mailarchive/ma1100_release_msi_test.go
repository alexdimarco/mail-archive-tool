package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// MA-1100..MA-1102: the unsigned Windows installer ships with every release (owner
// ruling 2026-11-02), verified as structure, the way MA-614 and MA-827 read the
// packaging scripts and release.yml as text. The workflows themselves run on GitHub,
// not here.

// msiAssetPrefix and msiAssetSuffix frame the installer's published name,
// MailArchiveDesktop-<version>-x64.msi: Build-MSI.ps1 writes it, release.yml publishes
// it, and the release notes and README tell the owner to download it.
const (
	msiAssetPrefix = "MailArchiveDesktop-"
	msiAssetSuffix = "-x64.msi"
)

// publishIf is release.yml's publish condition, exactly: it runs only on a tag push
// and only when every job it needs succeeded.
const publishIf = "success() && github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')"

// topLevelBlock returns the lines under a top-level key (up to the next line that is
// not indented), joined, without blank lines or comments.
func topLevelBlock(yml, key string) string {
	var out []string
	in := false
	for _, ln := range strings.Split(yml, "\n") {
		if !in {
			in = strings.TrimRight(ln, " ") == key+":"
			continue
		}
		s := strings.TrimSpace(ln)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if !strings.HasPrefix(ln, " ") {
			break
		}
		out = append(out, strings.TrimRight(ln, " "))
	}
	return strings.Join(out, "\n")
}

// readRepo reads a file under the module root, failing on an empty one.
func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return assure.Reached(t, string(b), rel)
}

// workflowJobs splits a workflow's text into its jobs, by the two-space-indented
// keys under the top-level "jobs:" line.
func workflowJobs(t *testing.T, yml string) map[string]string {
	t.Helper()
	jobs := map[string]string{}
	in, cur := false, ""
	var body []string
	flush := func() {
		if cur != "" {
			jobs[cur] = strings.Join(body, "\n")
		}
	}
	keyRe := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	for _, ln := range strings.Split(yml, "\n") {
		if !in {
			in = strings.TrimRight(ln, " ") == "jobs:"
			continue
		}
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "#") {
			break // the next top-level key
		}
		if m := keyRe.FindStringSubmatch(ln); m != nil {
			flush()
			cur, body = m[1], nil
			continue
		}
		body = append(body, ln)
	}
	flush()
	return assure.Reached(t, jobs, "workflow jobs")
}

// jobSteps splits a job's text into its steps (each "      - " item under steps:).
func jobSteps(job string) []string {
	var steps []string
	var cur []string
	for _, ln := range strings.Split(job, "\n") {
		if strings.HasPrefix(ln, "      - ") {
			if cur != nil {
				steps = append(steps, strings.Join(cur, "\n"))
			}
			cur = []string{ln}
			continue
		}
		if cur != nil {
			cur = append(cur, ln)
		}
	}
	if cur != nil {
		steps = append(steps, strings.Join(cur, "\n"))
	}
	return steps
}

// jobKey returns the value of a job-level key (four-space indent), or "".
func jobKey(job, key string) string {
	m := regexp.MustCompile(`(?m)^    ` + regexp.QuoteMeta(key) + `:\s*(.*)$`).FindStringSubmatch(job)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// inOrder reports whether each needle occurs in s, each after the one before.
func inOrder(s string, needles ...string) bool {
	at := 0
	for _, n := range needles {
		i := strings.Index(s[at:], n)
		if i < 0 {
			return false
		}
		at += i + len(n)
	}
	return true
}

// covers: MA-1100
// release.yml publishes only when the installer was built, installed and uninstalled:
// three jobs, build (Linux) -> msi (Windows) -> publish; publish needs both, has no
// always()/failure()/cancelled() escape and runs only on a tag push; the msi job hands
// over the installer only after its install and uninstall; publish adds the installer's
// line to SHA256SUMS, checks every file against SHA256SUMS, and only then makes the
// draft; and nothing but publish can create a release.
//
// Prove-fail: drop msi from publish's needs, or move the `sha256sum "$msi" >> SHA256SUMS`
// line after `gh release create`, or upload the msi artifact before the uninstall, and
// the matching case reddens.
func TestMA1100_ReleasePublishesOnlyWithTheInstaller(t *testing.T) {
	yml := readRepo(t, ".github/workflows/release.yml")
	jobs := workflowJobs(t, yml)
	build, msi, publish := jobs["build"], jobs["msi"], jobs["publish"]
	if build == "" || msi == "" || publish == "" {
		t.Fatalf("release.yml must have the jobs build, msi and publish; has %d jobs", len(jobs))
	}

	// The jobs' order and where they run.
	if jobKey(build, "runs-on") != "ubuntu-latest" || jobKey(msi, "runs-on") != "windows-latest" || jobKey(publish, "runs-on") != "ubuntu-latest" {
		t.Error("release.yml: build and publish must run on ubuntu-latest, msi on windows-latest")
	}
	if jobKey(msi, "needs") != "build" {
		t.Errorf("release.yml: msi must need build, has needs %q", jobKey(msi, "needs"))
	}
	needs := jobKey(publish, "needs")
	for _, j := range []string{"build", "msi"} {
		if !regexp.MustCompile(`\b` + j + `\b`).MatchString(needs) {
			t.Errorf("release.yml: publish must need %s (if it fails, nothing is published); has needs %q", j, needs)
		}
	}
	cond := jobKey(publish, "if")
	for _, escape := range []string{"always()", "failure()", "cancelled()"} {
		if strings.Contains(cond, escape) {
			t.Errorf("release.yml: publish's if %q runs even when a job it needs did not succeed", cond)
		}
	}
	// Exactly this condition: an added "|| …" (a hand run, say) is not a tag push.
	if cond != publishIf {
		t.Errorf("release.yml: publish must run only on a tag push (a dry run never publishes): its if is %q, want exactly %q", cond, publishIf)
	}

	// Who may write: the workflow's default is read-only, and only publish (which holds
	// no test or third-party code) gets contents: write. The build job runs the test
	// suite and the msi job runs WiX; neither may write to the repository.
	if got := topLevelBlock(yml, "permissions"); got != "  contents: read" {
		t.Errorf("release.yml: the top-level permissions must be exactly contents: read; got %q", got)
	}
	writes := regexp.MustCompile(`(?m)^\s*(permissions:.*write.*|[a-z-]+:\s*write)\s*$`).FindAllStringIndex(yml, -1)
	pubAt := strings.Index(yml, "\n  publish:\n")
	if len(writes) != 1 || pubAt < 0 || writes[0][0] < pubAt {
		t.Errorf("release.yml: exactly one write permission, contents: write in publish, is allowed; found %d (publish at %d)", len(writes), pubAt)
	}
	if !strings.Contains(publish, "\n    permissions:\n      contents: write\n") {
		t.Error("release.yml: publish's permissions must be exactly contents: write")
	}
	for name, job := range map[string]string{"build": build, "msi": msi} {
		if jobKey(job, "permissions") != "" || strings.Contains(job, "\n    permissions:") {
			t.Errorf("release.yml: job %s sets its own permissions; it keeps the read-only default", name)
		}
	}

	// Every artifact comes from this run: a download-artifact step that names another
	// run (run-id), another token or another repository could hand publish a swapped file.
	for name, job := range jobs {
		for _, st := range jobSteps(job) {
			if !strings.Contains(st, "actions/download-artifact") {
				continue
			}
			for _, bad := range []string{"run-id", "github-token", "repository:"} {
				if strings.Contains(st, bad) {
					t.Errorf("release.yml: job %s fetches an artifact with %s (not this run's):\n%s", name, bad, st)
				}
			}
		}
	}
	// No step is switched off, anywhere.
	if m := regexp.MustCompile(`(?m)^\s*if:\s*(\$\{\{\s*)?(false|0|'')(\s*\}\})?\s*$`).FindString(yml); m != "" {
		t.Errorf("release.yml: a step or job is switched off (%q)", strings.TrimSpace(m))
	}

	// The exact-tag refusal runs on every tag push, before anything is built, and
	// refuses v0.0.0 (the dry run's placeholder, whose programs build-release.sh does not
	// check against the tag).
	var tagStep string
	for _, st := range jobSteps(build) {
		if strings.Contains(st, "name: Refuse a tag that is not an exact release") {
			tagStep = st
		}
	}
	if tagStep == "" {
		t.Fatal("release.yml: build has no \"Refuse a tag that is not an exact release\" step")
	}
	if !strings.Contains(tagStep, "\n        if: github.event_name == 'push'\n") {
		t.Errorf("release.yml: the exact-tag step must run on every tag push (if: github.event_name == 'push'):\n%s", tagStep)
	}
	for _, want := range []string{
		`[[ "${GITHUB_REF_NAME}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {`,
		`[ "${GITHUB_REF_NAME}" != "v0.0.0" ] || {`,
	} {
		if !strings.Contains(tagStep, want) {
			t.Errorf("release.yml: the exact-tag step lacks %q", want)
		}
	}
	if !inOrder(build, "name: Refuse a tag that is not an exact release", "name: Build release artifacts") {
		t.Error("release.yml: the exact-tag step must come before the build")
	}
	for name, job := range map[string]string{"build": build, "msi": msi} {
		if strings.Contains(job, "gh release") {
			t.Errorf("release.yml: job %s touches a release; only publish may", name)
		}
	}
	if strings.Contains(publish, "continue-on-error") {
		t.Error("release.yml: no publish step may continue on error")
	}
	// A step of msi may continue on error only when it says it is informational.
	for _, st := range jobSteps(msi) {
		if strings.Contains(st, "continue-on-error") && !strings.Contains(st, "informational") {
			t.Errorf("release.yml: an msi step that builds, installs or uninstalls continues on error:\n%s", st)
		}
	}

	// The msi job: WiX through Get-WiX.ps1, then Build-MSI.ps1, the install, the
	// uninstall, and only then the hand-over of the installer.
	if !inOrder(msi, `Get-WiX.ps1 -Destination`, `.\Build-MSI.ps1 -PayloadDir`, "msiexec -ArgumentList \"/i", "msiexec -ArgumentList \"/x", "name: msi\n") {
		t.Error("release.yml: the msi job must stage WiX (Get-WiX.ps1), build (Build-MSI.ps1), install (/i), uninstall (/x), and only then upload the installer (artifact msi)")
	}
	if !strings.Contains(msi, msiAssetPrefix+"$($env:RELEASE_TAG -replace '^v','')"+msiAssetSuffix) {
		t.Errorf("release.yml: the msi job does not look for %s<version>%s", msiAssetPrefix, msiAssetSuffix)
	}

	// The publish job: the installer joins dist and SHA256SUMS before the check and
	// before the draft; the existing draft -> download -> compare -> publish follows.
	if !strings.Contains(publish, `msi="`+msiAssetPrefix+`${RELEASE_TAG#v}`+msiAssetSuffix+`"`) {
		t.Errorf("release.yml: publish does not name the installer %s<version>%s", msiAssetPrefix, msiAssetSuffix)
	}
	if !inOrder(publish,
		"name: msi",
		`[ "$(ls msi)" = "$msi" ] || { echo "the msi job did not hand over exactly $msi:"; ls -la msi; exit 1; }`,
		`[ ! -e "dist/$msi" ] || { echo "dist already holds $msi"; exit 1; }`,
		`mv "msi/$msi" "dist/$msi"`,
		`(cd dist && sha256sum "$msi" >> SHA256SUMS)`,
		`(cd dist && sha256sum -c SHA256SUMS)`,
		`diff <(sed -E 's/^[0-9a-f]{64} [ *]//' dist/SHA256SUMS | sort) <(ls dist | grep -vx SHA256SUMS | sort)`,
		`gh release create "${GITHUB_REF_NAME}" dist/* --draft`,
		`gh release download`,
		`cmp "$check/SHA256SUMS" dist/SHA256SUMS`,
		`(cd "$check" && sha256sum -c SHA256SUMS)`,
		`gh release edit "${GITHUB_REF_NAME}" --draft=false`,
	) {
		t.Error("release.yml: publish must fetch the installer, check it is exactly one file not already in dist, add it and its SHA256SUMS line, check every file and that SHA256SUMS names exactly the files, then draft, download again, compare and only then publish")
	}

	// Build-MSI.ps1 writes the name publish expects.
	if !strings.Contains(readRepo(t, "packaging/mailarchive-desktop/Build-MSI.ps1"), `"`+msiAssetPrefix+`$Version`+msiAssetSuffix+`"`) {
		t.Errorf("Build-MSI.ps1 no longer writes %s$Version%s", msiAssetPrefix, msiAssetSuffix)
	}

	// A dry run (workflow_dispatch) never publishes: the publish job's push-only if,
	// checked above, is what holds it back.
	if strings.Contains(yml, "workflow_dispatch") && !strings.Contains(cond, "github.event_name == 'push'") {
		t.Error("release.yml: a hand-run release could publish")
	}
}

// covers: MA-1101
// The WiX download is used only after it matches the SHA-256 pinned in the repo:
// Get-WiX.ps1 refuses a pin that is not 64 hex digits before it downloads anything,
// compares the download's SHA-256 with the pin before it expands it, and is the one
// place WiX is downloaded (Build-MSI.ps1, release.yml and windows-msi-lab.yml all call
// it, before Build-MSI.ps1 runs). The pin's pattern accepts exactly 64 hex digits, so
// the placeholder the pin file ships with fails closed.
//
// Prove-fail: move Expand-Archive above the hash comparison, or put an
// Invoke-WebRequest back into Build-MSI.ps1 or a workflow, or loosen the pin pattern to
// [0-9A-F]+, and the matching case reddens.
func TestMA1101_WiXDownloadIsCheckedAgainstThePin(t *testing.T) {
	pkg := "packaging/mailarchive-desktop/"
	get := stripComments(readRepo(t, pkg+"Get-WiX.ps1"))

	// The pin is read from the file beside the script, and its form checked first.
	if !strings.Contains(get, "'wix314-binaries.sha256'") {
		t.Error("Get-WiX.ps1 does not read the pin from wix314-binaries.sha256")
	}
	m := regexp.MustCompile(`\$pinned -notmatch '([^']+)'`).FindStringSubmatch(get)
	if m == nil {
		t.Fatal("Get-WiX.ps1 does not refuse a pin that is not a SHA-256 ($pinned -notmatch '…')")
	}
	// PowerShell's -notmatch ignores letter case.
	pinRe := regexp.MustCompile("(?i)" + m[1])
	for _, c := range []struct {
		pin  string
		good bool
	}{
		{strings.Repeat("ab", 32), true},
		{strings.Repeat("AB", 32), true},
		{strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false},
		{strings.Repeat("g", 64), false},
		{"", false},
		{strings.Repeat("a", 64) + " wix314-binaries.zip", false},
	} {
		if pinRe.MatchString(c.pin) != c.good {
			t.Errorf("Get-WiX.ps1's pin pattern %q: %q accepted=%v, want %v", m[1], c.pin, !c.good, c.good)
		}
	}
	// The two refusals are these exact lines, each followed by its throw: a condition
	// with something added ("-and $false") would keep the text and drop the check. The
	// Windows half of this row (ma1101_getwix_windows_test.go) runs the script itself.
	lines := strings.Split(get, "\n")
	for _, want := range []string{
		`if ($pinned -notmatch '^[0-9A-F]{64}$') {`,
		`if ($actual -ne $pinned) {`,
	} {
		found := false
		for i, ln := range lines {
			if strings.TrimSpace(ln) == want && i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "throw ") {
				found = true
			}
		}
		if !found {
			t.Errorf("Get-WiX.ps1 lacks the exact line %q followed by its throw", want)
		}
	}
	// -ZipPath is held to the same pin: the hash is taken of $zip whichever way it came.
	if !strings.Contains(get, "Get-FileHash -Path $zip -Algorithm SHA256") || !strings.Contains(get, "Expand-Archive -LiteralPath $zip ") {
		t.Error("Get-WiX.ps1 must hash and expand the same file, $zip")
	}

	// The order inside Get-WiX.ps1: refuse a bad pin, download, compare, then expand.
	if !inOrder(get,
		"$pinned -notmatch", "throw",
		"Invoke-WebRequest",
		"Get-FileHash", "-ne $pinned", "throw",
		"Expand-Archive",
	) {
		t.Error("Get-WiX.ps1 must refuse a bad pin before downloading, and compare the download with the pin (and throw) before expanding it")
	}
	if strings.Count(get, "Expand-Archive") != 1 || strings.Count(get, "Invoke-WebRequest") != 1 {
		t.Error("Get-WiX.ps1 must download once and expand once")
	}

	// Get-WiX.ps1 is the one WiX download.
	for _, rel := range []string{pkg + "Build-MSI.ps1", ".github/workflows/release.yml", ".github/workflows/windows-msi-lab.yml"} {
		s := readRepo(t, rel)
		if strings.Contains(s, "Invoke-WebRequest") || strings.Contains(s, "wix314-binaries.zip") {
			t.Errorf("%s fetches WiX itself; it must go through Get-WiX.ps1", rel)
		}
		if !strings.Contains(s, "Get-WiX.ps1") {
			t.Errorf("%s does not use Get-WiX.ps1", rel)
		}
	}
	for _, rel := range []string{".github/workflows/release.yml", ".github/workflows/windows-msi-lab.yml"} {
		if !inOrder(readRepo(t, rel), `Get-WiX.ps1 -Destination`, `Build-MSI.ps1`) {
			t.Errorf("%s must stage WiX with Get-WiX.ps1 before Build-MSI.ps1 runs", rel)
		}
	}
	if !inOrder(readRepo(t, pkg+"Build-MSI.ps1"), "Get-Command candle.exe", "'Get-WiX.ps1') -Destination", "& $candle") {
		t.Error("Build-MSI.ps1 must fall back to Get-WiX.ps1 when WiX is not installed, before it runs candle")
	}
}

// covers: MA-1103
// A release can be made only once wix314-binaries.sha256 holds a checked SHA-256 of
// wix314-binaries.zip, and the file says who checked it, when, and against what (a
// second source, not the download alone). Until a person fills it in, every WiX download
// is refused, so a tag push's msi job fails and NO release is published (no zip, no Mac
// app, nothing), and windows-msi-lab.yml is red.
//
// OPEN GAP ENCODER: while the pin is the placeholder this test SKIPS, naming the block,
// so an unfiltered run shows it. Once the pin is filled in, the skip no longer fires and
// the checks below hold; then REMOVE the placeholder branch so it is a permanent guard.
func TestMA1103_TheWiXPinIsFilledInAndSaysWhoCheckedIt(t *testing.T) {
	pinFile := readRepo(t, "packaging/mailarchive-desktop/wix314-binaries.sha256")
	var pinLine string
	for _, ln := range strings.Split(pinFile, "\n") {
		if s := strings.TrimSpace(ln); s != "" && !strings.HasPrefix(s, "#") {
			pinLine = s
			break
		}
	}
	assure.Reached(t, pinLine, "the pin line of wix314-binaries.sha256")
	if strings.HasPrefix(pinLine, "PLACEHOLDER-") {
		t.Skipf("OPEN (MA-1103): wix314-binaries.sha256 is still the placeholder %q; the release is BLOCKED (a tag push publishes nothing) until a person fills in the checked SHA-256 and who checked it, when, and against what", pinLine)
	}
	if !regexp.MustCompile(`^[0-9A-Fa-f]{64}$`).MatchString(pinLine) {
		t.Errorf("wix314-binaries.sha256's pin line %q is not 64 hexadecimal digits", pinLine)
	}
	for _, label := range []string{"Checked by:", "Checked on:", "Checked against:"} {
		m := regexp.MustCompile(`(?m)^#\s*` + regexp.QuoteMeta(label) + `[ \t]*(\S.*)$`).FindStringSubmatch(pinFile)
		if m == nil {
			t.Errorf("wix314-binaries.sha256 lacks a filled-in %q comment line", label)
		}
	}
}

// covers: MA-1102
// The owner's words name the installer first on Windows and keep the zip: the release
// notes' Windows section offers MailArchiveDesktop-<version>-x64.msi before the zip,
// says what the unsigned warnings look like and what to click, says how a zip user
// moves the scheduled backups (Repair on the Scheduled backup card), and the notes
// have a "New in" line for it; the README's download table lists the installer row
// before the zip row; the uninstall note covers both an installed and an unzipped copy.
//
// Prove-fail: put the zip bullet back above the installer in the release notes, or
// drop "Unknown publisher", and the matching case reddens.
func TestMA1102_InstallWordsNameTheInstaller(t *testing.T) {
	asset := msiAssetPrefix + "<version>" + msiAssetSuffix
	notes := readRepo(t, "packaging/release-notes.md")
	start := strings.Index(notes, "### Windows — start here")
	end := strings.Index(notes, "### Linux")
	if start < 0 || end < start {
		t.Fatal("release-notes.md: no \"### Windows — start here\" section before \"### Linux\"")
	}
	win := strings.Join(strings.Fields(notes[start:end]), " ")
	for _, want := range []string{
		asset, "Windows protected your PC", "More info", "Run anyway", "Unknown publisher",
		"administrator", "Start menu", "desktop", "sign in",
		"mailarchive-windows-amd64.zip", "Shut down", "Repair", "Scheduled backup", "SHA256SUMS",
		// The adversarial pass's wording findings (2026-11-02): it starts for every
		// account (L2); update after Shut down, letting a backup finish (M3); remove the
		// schedules with the Desktop's own Uninstall before Settings → Apps (L3); the
		// browser may warn at download (L4).
		"each time anyone signs in to this computer",
		"**Updating to a newer version.** First click **Shut down**", "let it finish",
		"**Danger zone → Uninstall**", "removes your scheduled backups", "Settings → Apps",
		"choose **Keep**",
	} {
		if !strings.Contains(win, want) {
			t.Errorf("release-notes.md's Windows section lacks %q", want)
		}
	}
	if !inOrder(win, asset, "mailarchive-windows-amd64.zip") {
		t.Error("release-notes.md: the installer must come before the zip")
	}
	if !regexp.MustCompile(`(?m)^### New in [0-9.]+ — a Windows installer$`).MatchString(notes) {
		t.Error("release-notes.md: no \"### New in <version> — a Windows installer\" heading")
	}

	readme := readRepo(t, "README.md")
	i, j := strings.Index(readme, "| **Windows** | **`"+asset+"`**"), strings.Index(readme, "`mailarchive-windows-amd64.zip` | unzip")
	if i < 0 || j < 0 || i > j {
		t.Error("README.md: the download table must list the installer's Windows row before the zip's")
	}
	for _, want := range []string{
		"| **Windows (installed)** | First click **Shut down** in MailArchive Desktop (if a backup is running, let it finish first).",
		"each time anyone\nsigns in to this computer",
		"use **Danger zone → Uninstall** in MailArchive\nDesktop first (it removes the scheduled backups)",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md lacks %q", want)
		}
	}
}
