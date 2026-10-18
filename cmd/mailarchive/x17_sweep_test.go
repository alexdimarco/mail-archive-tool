package main

import (
	"encoding/xml"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// X17 / MA-622 — one program, one name in every swept text. A structural sweep
// of an ENUMERATED set (§9.2): the README, the release notes, the five
// administrator texts, README-macOS, the Product.wxs shortcut names and
// descriptions (parsed), and the string literals of the owner surfaces (not Go
// comments). The matcher is `mailarchive-(gui|desktop)` as a program or asset
// token; the only exemptions are X17's two excluded non-program tokens (the
// build directory packaging/mailarchive-desktop/ and the MSI payload asset name)
// and the phrase rule: a former name may appear only on a line that also says
// "former name", and never as the thing to run, double-click, download or build.
// There is no other skip, allowlist or suppression. All-or-nothing: green only
// when every swept text is clean (SC47, SC28, SC21, SC39).

var formerNameRe = regexp.MustCompile(`mailarchive-(gui|desktop)`)

// X17's two excluded non-program tokens (§9.2), part of the definition the
// operator accepted at SE0 (OP-6): the build directory path and the one asset
// whose name keeps a former name (the MSI payload, §4.10 unless it is renamed).
const (
	x17DirToken     = "packaging/mailarchive-desktop/"
	x17PayloadAsset = "mailarchive-desktop-windows-msi-payload.zip"
	x17PhraseExempt = "former name"
)

// sweptDocs are the text files swept line by line (§6, §9.2). A path that does
// not exist is a fault in the enumeration, not a pass.
var sweptDocs = []string{
	"README.md",
	"packaging/release-notes.md",
	"packaging/mailarchive-desktop/README-RMM.md",
	"packaging/mailarchive-desktop/DEPLOYMENT.txt",
	"packaging/mailarchive-desktop/README-payload.txt",
	"packaging/mailarchive-desktop/BUNDLE-CONTENTS.txt",
	"packaging/mailarchive-desktop/SOURCE-NOTICE.txt",
	"packaging/macos/README-macOS.txt",
}

// sweptWxs is parsed as XML; only Shortcut Name and Description are swept (the
// File and CloseApplication elements are release-N machine detail, pinned by
// MA-614 and MA-376, outside the sweep).
const sweptWxs = "packaging/mailarchive-desktop/Product.wxs"

// sweptGo are the owner-surface Go files whose STRING LITERALS are swept (not
// their comments). This is X17's "string literals of the owner surfaces": the
// -h usage and the console pointer (cmd/mailarchive), the wizard's texts and
// refusals (internal/wizard), the dashboard templates including backupOff and
// the launcher's owner-facing lines (internal/desktop), and the health remedies
// (internal/health). cmd/mailarchive/dispatch.go is NOT here: its former-name
// string literals are the §4.1 rule-6 detection (MA-611), the matcher's own
// target names, not owner text — the completeness guard below scopes around it.
var sweptGo = []string{
	"cmd/mailarchive/main.go",
	"cmd/mailarchive/noarg.go",
	"internal/wizard/wizard.go",
	"internal/desktop/dashboard.go",
	"internal/desktop/localarchive.go",
	"internal/desktop/launch.go",
	"internal/health/health.go",
	"internal/health/gaps.go",
	"internal/health/shownassource.go",
}

// guardPkgs are the owner-facing packages the completeness guard scans for a
// former-name string literal in a file the enumeration missed, so a new owner
// surface cannot slip in unswept. These packages hold no former-name detection
// literal (that lives only in cmd/mailarchive/dispatch.go), so every former-name
// literal found here must be in sweptGo.
var guardPkgs = []string{"internal/wizard", "internal/desktop", "internal/health"}

// x17StripExcluded removes X17's two excluded non-program tokens so their
// embedded "mailarchive-desktop" is not read as a program token.
func x17StripExcluded(s string) string {
	s = strings.ReplaceAll(s, x17PayloadAsset, "")
	s = strings.ReplaceAll(s, x17DirToken, "")
	return s
}

// formerNameViolation reports a former-name program/asset token on a line that
// the phrase rule does not exempt, after the two excluded tokens are removed.
func formerNameViolation(line string) bool {
	if !formerNameRe.MatchString(x17StripExcluded(line)) {
		return false
	}
	return !strings.Contains(line, x17PhraseExempt)
}

// surfacePhrases are the surface-dependent phrases no owner text may use (SC21),
// plus the page name no SE1/SE2 text may name (SC39). Matched case-insensitively;
// "open mail archive" is a hit only when it is not "open mail archive.app".
var surfacePhrases = []string{
	"the window that opens with this program",
	"re-open this program",
	"add mail from this computer",
}

func forbiddenPhrases(line string) []string {
	low := strings.ToLower(line)
	var hits []string
	for _, p := range surfacePhrases {
		if strings.Contains(low, p) {
			hits = append(hits, p)
		}
	}
	const om = "open mail archive"
	for i := 0; ; {
		j := strings.Index(low[i:], om)
		if j < 0 {
			break
		}
		at := i + j
		if !strings.HasPrefix(low[at+len(om):], ".app") {
			hits = append(hits, om)
		}
		i = at + len(om)
	}
	return hits
}

// scanLines applies both rules to a labelled set of text lines.
func scanLines(label string, lines []string) (findings []string) {
	for n, line := range lines {
		if formerNameViolation(line) {
			findings = append(findings, label+":"+strconv.Itoa(n+1)+": former name as a program/asset token — "+strings.TrimSpace(line))
		}
		for _, p := range forbiddenPhrases(line) {
			findings = append(findings, label+":"+strconv.Itoa(n+1)+": surface-dependent phrase "+strconv.Quote(p)+" — "+strings.TrimSpace(line))
		}
	}
	return findings
}

// goLiteralLines returns, for one Go file, the lines of every string literal
// (comments excluded, because only *ast.BasicLit values are read), each labelled
// with the file and the literal's start line.
func goLiteralLines(t *testing.T, path, rel string) (labelled [][2]string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			return true
		}
		start := fset.Position(lit.Pos()).Line
		for _, line := range strings.Split(v, "\n") {
			labelled = append(labelled, [2]string{rel + ":" + strconv.Itoa(start), line})
		}
		return true
	})
	return labelled
}

// covers: MA-622, S59, X17
// The enumerated former-name sweep over the texts SE1 ships: every doc line,
// every swept Product.wxs shortcut name/description, and every owner-surface Go
// string literal is clean of a former-name program/asset token (phrase rule and
// the two excluded tokens aside); no owner text uses a surface-dependent phrase
// or names "Add mail from this computer"; the add-mail help names the wizard;
// and a completeness guard proves no owner-facing package hides a former-name
// literal in a file the enumeration missed. Fail-closed twins prove the matcher,
// the exclusions and the phrase rule each fire the right way.
//
// Prove-fail (§9.4 idiom): restoring any former name as the thing to run,
// download or build on a line that does not say "former name" reddens this test.
func TestMA622_X17EnumeratedSweep(t *testing.T) {
	root := moduleRoot(t)
	var findings []string

	// Docs, line by line.
	for _, rel := range assure.Reached(t, sweptDocs, "swept docs") {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("swept doc missing (enumeration is wrong): %s: %v", rel, err)
		}
		findings = append(findings, scanLines(rel, strings.Split(string(b), "\n"))...)
	}

	// Product.wxs: the Shortcut Name and Description attributes only.
	findings = append(findings, sweepWxsShortcuts(t, filepath.Join(root, sweptWxs))...)

	// Owner-surface Go string literals, line by line within each literal.
	scanned := 0
	for _, rel := range assure.Reached(t, sweptGo, "swept Go surfaces") {
		for _, ll := range goLiteralLines(t, filepath.Join(root, rel), rel) {
			scanned++
			for _, f := range scanLines(ll[0], []string{ll[1]}) {
				// ll[0] already carries file:litline; scanLines re-appends :1, trim it.
				findings = append(findings, strings.Replace(f, ll[0]+":1:", ll[0]+":", 1))
			}
		}
	}
	assure.Reached(t, scanned, "owner-surface string-literal lines scanned")

	// Completeness guard: no owner-facing package hides a former-name literal in
	// a file the enumeration missed.
	findings = append(findings, guardStrayOwnerLiterals(t, root)...)

	for _, f := range findings {
		t.Errorf("X17 sweep: %s", f)
	}

	// SC39 positive: the add-mail help names the wizard by its one-program verb.
	assertAddMailNamesWizard(t, root)

	// Fail-closed twins: the matcher, the two exclusions and the phrase rule.
	for _, tw := range []struct {
		name string
		line string
		want bool
	}{
		{"download of a former name", "Download mailarchive-gui-windows-amd64.exe and run it.", true},
		{"double-click a former name", "Double-click mailarchive-desktop.exe to open the dashboard.", true},
		{"former-name line with the exemption", "mailarchive-desktop is a former name kept as a copy this release.", false},
		{"the excluded build directory", "The texts live in packaging/mailarchive-desktop/README-RMM.md.", false},
		{"the excluded payload asset", "The MSI payload is mailarchive-desktop-windows-msi-payload.zip.", false},
		{"a clean one-program line", "Run mailarchive wizard, or double-click mailarchivew.exe.", false},
	} {
		if got := formerNameViolation(tw.line); got != tw.want {
			t.Errorf("fail-closed twin %q: formerNameViolation=%v, want %v (line %q)", tw.name, got, tw.want, tw.line)
		}
	}
	if got := forbiddenPhrases("Re-open this program any time to check the backup."); len(got) == 0 {
		t.Error("fail-closed twin: the surface-dependent phrase was not flagged")
	}
	if got := forbiddenPhrases("Unzip and open Mail Archive.app, then drag it to Applications."); len(got) != 0 {
		t.Errorf("fail-closed twin: 'open Mail Archive.app' must not be flagged, got %v", got)
	}
}

// sweepWxsShortcuts parses Product.wxs and applies the matcher to every Shortcut
// Name and Description attribute value (the File and CloseApplication elements
// are outside the sweep, §9.2).
func sweepWxsShortcuts(t *testing.T, path string) (findings []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("swept Product.wxs missing (enumeration is wrong): %v", err)
	}
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Shortcut" {
			continue
		}
		for _, a := range se.Attr {
			if a.Name.Local != "Name" && a.Name.Local != "Description" {
				continue
			}
			if formerNameViolation(a.Value) {
				findings = append(findings, sweptWxs+": Shortcut @"+a.Name.Local+" names a former name — "+a.Value)
			}
		}
	}
	return findings
}

// guardStrayOwnerLiterals scans the owner-facing packages for a former-name
// string literal in a file not in sweptGo, so a newly-added owner surface cannot
// escape the enumerated sweep.
func guardStrayOwnerLiterals(t *testing.T, root string) (findings []string) {
	t.Helper()
	enumerated := map[string]bool{}
	for _, rel := range sweptGo {
		enumerated[filepath.ToSlash(rel)] = true
	}
	for _, pkg := range guardPkgs {
		_ = filepath.WalkDir(filepath.Join(root, pkg), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if enumerated[rel] {
				return nil
			}
			for _, ll := range goLiteralLines(t, path, rel) {
				if formerNameViolation(ll[1]) {
					findings = append(findings, "unswept owner surface "+ll[0]+": "+strings.TrimSpace(ll[1])+" (add it to sweptGo or confirm it is mechanism, not a surface)")
				}
			}
			return nil
		})
	}
	return findings
}

// assertAddMailNamesWizard proves the add-mail help (addMailHelp, in
// internal/desktop/localarchive.go) names the MailArchive wizard by its
// one-program verb (SC39) — the positive twin to the former-name sweep.
func assertAddMailNamesWizard(t *testing.T, root string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "internal/desktop/localarchive.go"))
	if err != nil {
		t.Fatalf("reading localarchive.go: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "mailarchive wizard") && !strings.Contains(src, "MailArchive wizard") {
		t.Error("the add-mail help must name the MailArchive wizard (mailarchive wizard) (SC39)")
	}
}
