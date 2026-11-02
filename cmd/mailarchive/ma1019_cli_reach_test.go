package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// otherComputersItems are the "What Delete does not reach" items of the platforms this one is not.
func otherComputersItems(goos string) []string {
	var out []string
	if goos != "windows" {
		out = append(out, "Windows File History and Previous Versions")
	}
	if goos != "darwin" {
		out = append(out, "macOS Time Machine")
	}
	return out
}

// covers: MA-1019, R36
// At the terminal, as on the Desktop, "What Delete does not reach" names only this computer's
// items in the plan and the result (the 1.5.3 friction review's B4: a Windows walk saw macOS Time
// Machine listed); the deletion record keeps the whole line, as it may be read on another computer.
func TestTheTerminalSaysWhatDeleteDoesNotReachOnThisComputer(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Reach")
	a := &scripted{isAttended: true, answers: []string{"Reach"}}
	var out bytes.Buffer
	if err := deleteArchive(a, dir, "", "", &out); err != nil {
		t.Fatalf("the delete: %v\n%s", err, a.transcript())
	}
	local := assure.Reached(t, archdelete.DoesNotReachLineOn(runtime.GOOS), "this computer's does-not-reach line")
	full := "What Delete does not reach: " + archdelete.DoesNotReach
	result, record, ok := strings.Cut(out.String(), recordHeading)
	if !ok {
		t.Fatalf("no record printed:\n%s", out.String())
	}
	plan := a.transcript()
	for name, text := range map[string]string{"the plan": plan, "the result": result} {
		if !strings.Contains(text, local) || strings.Contains(text, full) {
			t.Errorf("%s does not say this computer's line %q:\n%s", name, local, text)
		}
		for _, other := range otherComputersItems(runtime.GOOS) {
			if strings.Contains(text, other) {
				t.Errorf("%s names another computer's %q:\n%s", name, other, text)
			}
		}
	}
	if !strings.Contains(record, archdelete.DoesNotReach) {
		t.Errorf("the record does not keep the whole line:\n%s", record)
	}
}
