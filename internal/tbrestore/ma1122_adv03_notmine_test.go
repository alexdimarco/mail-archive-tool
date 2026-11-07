package tbrestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1122, R40, S68
// ADV-03 (the R3–R5 adversarial pass, settled 2026-11-07): a listed name whose exclusive
// create failed because something already stood there is not the restore's, and the
// run's own cleanup never removes it. Another program makes, in the instant before the
// writer's create, a file at a listed file's name (B) or a folder at a listed folder's
// name (A.sbd): the write fails, its one cleanup removes nothing (W-LEFT: what it found
// named), every entry — the other program's included, byte for byte — stays, and the log
// says "abandoned". Positive twin: with nothing made first, the same restore finishes.
func TestRestoreCleanupKeepsANameItFailedToCreate(t *testing.T) {
	in := []FolderIn{{Names: []string{"A"}, Messages: 1}, {Names: []string{"A", "C"}, Messages: 1}, {Names: []string{"B"}, Messages: 1}}
	msgs := [][][]byte{{[]byte("Subject: a\n\nx\n")}, {[]byte("Subject: c\n\nz\n")}, {[]byte("Subject: b\n\ny\n")}}
	for _, tc := range []struct{ name, op, rel, words string }{
		{"a file at a listed file's name", "create", "B", "(B)"},
		{"a folder at a listed folder's name", "mkdir", "A.sbd", "(A.sbd)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, in, msgs)
			container := fx.top + ".sbd"
			other := filepath.Join(fx.lf.Dir, container, filepath.FromSlash(tc.rel))
			var before string
			f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
				if op == tc.op && name == container+"/"+tc.rel && before == "" {
					if tc.op == "mkdir" {
						_ = os.Mkdir(other, 0o700)
					} else {
						_ = os.WriteFile(other, []byte("another program's file"), 0o600)
					}
					before = fx.local()
				}
				return nil
			}}
			res, id := fx.write(context.Background(), f)
			rc := 0
			if res.Outcome == OutFailed && res.Clean != nil && res.Clean.Kind == CleanLeft && lastEv(fx.events(id)) == EvAbandoned {
				rc = 1
			}
			refusedAs(t, errFrom(rc, res.Words), "W-KEPT", func() bool { return before != "" && fx.local() == before }, "it did not make", tc.words, "removed nothing", fx.top)
		})
	}
	t.Run("positive twin: nothing made first", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		res, _ := fx.write(context.Background(), nil)
		if res.Outcome != OutDone || res.Restored != 3 {
			t.Fatalf("the restore finishes: %s %v", res.Outcome, res.Err)
		}
	})
}
