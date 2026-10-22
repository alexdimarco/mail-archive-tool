package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-568
// The pre-sync seam is read in exactly ONE place (design thunderbird-presync
// §4.1): opts.BeforeRead is called once, in Run, and no other entry point
// (RunGraph, Reindex, Verify, Rebuild, the extraction export) references it — so
// with the hook nil, which is every path today, Run's observable behaviour is
// unchanged (the rest of the suite is that golden).
func TestBeforeReadSeamSinglePlacement(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		calls += strings.Count(src, "opts.BeforeRead(") // the invocation, not the != nil guard
		if name != "run.go" && strings.Contains(src, "BeforeRead") {
			t.Errorf("%s references BeforeRead; the pre-sync seam must live only in run.go", name)
		}
	}
	if calls != 1 {
		t.Errorf("opts.BeforeRead is called %d times, want exactly 1 (the single Run call site)", calls)
	}
}
