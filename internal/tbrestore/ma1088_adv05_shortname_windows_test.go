//go:build windows

package tbrestore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// covers: MA-1088, R40, S69
// ADV-05 on Windows (the VM, NTFS with 8.3 names on): an archive with sibling folders
// "Long folder name one" and "LONGFO~1" restores both — the second under its changed
// name — where before the plan said OK and Start failed at the second create. The temp
// volume's own short name for a long name is read first, so the test knows the alias
// is real here.
func TestRestoreShortNameSiblingRestoresOnWindows(t *testing.T) {
	probe := filepath.Join(t.TempDir(), "Long folder name one")
	writeFile(t, probe, "x")
	buf := make([]uint16, 300)
	p16, _ := windows.UTF16PtrFromString(probe)
	n, err := windows.GetShortPathName(p16, &buf[0], uint32(len(buf)))
	short := windows.UTF16ToString(buf[:n])
	if err != nil || !strings.HasSuffix(short, `\LONGFO~1`) {
		t.Skipf("8.3 names are off on this volume (%q, %v): the alias cannot arise here", short, err)
	}
	in := []FolderIn{{Names: []string{"Long folder name one"}, Messages: 1}, {Names: []string{"LONGFO~1"}, Messages: 1}}
	msgs := [][][]byte{{[]byte("Subject: a\n\nx\n")}, {[]byte("Subject: b\n\ny\n")}}
	fx := newFixture(t, in, msgs)
	res, _ := fx.write(context.Background(), nil)
	if res.Outcome != OutDone || res.Restored != 2 || fx.layout.Changed != 1 {
		t.Fatalf("both folders restored: %s %v (%d changed) %q", res.Outcome, res.Err, fx.layout.Changed, res.Words)
	}
	if !fileExists(filepath.Join(fx.lf.Dir, fx.top+".sbd", fx.layout.Folders[1].Parts[0])) {
		t.Fatalf("the renamed folder is there: %q", fx.layout.Folders[1].Parts)
	}
}
