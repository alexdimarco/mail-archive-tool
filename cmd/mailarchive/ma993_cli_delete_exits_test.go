package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-993, R36, R12
// delete-archive and unlist-archive exit by one declared set, named in their help and in
// docs/ux-contract.md X1 beside decrypt's: 0 done, 3 not fully deleted, 1 refused, 130/143
// stopped by a signal — a prompt so stopped changes nothing; the delete so stopped says it did
// not finish and how to finish it (design-archive-delete §3.3, §5.4).
func TestTheDeleteVerbsExitByTheirDeclaredSet(t *testing.T) {
	// Positive twin: each verb's help names its flags and the set, exit 0.
	for verb, want := range map[string][]string{
		"delete-archive": {"-out", "-plan-out", "-record-out", "Exit: 0 deleted (or the plan saved); 3 not fully deleted", "1 refused", "130 or 143"},
		"unlist-archive": {"-out", "Exit: 0 removed from the list; 3 stopped part way", "1 refused", "130 or 143"},
	} {
		code, stderr := runCLI(verb, "-h")
		if code != 0 {
			t.Fatalf("%s -h: exit %d\n%s", verb, code, stderr)
		}
		for _, w := range want {
			if !strings.Contains(stderr, w) {
				t.Fatalf("%s -h does not name %q:\n%s", verb, w, stderr)
			}
		}
	}
	// The root usage and the "-out is required" hint name both verbs.
	usage := captureExportUsage(t)
	for _, w := range []string{"  mailarchive delete-archive -out DIR", "  mailarchive unlist-archive -out DIR"} {
		if !strings.Contains(usage, w) {
			t.Fatalf("the root usage lacks %q", w)
		}
	}
	if err := runExport(nil); err == nil || !strings.Contains(err.Error(), "delete-archive, unlist-archive") {
		t.Fatalf("the -out hint does not name the verbs: %v", err)
	}
	// X1 declares the set beside decrypt's.
	ux, err := os.ReadFile(filepath.Join("..", "..", "docs", "ux-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	var x1 string
	for _, line := range strings.Split(string(ux), "\n") {
		if strings.HasPrefix(line, "| X1 |") {
			x1 = line
		}
	}
	for _, w := range []string{"**`delete-archive`** and **`unlist-archive`**", "`0` deleted, removed from the list, or a plan saved", "`3` not fully deleted", "`1` refused", "`130` / `143`"} {
		if !strings.Contains(x1, w) {
			t.Fatalf("X1 does not declare %q:\n%s", w, x1)
		}
	}
	if strings.Index(x1, "**`decrypt`**") > strings.Index(x1, "**`delete-archive`**") {
		t.Fatal("X1 does not declare the delete verbs beside (after) decrypt's set")
	}

	// A prompt stopped by Ctrl-C: 130, nothing changed.
	e := newCLIDel(t)
	dir := e.plainArchive("Archive M")
	before := tree(t, dir)
	var errb bytes.Buffer
	a := &scripted{isAttended: true, interrupt: "Type the folder's name"}
	err = interrupted(deleteArchive(a, dir, "", "", io.Discard), &errb)
	assure.Refused(t, exitCode(err), errb.String(), assure.Code(130), assure.Names("interrupted; nothing was changed"),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) && e.listed(dir) && e.logText() == "" }))
	err = interrupted(unlistArchive(&scripted{isAttended: true, interrupt: "Remove from list?"}, dir, io.Discard), io.Discard)
	if exitCode(err) != 130 || !e.listed(dir) {
		t.Fatalf("Remove from list's question stopped by Ctrl-C: exit %d, listed %v", exitCode(err), e.listed(dir))
	}

	// The delete itself stopped by a signal: it says it did not finish and how to finish it, and
	// exits with the signal's code at once.
	for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143} {
		sigs := make(chan os.Signal, 1)
		var w bytes.Buffer
		got := make(chan int, 1)
		stop := watchDeleteStop(sigs, &w, dir, func(code int) { got <- code })
		sigs <- sig
		select {
		case code := <-got:
			if code != want {
				t.Fatalf("%v during the delete: exit %d, want %d", sig, code, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v during the delete did not stop it", sig)
		}
		stop()
		for _, s := range []string{"did not finish", "mailarchive delete-archive -out", "Delete… in MailArchive Desktop"} {
			if !strings.Contains(w.String(), s) {
				t.Fatalf("the stop does not say %q: %s", s, w.String())
			}
		}
	}
	// A watch stopped before any signal exits nothing.
	sigs := make(chan os.Signal, 1)
	exited := false
	stop := watchDeleteStop(sigs, io.Discard, dir, func(int) { exited = true })
	stop()
	if exited {
		t.Fatal("a delete that ended exited by the signal watch")
	}
}
