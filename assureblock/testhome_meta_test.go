// Throw-away home meta-test: every test package whose tests can reach a lookup in this computer's
// MailArchive settings folder (the backup engine, internal/app, which looks up the tidy-up wait;
// and internal/gmailanswer, which keeps the Gmail answer and the wait, itself included) runs in a
// throw-away home, checked by internal/testhome: its TestMain calls testhome.Run or
// testhome.Check, which fail the run when MailArchive's settings folder resolves outside the
// throw-away folder. A new package that reaches the lookup without it is named here (the lead's
// follow-up to the B3 adversarial pass, round 2: tests never read the owner's real settings).
package assureblock

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// settingsLookups are the packages whose code looks up this computer's settings folder for the
// Gmail answer and the tidy-up wait.
var settingsLookups = []string{"mail-archive-tool/internal/app", "mail-archive-tool/internal/gmailanswer"}

func TestEveryPackageReachingTheSettingsRunsInAThrowAwayHome(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-test", "-f", `{{if .ForTest}}{{.ForTest}}|{{.Dir}}|{{join .Deps " "}}{{end}}`, "./...")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("MISSING: go list of the module's test packages: %v", err)
	}
	reaching := map[string]string{} // package -> its folder
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 {
			continue
		}
		pkg, dir, deps := parts[0], parts[1], " "+parts[2]+" "
		for _, l := range settingsLookups {
			if pkg == l || strings.Contains(deps, " "+l+" ") {
				reaching[pkg] = dir
			}
		}
	}
	if len(reaching) == 0 {
		t.Fatal("UNPARSED: no test package reaches the settings lookups; go list's shape is the contract")
	}
	var missing []string
	for pkg, dir := range reaching {
		if !usesTestHome(t, dir) {
			missing = append(missing, pkg)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("these test packages can reach this computer's MailArchive settings folder without a throw-away home checked by internal/testhome (a TestMain calling testhome.Run or testhome.Check):\n  %s", strings.Join(missing, "\n  "))
	}
}

// usesTestHome reports whether a test file in dir calls testhome.Run or testhome.Check.
func usesTestHome(t *testing.T, dir string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if s := string(b); strings.Contains(s, "testhome.Run(") || strings.Contains(s, "testhome.Check(") {
			return true
		}
	}
	return false
}
