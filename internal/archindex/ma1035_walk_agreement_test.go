package archindex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// saveOver renames tmp over dst as a save of the manifest does (state's replaceFile): on Windows a
// rename refused while a reader holds the file open is waited out within util.RenameRetryBudget.
func saveOver(tmp, dst string) error {
	inUse := func(err error) bool { return runtime.GOOS == "windows" && util.WindowsRenameInUse(err) }
	return util.ReplaceRetrying(tmp, dst, os.Rename, inUse, time.Sleep)
}

// wholeReadKind is the kind a whole state.LoadFrom of data gives, with ok false when LoadFrom
// refuses it: the oracle the walk must agree with (loadFromKind's, on bytes in place of a folder).
func wholeReadKind(data []byte) (string, bool) {
	m, err := state.LoadFrom(manifestName, data)
	if err != nil {
		return "", false
	}
	var local, cloud bool
	for id := range m.Stores {
		if strings.HasPrefix(id, "mailbox:") {
			cloud = true
		} else {
			local = true
		}
	}
	switch {
	case local && cloud:
		return "mixed", true
	case local:
		return "local", true
	case cloud:
		return "m365", true
	}
	return "empty", true
}

// nesting is the deepest nesting of objects and arrays in valid JSON data (0 for a scalar).
func nesting(data []byte) int {
	d, deepest := 0, 0
	inStr, esc := false, false
	for _, b := range data {
		switch {
		case inStr && esc:
			esc = false
		case inStr && b == '\\':
			esc = true
		case inStr && b == '"':
			inStr = false
		case inStr:
		case b == '"':
			inStr = true
		case b == '{' || b == '[':
			d++
			if d > deepest {
				deepest = d
			}
		case b == '}' || b == ']':
			d--
		}
	}
	return deepest
}

// refusedForContentsOnly reports whether a whole LoadFrom refuses data only for what its records
// or its capture gaps hold (MA-1034's not-checked list): with each record and each gap that is an
// object emptied to {}, LoadFrom reads it. (A repeated top-level name keeps only its last value
// here; the walk checks each.)
func refusedForContentsOnly(data []byte) bool {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return false
	}
	for name, raw := range doc {
		if !strings.EqualFold(name, "entries") && !strings.EqualFold(name, "capture_gaps") {
			continue
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			return false
		}
		for k, v := range members {
			if t := bytes.TrimSpace(v); len(t) > 0 && t[0] == '{' {
				members[k] = json.RawMessage(`{}`)
			}
		}
		emptied, err := json.Marshal(members)
		if err != nil {
			return false
		}
		doc[name] = emptied
	}
	again, err := json.Marshal(doc)
	if err != nil {
		return false
	}
	_, ok := wholeReadKind(again)
	return ok
}

// covers: MA-1035, MA-1034
// The walk agrees with a whole LoadFrom of the same bytes, outside what the operator was told it
// does not check (MA-1035 amended 2026-11-02: the bounded walk's second adversarial pass ran this
// as a fuzz for 90 seconds, 28.8 million inputs; its one finding, the fields beside the records,
// is fixed and now held here too): it never accepts what is not JSON; it accepts what LoadFrom
// refuses only for what the records or the capture gaps hold (MA-1034's not-checked list); when
// both accept, the kinds are the same; and it refuses nothing LoadFrom reads but a manifest nested
// deeper than 64 or stores over their bound. go test runs the seeds; go test -fuzz runs it as the
// pass did.
func FuzzRecognitionAgreesWithAWholeRead(f *testing.F) {
	for _, s := range []string{
		``, `null`, `{}`, `{"version":6}`, `{"version":6,"stores":{"path:/m":"a"}}`,
		`{"version":6,"stores":{"mailbox:x@y":"a","path:/m":"b"},"entries":{"a\u0000b":{"path":"a/x.html","missing":["body"]}}}`,
		`{"Version":6,"STORES":{"x@y":"a"},"Entries":{}}`,
		`{"version":6,"x":[0,-0,1.5,-2e10,3E+2,4e-1,true,false,null,"é\n"]}`,
		`{"version":7}`, `{"ſtores":{"mailbox:x@y":"a"}}`,
		`{"version":6,"stores":null,"stores":{"path:/m":"b"}}`,
		`{"version":6,"capture_gaps":{},"collapsed_tokens":{"a":true}}`,
		" \n{\"version\":6}\n ", `{"version":6,"stores":{"/a.pst":"a","u@x":"b"}}`,
		// The pass's finding: the fields beside the records, ill typed.
		`{"version":6,"stores":{"path:/m":"a"},"capture_gaps":5}`,
		`{"version":6,"stores":{"path:/m":"a"},"capture_gaps":[1,2]}`,
		`{"version":6,"stores":{"path:/m":"a"},"capture_gaps":{"k":"gap"}}`,
		`{"version":6,"stores":{"path:/m":"a"},"collapsed_tokens":"yes"}`,
		`{"version":6,"stores":{"path:/m":"a"},"collapsed_tokens":{"a":"x"}}`,
		`{"version":6,"stores":{"path:/m":"a"},"Collapsed_Tokens":{"a":null},"CAPTURE_GAPS":{"k":null}}`,
		// What the records and the gaps hold is not checked (told to the operator).
		`{"version":6,"stores":{"path:/m":"a"},"entries":{"k":{"path":5}}}`,
		`{"version":6,"stores":{"path:/m":"a"},"capture_gaps":{"k":{"scope":5}}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		gotKind, err := scanManifest(bytes.NewReader(data))
		gotOK := err == nil
		wantKind, wantOK := wholeReadKind(data)
		switch {
		case gotOK && !(len(data) == 0 || json.Valid(data)):
			t.Fatalf("the walk accepts what is not JSON: %q", data)
		case gotOK && !wantOK && len(data) > 0 && !refusedForContentsOnly(data):
			t.Fatalf("the walk reads %q what a whole LoadFrom refuses, beyond the records' and the gaps' contents: %q", gotKind, data)
		case gotOK && wantOK && gotKind != wantKind:
			t.Fatalf("the walk reads %q, a whole LoadFrom %q: %q", gotKind, wantKind, data)
		case !gotOK && wantOK:
			if nesting(data) > maxDepth || errors.Is(err, errManifestStores) || errors.Is(err, errManifestDepth) {
				return // told to the operator (MA-1035's row)
			}
			t.Fatalf("the walk refuses (%v) what a whole LoadFrom reads (%q): %q", err, wantKind, data)
		}
	})
}

// covers: MA-1035, MA-1034
// The kept verdict is the folder's, not its spelling's (MA-1035 amended 2026-11-02, the bounded
// walk's second adversarial pass): a folder spelt through a link that is then pointed at another
// archive is read again, so the same spelling gives the other archive's kind. Positive twin first:
// through the link to the first archive, its kind. Off Windows (a link there needs a privilege).
func TestTheKeptVerdictFollowsALinkPointedAtAnotherArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a link needs a privilege on Windows; the identity check is the same os.SameFile")
	}
	base := t.TempDir()
	a, b := filepath.Join(base, "A"), filepath.Join(base, "B")
	realArchive(t, a, 2, localSource)
	realArchive(t, b, 2, m365Source)
	link := filepath.Join(base, "L")
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	if k := Kind(link); k != "local" {
		t.Fatalf("the positive twin: through the link to a local archive, %q", k)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, link); err != nil {
		t.Fatal(err)
	}
	if k := Kind(link); k != "m365" {
		t.Errorf("the link now names a Microsoft 365 archive; recognition keeps %q", k)
	}
}

// covers: MA-1035, MA-1034
// Reads at once while the manifest is replaced as a save replaces it (a new file renamed over it)
// settle on the manifest that is there once the saves stop: no stale verdict is kept (MA-1035
// amended 2026-11-02, the bounded walk's second adversarial pass; run with -race it also proves the
// kept verdict and the walk-once map are shared safely). Positive twin first: each manifest alone
// is recognised with its kind.
func TestReadsDuringSavesSettleOnTheManifestThere(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "A")
	realArchive(t, dir, 50, localSource)
	localBytes, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(base, "B")
	realArchive(t, other, 50, m365Source)
	cloudBytes, err := os.ReadFile(filepath.Join(other, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if Kind(dir) != "local" || Kind(other) != "m365" {
		t.Fatalf("the positive twin: %q %q", Kind(dir), Kind(other))
	}
	var stop atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_ = Kind(dir)
				_ = IsArchive(dir)
				// On Windows each look holds the manifest open for a moment, and a rename over a file
				// held open is refused, so eight readers looking every millisecond or less keep a save
				// refused past its wait (once in ten runs each way on the lab VM, 2026-11-02; with no
				// reader no rename was refused, and one reader every 20 ms made the longest save 34 ms).
				// No Desktop reads in such a loop: there each reader pauses between looks, and the
				// reads stay concurrent with the saves.
				if runtime.GOOS == "windows" {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}
	for i := 0; i < 301; i++ {
		body := localBytes
		if i%2 == 1 {
			body = cloudBytes
		}
		tmp := filepath.Join(dir, fmt.Sprintf(".tmp-%d", i))
		if err := os.WriteFile(tmp, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := saveOver(tmp, filepath.Join(dir, manifestName)); err != nil {
			t.Fatal(err)
		}
	}
	stop.Store(true)
	wg.Wait()
	final, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	want, ok := wholeReadKind(final)
	if !ok || want != "local" {
		t.Fatalf("the fixture: the last save is the local manifest, read whole as %q (%v)", want, ok)
	}
	if got := Kind(dir); got != want {
		t.Fatalf("after the saves stopped, recognition reads %q; the manifest there is %q", got, want)
	}
	// One save more, with nothing reading: the verdict just kept is not the one given.
	tmp := filepath.Join(dir, ".tmp-last")
	if err := os.WriteFile(tmp, cloudBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveOver(tmp, filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}
	if got := Kind(dir); got != "m365" {
		t.Errorf("after one more save, recognition reads %q; the manifest there is a Microsoft 365 archive's", got)
	}
}
