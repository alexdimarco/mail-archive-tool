package archdelete

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1021, R36
// A page of rows asks Remove from list's refusals through RowsCheck.UnlistRefusal, which answers
// as the route's UnlistRefusal does, in the same case and words, for each refusal it has: the
// archive in use, a Desktop started with -out naming the folder, and another archive's capture
// registered under the folder's name. Positive twins first: another archive, and the archive in
// use once its folder is gone (§5.2), are not refused by either.
func TestARowsUnlistRefusalIsTheRoutes(t *testing.T) {
	cur := plainRowArchive(t, "Current")
	other := plainRowArchive(t, "Other")
	launched := plainRowArchive(t, "Launched")
	under := plainRowArchive(t, "Under")
	gone := filepath.Join(t.TempDir(), "Gone")
	deps := testDeps()
	deps.Settings = Settings{Current: cur, LaunchOut: launched, CaptureBackupFor: under}
	rc := NewRowsCheck(deps)

	if err := rc.UnlistRefusal(other); err != nil || UnlistRefusal(other, deps) != nil {
		t.Fatalf("the positive twin: another archive: row %v, route %v", err, UnlistRefusal(other, deps))
	}
	goneDeps := testDeps()
	goneDeps.Settings = Settings{Current: gone}
	if err := NewRowsCheck(goneDeps).UnlistRefusal(gone); err != nil || UnlistRefusal(gone, goneDeps) != nil {
		t.Fatalf("the positive twin: the archive in use, its folder gone: row %v", err)
	}

	for _, c := range []struct {
		row  string
		want Case
		says string
	}{
		{cur, CaseCurrentArchive, "This is the archive in use."},
		{cur + string(filepath.Separator), CaseCurrentArchive, "This is the archive in use."},
		{launched, CaseLaunchOut, "Restart MailArchive without -out, then remove it from the list."},
		{under, CaseBackupFor, "is still registered under this folder's name"},
	} {
		r := AsRefusal(rc.UnlistRefusal(c.row))
		route := AsRefusal(UnlistRefusal(c.row, deps))
		if r == nil || route == nil || r.Case != c.want || route.Case != c.want || r.Message != route.Message {
			t.Fatalf("the row %s: row %v, route %v; want both the %s refusal in the same words", c.row, r, route, c.want)
		}
		assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(c.says),
			assure.NoSideEffect(func() bool { return exists(filepath.Join(c.row, manifestName)) }))
	}
}

// covers: MA-1021, R36
// RowsCheck.UnlistRefusal asks the file system once per page for each folder it compares (the 1.5.3
// friction fixes' adversarial pass, M1; del-s3a-A3-04): with the archive in use, the folder
// MailArchive was started with and the capture's folder all on a network share that is offline,
// a page of four rows asks each of them once, not once per row. Positive twin first: a single
// row asks each of them.
func TestARowsUnlistRefusalAsksEachFolderOncePerPage(t *testing.T) {
	offline := filepath.Join(t.TempDir(), "offline-share")
	var rows []string
	for i := 0; i < 4; i++ {
		rows = append(rows, plainRowArchive(t, "Row"+string(rune('A'+i))))
	}
	deps := testDeps()
	deps.Settings = Settings{Current: filepath.Join(offline, "Cur"), LaunchOut: filepath.Join(offline, "Launch"), CaptureBackupFor: filepath.Join(offline, "Under")}

	asked := countStats(t, offline)
	if err := NewRowsCheck(deps).UnlistRefusal(rows[0]); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	for _, p := range []string{deps.Settings.Current, deps.Settings.LaunchOut, deps.Settings.CaptureBackupFor} {
		if asked[p] == 0 {
			t.Fatalf("the positive twin: %s was never compared with the row", p)
		}
	}
	for p := range asked {
		delete(asked, p)
	}

	rc := NewRowsCheck(deps)
	for _, r := range rows {
		if err := rc.UnlistRefusal(r); err != nil {
			t.Fatalf("the row %s: %v", r, err)
		}
	}
	if len(asked) == 0 {
		t.Fatal("the page compared no folder on the offline share")
	}
	for p, n := range asked {
		if n != 1 {
			t.Errorf("%s was asked %d times for one page of %d rows; want once", p, n, len(rows))
		}
	}
}

// covers: MA-1021, R36
// RowsCheck.UnlistRefusal tests the archive in use with the row's kept manifest parse (the 1.5.3
// friction fixes' adversarial pass, M1; del-s3a-A3-03): a manifest unchanged since the last page
// is not read whole again. Kept by its identity, size and modification time: a manifest that
// changed is read again, and one that no longer parses is no longer an archive, which Remove from
// list may then take off the list (§5.2). Positive twin first: the archive in use is refused.
// (The parse is shown kept by bytes that do not parse written in place at the same size, the
// modification time put back, as MA-980 shows the row's Delete keeping it.)
func TestARowsUnlistRefusalKeepsTheManifestsParse(t *testing.T) {
	dir := plainRowArchive(t, "InUse")
	man := filepath.Join(dir, manifestName)
	deps := testDeps()
	deps.Settings = Settings{Current: dir}
	r := AsRefusal(NewRowsCheck(deps).UnlistRefusal(dir))
	if r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("the positive twin: the archive in use: %v", r)
	}
	fi, err := os.Stat(man)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = ' ' // the closing brace gone: the same size, no longer parses
	f, err := os.OpenFile(man, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chtimes(man, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	r = AsRefusal(NewRowsCheck(deps).UnlistRefusal(dir))
	if r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("the unchanged manifest (identity, size, time) was read again: %v", r)
	}
	assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names("This is the archive in use."),
		assure.NoSideEffect(func() bool { return exists(man) }))
	later := fi.ModTime().Add(time.Minute)
	if err := os.Chtimes(man, later, later); err != nil {
		t.Fatal(err)
	}
	if err := NewRowsCheck(deps).UnlistRefusal(dir); err != nil {
		t.Errorf("a changed manifest that does not parse is no longer an archive, as the route says (%v): %v", UnlistRefusal(dir, deps), err)
	}
}
