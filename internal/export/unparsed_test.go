package export

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// unparsedStub is what a raw reader delivers for a message the MIME parse gave up on
// (source.rawStub): the constant stub's identity, the bytes, the unparsed label.
func unparsedStub(raw []byte) *model.Message {
	terms := model.IdentityTerms{Subject: "(unreadable message)"}
	return &model.Message{
		Subject: "(unreadable message)", PlainBody: string(raw), Raw: raw, ReadGaps: []string{state.LabelUnparsed},
		ReadError: "the message could not be parsed", IdentityTerms: &terms,
	}
}

// covers: MA-318, R1, R3, S46
// A healthy capture of the bytes an earlier run could not parse supersedes that unparsed
// stub, metadata only (§4.7.4(4), FC25): the stub's label becomes "superseded" and
// RecoveredBy names the new record, and not one file is written, renamed or deleted for
// it (the stub's page keeps its bytes and its mtime, so its fixity still holds). A stub
// of OTHER bytes is untouched. The stub is stamped with the parser generation and the
// digest of its bytes when it is first written.
func TestAHealthyReadOfAnUnparsedStubsBytesSupersedesIt(t *testing.T) {
	out := t.TempDir()
	manifest, err := state.Load(filepath.Join(out, "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: a@example.com\r\nSubject: now readable\r\nMessage-ID: <h@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n")
	other := []byte("From: b@example.com\r\nSubject: another\r\n\r\nnever parsed\r\n")
	e := incExporter(out, manifest)
	for _, b := range [][]byte{raw, other} {
		if _, err := e.Export("store", []string{"Inbox"}, unparsedStub(b)); err != nil {
			t.Fatal(err)
		}
	}
	// Both stubs share the constant stub identity (ruling J8): one record, the second
	// skipped. Seed the second under a key of its own, as another folder's would be.
	if _, err := e.Export("store", []string{"Other"}, unparsedStub(other)); err != nil {
		t.Fatal(err)
	}
	stubKey, otherKey := state.Key("store", "Inbox", model.LegacyStubIdentity()), state.Key("store", "Other", model.LegacyStubIdentity())
	stub, _ := manifest.Get(stubKey)
	if stub.ParserGen != state.ParserGenCurrent || stub.RawDigest != rawDigest(raw) || len(stub.Terminal) != 1 || stub.Terminal[0] != state.LabelUnparsed {
		t.Fatalf("the stub = %+v; want terminal [unparsed], the current generation and the digest of its bytes", stub)
	}
	page := filepath.Join(out, filepath.FromSlash(stub.Path))
	before, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	at := mtime(t, page)
	time.Sleep(20 * time.Millisecond)

	healthy := source.ParseRFC822(raw)
	if len(healthy.ReadGaps) != 0 {
		t.Fatal("fixture: the bytes parse as a healthy message")
	}
	if _, err := e.Export("store", []string{"Inbox"}, healthy); err != nil {
		t.Fatal(err)
	}
	healthyKey := state.Key("store", "Inbox", healthy.Identity())
	if _, ok := manifest.Get(healthyKey); !ok {
		t.Fatalf("the healthy capture has no record at %q", healthyKey)
	}
	got, _ := manifest.Get(stubKey)
	if len(got.Terminal) != 1 || got.Terminal[0] != state.LabelSuperseded || got.RecoveredBy != healthyKey || got.Path != stub.Path || len(got.Missing) != 0 {
		t.Errorf("the superseded stub = %+v; want it re-classed to superseded, RecoveredBy %q, its path unchanged", got, healthyKey)
	}
	after, err := os.ReadFile(page)
	if err != nil || string(after) != string(before) || !mtime(t, page).Equal(at) {
		t.Errorf("the stub's page was written (err %v): supersede is metadata only", err)
	}
	if o, _ := manifest.Get(otherKey); len(o.Terminal) != 1 || o.Terminal[0] != state.LabelUnparsed {
		t.Errorf("the stub of other bytes = %+v; want it untouched", o)
	}
	assure.Reached(t, healthy.Raw, "the healthy capture's raw bytes")
}
