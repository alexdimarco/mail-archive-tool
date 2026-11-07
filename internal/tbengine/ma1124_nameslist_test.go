package tbengine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-1124, MA-1088, R40, S70
// The plan lists the names it changed (the lead's call of 2026-11-07, after ADV-N1): for
// a plain archive, the NAMES line says how many, then one "old name → new name" per line,
// each quoted, the top folder first when its NAME changed; up to 50, then "and N more.".
// The plan's text — what the terminal's -plan prints and what -record-out and Desktop's
// Save this record keep — carries the same lines. For an encrypted archive the line is
// the count only: no folder's name, old or new, is in the plan. Positive twin first: an
// archive whose names need no change has no NAMES line.
func TestThePlanListsTheChangedNames(t *testing.T) {
	pr := newProfile(t, "")
	twin := plan(t, Request{Out: plainArchive(t, true), Profile: pr.choice()})
	assure.Reached(t, lineOf(twin, "NAMES") == "", fmt.Sprintf("positive twin: no changed names, no NAMES line: %q", lineOf(twin, "NAMES")))

	src := filepath.Join(t.TempDir(), "Mail")
	for i, n := range []string{"a%41b", "Budget%2026", "100%", "semi;colon", "Plain"} {
		write(t, filepath.Join(src, n), mboxOf(fmt.Sprintf("pct %d", i)))
	}
	write(t, filepath.Join(src, "Plain.sbd", "x#y"), mboxOf("nested one"))
	out := filepath.Join(t.TempDir(), "Mail%20backup")
	backup(t, src, out, true, nil)
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	got := strings.Split(lineOf(p, "NAMES"), "\n")
	want := []string{
		`"Restored — Mail%20backup" → "Restored — Mail_20backup"`,
		`"a%41b" → "a_41b"`, `"Budget%2026" → "Budget_2026"`, `"100%" → "100_"`,
		`"semi;colon" → "semi_colon"`, `"Plain/x#y" → "Plain/x_y"`,
	}
	assure.Reached(t, got[0] == "6 folder names had to be changed to be safe as file names; Thunderbird shows the changed names:", fmt.Sprintf("the NAMES head: %q", got[0]))
	assure.Reached(t, len(got) == 1+len(want) && got[1] == want[0], fmt.Sprintf("the top first, then one line per name: %q", got))
	for _, w := range want {
		found := false
		for _, g := range got[1:] {
			found = found || g == w
		}
		assure.Reached(t, found, fmt.Sprintf("the plan lists %s: %q", w, got))
	}
	for _, w := range want {
		assure.Reached(t, strings.Contains(p.Text(), "\n"+w+"\n") || strings.HasSuffix(p.Text(), "\n"+w), fmt.Sprintf("the plan's text (the terminal's and the record's) lists %s", w))
	}

	// The top's new name is the free one the plan chose, " (n)" and all.
	write(t, filepath.Join(pr.local, "Restored — Mail_20backup"), "")
	p2 := plan(t, Request{Out: out, Profile: pr.choice()})
	got2 := strings.Split(lineOf(p2, "NAMES"), "\n")
	assure.Reached(t, len(got2) > 1 && got2[1] == `"Restored — Mail%20backup" → "Restored — Mail_20backup (2)"`, fmt.Sprintf("the top's new name is the one chosen: %q", got2))

	// Over 50: the first 50, then "and N more.".
	many := filepath.Join(t.TempDir(), "Mail")
	for i := 0; i < 60; i++ {
		write(t, filepath.Join(many, fmt.Sprintf("n;%02d", i)), mboxOf(fmt.Sprintf("many %d", i)))
	}
	mout := filepath.Join(t.TempDir(), "Many")
	backup(t, many, mout, true, nil)
	mp := plan(t, Request{Out: mout, Profile: pr.choice()})
	ml := strings.Split(lineOf(mp, "NAMES"), "\n")
	assure.Reached(t, len(ml) == 52 && strings.HasPrefix(ml[0], "60 folder names") && ml[51] == "and 10 more.", fmt.Sprintf("60 changed: the head, 50 names, and 10 more: %d lines, last %q", len(ml), ml[len(ml)-1]))
	for _, l := range ml[1:51] {
		assure.Reached(t, strings.HasPrefix(l, `"n;`) && strings.Contains(l, `" → "n_`), fmt.Sprintf("a listed name: %q", l))
	}

	// An encrypted archive: the count only, and no folder's name anywhere in the plan.
	eout := filepath.Join(t.TempDir(), "Secret mail")
	pc, err := keyring.Create(eout, testPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := pc.Commit()
	if err != nil {
		t.Fatal(err)
	}
	backup(t, src, eout, true, w)
	w.Destroy()
	u, err := keyring.Open(eout, keyring.Password(testPassword), keyring.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	ep := plan(t, Request{Out: eout, Keys: u, Profile: pr.choice()})
	assure.Reached(t, lineOf(ep, "NAMES") == "5 folder names had to be changed to be safe as file names; Thunderbird shows the changed names.", fmt.Sprintf("encrypted: the count only: %q", lineOf(ep, "NAMES")))
	for _, n := range []string{"a%41b", "a_41b", "Budget%2026", "Budget_2026", "100%", "100_", "semi;colon", "semi_colon", "x#y", "x_y", "→"} {
		assure.Reached(t, !strings.Contains(ep.Text(), n), fmt.Sprintf("an encrypted archive's plan names no folder: %q found", n))
	}
}
