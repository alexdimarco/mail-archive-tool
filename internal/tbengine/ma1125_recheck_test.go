package tbengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// profilesINI writes a profiles.ini in base listing dirs, in order (absolute paths).
func profilesINI(t *testing.T, base string, names []string, dirs []string) {
	t.Helper()
	s := ""
	for i, d := range dirs {
		s += "[Profile" + itoa(i) + "]\nName=" + names[i] + "\nIsRelative=0\nPath=" + d + "\n\n"
	}
	write(t, filepath.Join(base, tbrestore.ProfilesININame), s)
}

// covers: MA-1125, R40, S66, S67
// At Start, under the archive lock and the restore lock and before Thunderbird's lock,
// the plan is checked again (design §5.3 as folded). Positive twin first: an unchanged
// plan restores. Then each difference refuses with nothing written — the profile, its
// Local Folders and restores.log byte for byte as they were: Thunderbird started (W-RUNNING),
// Local Folders swapped for a link, the chosen name taken, the archive changed by a
// backup, the profile list rewritten under the position the page sent (W-PROFCHANGED),
// the free space gone, and a Microsoft Store Thunderbird appearing (W-CHANGED, naming what).
func TestStartRefusesWhatChangedSinceCheck(t *testing.T) {
	out := plainArchive(t, true)
	{
		pr := newProfile(t, "")
		p := plan(t, Request{Out: out, Profile: pr.choice()})
		assure.Reached(t, mustRun(t, p, Start{}).Done(), fmt.Sprintf("positive twin: an unchanged plan restores"))
	}
	type tcase struct {
		name   string
		change func(t *testing.T, pr profile) (undo func())
		code   string
		words  string
	}
	cases := []tcase{
		{"Thunderbird started", func(t *testing.T, pr profile) func() {
			return tbrestore.SetProcessProbeForTest(func() (int, bool) { return 1, true })
		}, "W-RUNNING", "Thunderbird is open"},
		{"the name taken", func(t *testing.T, pr profile) func() {
			write(t, filepath.Join(pr.local, "Restored — Work mail"), "")
			return func() {}
		}, "W-CHANGED", "This changed since you clicked Check: the name 'Restored — Work mail' is now taken"},
		{"the space gone", func(t *testing.T, pr profile) func() {
			was := freeBytes
			freeBytes = func(string) (uint64, error) { return 1 << 20, nil }
			return func() { freeBytes = was }
		}, "W-CHANGED", "no longer enough free space"},
		{"a Store Thunderbird appeared", func(t *testing.T, pr profile) func() {
			was := storeWarning
			storeWarning = func() string { return "If you use Thunderbird from the Microsoft Store…" }
			return func() { storeWarning = was }
		}, "W-CHANGED", "Microsoft Store"},
		{"Local Folders swapped for a link", func(t *testing.T, pr profile) func() {
			other := filepath.Join(pr.dir, "Mail", "Elsewhere")
			if err := os.MkdirAll(other, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(pr.local, pr.local+"-was"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, pr.local); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return func() {}
		}, "W-CHANGED", "Local Folders"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := newProfile(t, "")
			p := plan(t, Request{Out: out, Profile: pr.choice()})
			undo := c.change(t, pr)
			defer undo()
			before := snapshot(t, pr.dir)
			_, err := Run(context.Background(), p, Start{})
			refusedAs(t, err, c.code, func() bool { return snapshot(t, pr.dir) == before }, c.words)
		})
	}

	t.Run("the archive changed", func(t *testing.T) {
		own := plainArchive(t, true)
		pr := newProfile(t, "")
		p := plan(t, Request{Out: own, Profile: pr.choice()})
		src := sourceDir(t)
		write(t, filepath.Join(src, "Later"), mboxOf("delta one"))
		backup(t, src, own, true, nil)
		before := snapshot(t, pr.dir)
		_, err := Run(context.Background(), p, Start{})
		refusedAs(t, err, "W-CHANGED", func() bool { return snapshot(t, pr.dir) == before }, "the archive changed")
	})

	t.Run("the profile list rewritten", func(t *testing.T) {
		base := t.TempDir()
		a, b := newProfile(t, ""), newProfile(t, "")
		profilesINI(t, base, []string{"Main", "Other"}, []string{a.dir, b.dir})
		list, err := tbrestore.ReadProfiles([]string{base})
		if err != nil {
			t.Fatal(err)
		}
		p := plan(t, Request{Out: out, Profile: ProfileChoice{ListID: list.ID, Position: 0, Bases: []string{base}}})
		assure.Reached(t, p.Profile.Dir == a.dir, fmt.Sprintf("the position names Main"))
		profilesINI(t, base, []string{"Main", "Other"}, []string{b.dir, a.dir})
		before := snapshot(t, a.dir, b.dir)
		_, err = Run(context.Background(), p, Start{})
		refusedAs(t, err, "W-PROFCHANGED", func() bool { return snapshot(t, a.dir, b.dir) == before }, "The list of Thunderbird profiles changed")
	})
}
