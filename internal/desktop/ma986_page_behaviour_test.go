package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The page script's Delete and Remove from list, run (docs/design-archive-delete.md §3.1, §3.2,
// §8.1, slice 3b's adversarial pass and conformance review, MA-986): the served dashboard.js is
// run under Node against a small stand-in for the browser's document
// (testdata/pagescript/harness.js), so what the page does — not only what its text says — is
// checked. Node is on every CI runner, where these fail without it (the CI setting is present);
// any other computer without it skips these, saying so.

// runPageScenario runs one of the harness's scenarios on the served script and fails on each
// check that failed; it returns the checks that passed. With withEncryption, the served
// encryption.js runs after it in the same page, as the Overview loads them (MA-1070).
func runPageScenario(t *testing.T, scenario string, withEncryption ...bool) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		// On CI a missing Node would turn every page check into a silent skip (del-s3b leftover 3).
		if os.Getenv("CI") != "" {
			t.Fatal("Node is not installed, and CI must run the page script: install Node on this runner. " + err.Error())
		}
		t.Skip("Node is not installed on this computer, so the page script is not run here (CI runs it): " + err.Error())
	}
	js := filepath.Join(t.TempDir(), "dashboard.js")
	if err := os.WriteFile(js, []byte(dashboardJS), 0o600); err != nil {
		t.Fatal(err)
	}
	harness, err := filepath.Abs(filepath.Join("testdata", "pagescript", "harness.js"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{harness, js, scenario}
	if len(withEncryption) > 0 && withEncryption[0] {
		enc := filepath.Join(filepath.Dir(js), "encryption.js")
		if err := os.WriteFile(enc, []byte(encryptionJS), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, enc)
	}
	out, err := exec.Command(node, args...).CombinedOutput()
	var passed []string
	done := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case strings.HasPrefix(line, "ok "):
			passed = append(passed, strings.TrimPrefix(line, "ok "))
		case strings.HasPrefix(line, "FAIL "):
			t.Error(strings.TrimPrefix(line, "FAIL "))
		case line == "done":
			done = true
		}
	}
	if err != nil || !done {
		t.Fatalf("the %s scenario did not run to its end (%v):\n%s", scenario, err, out)
	}
	return passed
}

// covers: MA-986, R36, S41
// On CI the page script's tests fail when Node is missing, never skip (del-s3b leftover 3): this
// test binary is run again for one page test with no Node on its PATH. Positive twin first:
// without the CI setting, the same run skips, saying Node is not installed.
func TestThePageScriptTestsFailOnCIWithoutNode(t *testing.T) {
	run := func(ci bool) (string, error) {
		t.Helper()
		var env []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "PATH=") && !strings.HasPrefix(kv, "CI=") {
				env = append(env, kv)
			}
		}
		env = append(env, "PATH="+t.TempDir()) // no node here
		if ci {
			env = append(env, "CI=true")
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestOnlyTheLastDeleteClickedOpensItsPanel$", "-test.count=1", "-test.v")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run(false)
	if err != nil || !strings.Contains(out, "--- SKIP") || !strings.Contains(out, "Node is not installed on this computer") {
		t.Fatalf("the positive twin: without CI a missing Node skips (err %v):\n%s", err, out)
	}
	out, err = run(true)
	if err == nil || !strings.Contains(out, "--- FAIL") || !strings.Contains(out, "CI must run the page script") || strings.Contains(out, "--- SKIP") {
		t.Fatalf("on CI a missing Node must fail the page tests, not skip them (err %v):\n%s", err, out)
	}
}

// covers: MA-986, R36, S41
// Opening a delete panel closes any other one, whatever order the plans answer in (§3.1;
// del-s3b-A1-01): only the last Delete… or Remove from list clicked opens, an earlier one's slower
// reply is dropped, and each panel's box has its own id so its label is its own. Positive twin
// first: the last one clicked does open, with the cursor in its box.
func TestOnlyTheLastDeleteClickedOpensItsPanel(t *testing.T) {
	passed := runPageScenario(t, "race")
	if len(passed) != 6 {
		t.Errorf("the race scenario passed %d of its 6 checks: %v", len(passed), passed)
	}
}

// covers: MA-986, R36, S41
// A Delete… opened on another row while a delete waits for its reply leaves that delete's panel
// and its "Keep this page open" note alone, and the delete's reply closes only its own panel, never
// the one opened meanwhile (del-s3b leftover 1); each panel's box keeps an id of its own. Positive
// twin within: the reply still closes its own panel and shows its result, and a delete refused
// once it answered waits no more, so the next panel opened closes it.
func TestADeletesReplyClosesOnlyItsOwnPanel(t *testing.T) {
	if passed := runPageScenario(t, "twoPanels"); len(passed) != 5 {
		t.Errorf("the twoPanels scenario passed %d of its 5 checks: %v", len(passed), passed)
	}
}

// covers: MA-986, R36, S41
// A delete's reply marks every row the server says is that folder and, when the server says it
// was the page's own archive, takes Archive now and Run now off the page — but not for a delete
// refused after its start, which removed nothing (§3.1 "Where the result sits", §5.4;
// del-s3b-A1-02, A2-02, conformance gap 1). A held result drawn at a page load changes no row: the
// server drew them as they are now, and a row of the same spelling may be a new archive
// (del-s3b-A1-04). Positive twin first: the stopped delete's rows and page are marked.
func TestADeletesReplyMarksTheFoldersRowsAndOnlyAReachedPage(t *testing.T) {
	for scenario, want := range map[string]int{"alias": 5, "refusedAfterStart": 4, "heldAtLoad": 3} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}

// covers: MA-986, R36, S41
// Reloads the page starts are held while a delete asked for on the page has not answered, so its
// reply is not lost (§3.1 "After a Delete"; del-s3b-A2-03); a page drawn while a delete ran asks
// until it ends and is drawn again with its result; a result dismissed in another tab goes from
// this one too, and stops holding its reloads (A2-05); a reload held while a result was held is
// kept when the last result is closed for a new Delete… (A2-06). Positive twin first: the result
// shows when the delete answers.
func TestReloadsAreHeldUntilADeletesResultCanBeShown(t *testing.T) {
	for scenario, want := range map[string]int{"inFlight": 6, "runningRow": 2, "notHeld": 3, "heldReload": 4} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}

// covers: MA-986, R36, S41
// A record saved is remembered for the tab across a reload, so Dismiss asks "Close without saving
// the deletion record?" only of a record not saved (§8.1; conformance gap 4); a refusal that spent
// the plan's token leaves Delete permanently disabled, saying to click Delete… again, and one that
// kept it gives it back (conformance gap 3); Archive now and Run now name the archive the page was
// drawn for (MA-987). Positive twins first: a record not saved is still asked about, and a name
// that did not match gives Delete permanently back.
func TestTheResultAndThePanelKeepWhatThePersonDid(t *testing.T) {
	for scenario, want := range map[string]int{"savedMark": 3, "planUsed": 3, "drawnFor": 2} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}
