package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/job"
)

// covers: MA-322, R25, S47
// The wizard's directory-link opt-in (owner ruling OR6, following X4 and X5): the answer
// defaults to No, so a click-through (or a cancelled dialog) follows no link; only the
// explicit "Follow folder links" opts in, and only the FOLDER inputs. The question is
// asked only where a link can live (a folder store), never for a .pst, a single mbox
// file or the Outlook-app path. The opt-in travels into the scheduled job file, which
// reads back as it was written, and a job written before the field existed reads as no
// opt-in. (The dialog itself is lab-tier; the decisions are these functions.)
func TestWizardFollowLinksOptInDefaultsOffAndTravelsInTheJob(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "tb")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	pst := filepath.Join(dir, "a.pst")
	if err := os.WriteFile(pst, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := followLinksFor(followLinksNo, []string{store}); got != nil {
		t.Errorf("the default answer opted %v in; a click-through must follow no link", got)
	}
	if got := followLinksFor("", []string{store}); got != nil {
		t.Errorf("an empty answer (a closed dialog) opted %v in", got)
	}
	if got := followLinksFor(followLinksYes, []string{store, pst}); !reflect.DeepEqual(got, []string{store}) {
		t.Errorf("Yes opted %v in; want only the folder input %q", got, store)
	}

	for _, c := range []struct {
		src    string
		inputs []string
		want   bool
	}{
		{srcThunderbird, []string{store}, true},
		{srcEvolution, []string{store}, true},
		{srcAuto, []string{store, pst}, true},
		{srcAuto, []string{pst}, false},
		{srcOutlook, []string{pst}, false},
		{srcMbox, []string{pst}, false},
		{srcOutlookCOM, nil, false},
	} {
		if got := followLinksWorthAsking(c.src, c.inputs); got != c.want {
			t.Errorf("followLinksWorthAsking(%q, %v) = %v, want %v", c.src, c.inputs, got, c.want)
		}
	}

	j := jobFor("backup", wizardChoice{inputs: []string{store}, out: filepath.Join(dir, "out"), followLinks: []string{store}})
	if !reflect.DeepEqual(j.FollowLinks, []string{store}) {
		t.Fatalf("the job file carries FollowLinks %v; want the opted input", j.FollowLinks)
	}
	path := filepath.Join(dir, "job.json")
	if err := job.Write(path, j); err != nil {
		t.Fatal(err)
	}
	back, err := job.Read(path)
	if err != nil || !reflect.DeepEqual(back.FollowLinks, []string{store}) {
		t.Errorf("the job read back as %+v (err %v); want follow_links kept", back, err)
	}
	old := filepath.Join(dir, "old.json")
	if err := os.WriteFile(old, []byte(`{"version":1,"name":"x","out":"/o","mode":"incremental","inputs":["/i"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if o, err := job.Read(old); err != nil || len(o.FollowLinks) != 0 {
		t.Errorf("a job written before the field = %+v (err %v); want no opt-in", o, err)
	}
}
