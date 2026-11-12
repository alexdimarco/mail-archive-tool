package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// covers: MA-1163, R18
// The backup's own command-line summary names a Windows archive folder in its commands as it is
// typed, in double quotes with each backslash single (the polish-161 friction walk: `Search:
// mailarchive serve -out "C:\\Users\\…"`, Go's quoted form), and a drive root keeps its closing
// quote (its trailing backslash doubled, as status's commands, MA-1158). Positive twin first: a
// POSIX folder is named as before.
func TestTheRunSummaryNamesAWindowsFolderAsItIsTyped(t *testing.T) {
	text := func(r app.Result, out, goos string) string {
		var buf bytes.Buffer
		printSummaryFor(log.New(&buf, "", 0), r, true, false, out, "Done.", goos)
		return buf.String()
	}
	served := app.Result{Stats: export.Stats{Exported: 3}, ManifestSize: 3, Indexed: 3}
	if got := text(served, "/tmp/Mail Archive", "darwin"); !strings.Contains(got, `Search:  mailarchive serve -out "/tmp/Mail Archive"`) {
		t.Fatalf("the positive twin: %s", got)
	}
	folder := `C:\Users\Public\polishlab\Archives\Polish Archive`
	typed := `-out "` + folder + `"`
	var all []string
	for _, c := range []struct {
		r    app.Result
		want string
	}{
		{served, "Search:  mailarchive serve " + typed + "   (then open the printed URL)"},
		{app.Result{ManifestSize: 80, Indexed: 40}, "`mailarchive reindex -rebuild " + typed + "`"},
		{app.Result{ManifestSize: 3, Indexed: 2, IndexErrors: 1}, "`mailarchive reindex -rebuild " + typed + "` and re-check"},
	} {
		got := text(c.r, folder, "windows")
		if !strings.Contains(got, c.want) {
			t.Errorf("the summary lacks %s:\n%s", c.want, got)
		}
		all = append(all, got)
	}
	root := text(served, `E:\`, "windows")
	if !strings.Contains(root, `mailarchive serve -out "E:\\"`) {
		t.Errorf("a drive root loses its closing quote:\n%s", root)
	}
	assure.Reached(t, !strings.Contains(strings.Join(all, "\n"), `\\Users`), "no doubled backslash in the summary's commands:\n"+strings.Join(all, "\n"))
}
