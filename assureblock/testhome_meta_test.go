// Throw-away home meta-test: every test package whose tests can reach this computer's per-user
// folders, MailArchive's settings folder among them, by any route runs in a throw-away home,
// checked by internal/testhome: its TestMain calls testhome.Run or testhome.Check, which fail the
// run when MailArchive's settings folder resolves outside the throw-away folder.
//
// The packages are found by the call graph, not by a list (the B3 adversarial pass, round 3's H4,
// MA-1333): a package whose own code computes a per-user folder (reach.ComputesUserFolder: it
// calls one of the os package's per-user folder functions, which job.Dir wraps, or reads a
// variable that names such a folder) is a root, and every test package that is a root or depends
// on one (go list -test, its Deps) must run in a throw-away home: job.Dir's callers (the restore
// lock, the restore log, the Gmail answer, the tidy-up wait, the job files) and the Thunderbird
// profile lookups alike. The check reads the Go syntax of the test files (reach.CallsGuard): a
// call to testhome.Run or testhome.Check in the body of TestMain, never a comment or a string.
package assureblock

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/testhome/reach"
)

// goFiles reads the .go files in dir: the test files when tests, else the others.
func goFiles(t *testing.T, dir string, tests bool) [][]byte {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") != tests {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

// Positive twin first: the call graph finds the roots (internal/job, which computes MailArchive's
// settings folder, among them) and the packages that reach them (internal/app and
// internal/tbrestore among them). The checks themselves, with their fail-closed twins, are
// MA-1333's internal/testhome/reach/ma1333_reach_test.go.
func TestEveryPackageReachingTheSettingsRunsInAThrowAwayHome(t *testing.T) {
	root := repoRoot(t)
	list := exec.Command("go", "list", "-f", `{{.ImportPath}}|{{.Dir}}`, "./...")
	list.Dir = root
	list.Stderr = os.Stderr
	pkgs, err := list.Output()
	if err != nil {
		t.Fatalf("MISSING: go list of the module's packages: %v", err)
	}
	roots := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(pkgs)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 {
			continue
		}
		for _, src := range goFiles(t, parts[1], false) {
			if reach.ComputesUserFolder(src) {
				roots[parts[0]] = true
				break
			}
		}
	}
	if !roots["mail-archive-tool/internal/job"] {
		t.Fatalf("UNPARSED: internal/job, which computes the settings folder, is not found as a root: %v", roots)
	}

	deps := exec.Command("go", "list", "-test", "-f", `{{if .ForTest}}{{.ForTest}}|{{.Dir}}|{{join .Deps " "}}{{end}}`, "./...")
	deps.Dir = root
	deps.Stderr = os.Stderr
	out, err := deps.Output()
	if err != nil {
		t.Fatalf("MISSING: go list of the module's test packages: %v", err)
	}
	reaching := map[string]string{} // package -> its folder
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 {
			continue
		}
		pkg, dir := parts[0], parts[1]
		if roots[pkg] {
			reaching[pkg] = dir
			continue
		}
		for _, d := range strings.Fields(parts[2]) {
			if roots[d] {
				reaching[pkg] = dir
				break
			}
		}
	}
	for _, want := range []string{"mail-archive-tool/internal/app", "mail-archive-tool/internal/tbrestore"} {
		if _, ok := reaching[want]; !ok {
			t.Fatalf("UNPARSED: %s is not found among the packages reaching a per-user folder", want)
		}
	}
	var missing []string
	for pkg, dir := range reaching {
		ok := false
		for _, src := range goFiles(t, dir, true) {
			if reach.CallsGuard(src, pkg == reach.TesthomePath) {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, pkg)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("these test packages can reach this computer's per-user folders (MailArchive's settings folder among them) without a throw-away home checked by internal/testhome (a TestMain calling testhome.Run or testhome.Check):\n  %s", strings.Join(missing, "\n  "))
	}
}
