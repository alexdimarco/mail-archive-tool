package archdelete

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/delmark"
)

// covers: MA-1049, R36
// What a status line says (UnfinishedToSay; the slice-2 adversarial pass's LOW-3): when the
// marker's lookup fails at a folder that is there, the delete this computer's deletion log knows is
// still said (the log's line, not marker-only, its Marker Unknown), where UnfinishedAt gives only
// the lookup's error; when the log has no line for the folder, or no log is asked, the lookup's
// error stays (the line says nothing; the actions refuse "could not check"). Positive twin first:
// with the lookup working, UnfinishedToSay is UnfinishedAt (the log's line; the marker's).
func TestMA1049_AFailedLookupDoesNotHideTheLogsDelete(t *testing.T) {
	half, other, _ := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)
	ma1045Started(t, logPath, other, nil)

	// Positive twin.
	if u, err := UnfinishedToSay(logPath, other, ""); err != nil || u == nil || u.MarkerOnly || u.Marker != delmark.Absent {
		t.Fatalf("positive twin: the log's line: %+v %v", u, err)
	}
	if u, err := UnfinishedToSay(logPath, half, ""); err != nil || u == nil || !u.MarkerOnly || u.Marker != delmark.Regular {
		t.Fatalf("positive twin: the marker's delete: %+v %v", u, err)
	}

	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("input/output error")}
	})
	defer restore()
	var le *delmark.LookupError
	if u, err := UnfinishedAt(logPath, other, ""); u != nil || !errors.As(err, &le) {
		t.Fatalf("precondition: UnfinishedAt gives the lookup's error alone: %+v %v", u, err)
	}
	u, err := UnfinishedToSay(logPath, other, "")
	if err != nil || u == nil || u.MarkerOnly || u.Marker != delmark.Unknown || u.Run != "s1045-Other" {
		t.Fatalf("the log's line through a failed lookup: %+v %v", u, err)
	}
	for name, lp := range map[string]string{"no line for the folder": logPath, "no log asked": ""} {
		if u, err := UnfinishedToSay(lp, half, ""); u != nil || !errors.As(err, &le) {
			t.Errorf("%s: want the lookup's error alone: %+v %v", name, u, err)
		}
	}
}
