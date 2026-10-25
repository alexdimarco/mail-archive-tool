package export

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

var (
	unitsKeyOnce sync.Once
	unitsKey     *vault.Key
	unitsKeyErr  error
)

// testUnitsKey is one real archive key for this package's tests (made once: the
// shipped Argon2id cost makes each Create take about a second).
func testUnitsKey(t *testing.T) *vault.Key {
	t.Helper()
	unitsKeyOnce.Do(func() {
		_, unitsKey, _, unitsKeyErr = vault.Create("units test passphrase")
	})
	if unitsKeyErr != nil {
		t.Fatal(unitsKeyErr)
	}
	return unitsKey
}

// sealedExporter is an encrypted archive's Exporter writing into out, with its
// manifest kept OUTSIDE out so out holds only what the units write.
func sealedExporter(t *testing.T, out string) *Exporter {
	t.Helper()
	return &Exporter{
		OutDir:   out,
		Manifest: mustManifest(t),
		Log:      log.New(io.Discard, "", 0),
		KeepRaw:  true,
		Units:    &Units{Root: out, Key: testUnitsKey(t), RunStart: time.Now().Add(-time.Minute)},
	}
}

// fullMessage has a page, an attachment and an original.
func fullMessage(body string) *model.Message {
	return &model.Message{
		Subject:           "Quarterly board minutes",
		SenderEmail:       "cfo@example.com",
		InternetMessageID: "<board-q3@example.com>",
		Received:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		HTMLBody:          "<p>" + body + "</p>",
		Raw:               []byte("Message-ID: <board-q3@example.com>\r\nSubject: Quarterly board minutes\r\n\r\n" + body),
		Attachments: []model.Attachment{{Filename: "minutes.pdf", WriteTo: func(w io.Writer) (int64, error) {
			n, err := w.Write([]byte("%PDF the minutes"))
			return int64(n), err
		}}},
	}
}

// onlyRecord is the manifest's single record.
func onlyRecord(t *testing.T, m *state.Manifest) (string, state.Record) {
	t.Helper()
	all := m.All()
	if len(all) != 1 {
		t.Fatalf("want one record, have %d", len(all))
	}
	for k, r := range all {
		return k, r
	}
	return "", state.Record{}
}

var unitPath = regexp.MustCompile(`^units/[0-9a-f]{2}/[0-9a-f]{32}$`)

// covers: MA-722, R37
func TestAnEncryptedExportSealsTheSamePlaintextAsAPlainOne(t *testing.T) {
	plainOut := t.TempDir()
	plain := &Exporter{OutDir: plainOut, Manifest: mustManifest(t), Log: log.New(io.Discard, "", 0), KeepRaw: true}
	if _, err := plain.Export("store", []string{"Board", "2026"}, fullMessage("approved")); err != nil {
		t.Fatal(err)
	}
	_, prec := onlyRecord(t, plain.Manifest)

	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Board", "2026"}, fullMessage("approved")); err != nil {
		t.Fatal(err)
	}
	_, rec := onlyRecord(t, exp.Manifest)

	if rec.Path != prec.Path {
		t.Fatalf("logical Path %q, want the plaintext archive's %q", rec.Path, prec.Path)
	}
	if !state.ValidUnitID(rec.Unit) || rec.Sealed == nil || rec.Sealed.HTML == nil || rec.Sealed.Zip == nil || rec.Sealed.EML == nil {
		t.Fatalf("the record must carry a unit and a sealed page, zip and original: %+v", rec)
	}
	stem := filepath.Join(plainOut, filepath.FromSlash(rec.Path[:len(rec.Path)-len(".html")]))
	for _, c := range []struct {
		kind   vault.UnitKind
		sealed *state.SealedUnit
		plain  string
		digest *state.FileDigest
		pdig   *state.FileDigest
	}{
		{vault.UnitMessage, rec.Sealed.HTML, stem + ".html", rec.Fixity.HTML, prec.Fixity.HTML},
		{vault.UnitAttachment, rec.Sealed.Zip, stem + zipSuffix, rec.Fixity.Zip, prec.Fixity.Zip},
		{vault.UnitOriginal, rec.Sealed.EML, stem + ".eml", rec.Fixity.EML, prec.Fixity.EML},
	} {
		got, err := exp.Units.Read(rec.Unit, c.kind, *c.sealed)
		if err != nil {
			t.Fatalf("kind %d: the sealed unit must open: %v", c.kind, err)
		}
		want, err := os.ReadFile(c.plain)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("kind %d: the sealed unit's plaintext differs from the plaintext archive's file", c.kind)
		}
		if *c.digest != *c.pdig {
			t.Fatalf("kind %d: Fixity %+v, want the plaintext archive's %+v (reproducibility lives at the plaintext layer)", c.kind, c.digest, c.pdig)
		}
	}

	var paths []string
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	for _, p := range assure.Reached(t, paths, "files under out") {
		if !unitPath.MatchString(p) {
			t.Fatalf("%q under an encrypted archive is not a unit: no folder tree, no logical name may reach disk", p)
		}
	}
	if len(paths) != 3 {
		t.Fatalf("want exactly 3 units (page, zip, original), have %v", paths)
	}
	for _, kind := range []vault.UnitKind{vault.UnitMessage, vault.UnitAttachment, vault.UnitOriginal} {
		if _, err := os.Stat(filepath.Join(out, exp.Units.Name(rec.Unit, kind, 1))); err != nil {
			t.Fatalf("kind %d is not at the name its key derives: %v", kind, err)
		}
	}
}

// covers: MA-723, R37
func TestAChangedUnitIsANewRevisionAndTheOldGoesOnlyAfterTheSave(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("draft one")); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, first := onlyRecord(t, exp.Manifest)
	durable := state.Record(first)

	exp.Mode = Full
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("draft one")); err != nil {
		t.Fatal(err)
	}
	_, same := onlyRecord(t, exp.Manifest)
	if same.Unit != first.Unit || same.Sealed.HTML.Rev != 1 || len(exp.pendingDeletes) != 0 {
		t.Fatalf("re-exporting unchanged content must keep every unit and revision (got html rev %d, %d pending deletes)", same.Sealed.HTML.Rev, len(exp.pendingDeletes))
	}

	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("draft two")); err != nil {
		t.Fatal(err)
	}
	_, changed := onlyRecord(t, exp.Manifest)
	if changed.Unit != first.Unit {
		t.Fatal("a re-export must keep the record's unit")
	}
	if changed.Sealed.HTML.Rev != 2 || changed.Sealed.EML.Rev != 2 || changed.Sealed.Zip.Rev != 1 {
		t.Fatalf("changed page and original are new revisions, the unchanged zip is kept: html %d eml %d zip %d", changed.Sealed.HTML.Rev, changed.Sealed.EML.Rev, changed.Sealed.Zip.Rev)
	}
	// The crash window: the new revision is written, the manifest not yet saved.
	// The durable record's units are all still there and still open.
	if _, err := exp.Units.Read(durable.Unit, vault.UnitMessage, *durable.Sealed.HTML); err != nil {
		t.Fatalf("before the save, the durable manifest's revision must still open: %v", err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, exp.Units.Name(first.Unit, vault.UnitMessage, 1))); !os.IsNotExist(err) {
		t.Fatalf("after the save the superseded revision must be gone (stat err %v)", err)
	}
	got, err := exp.Units.Read(changed.Unit, vault.UnitMessage, *changed.Sealed.HTML)
	if err != nil || !bytes.Contains(got, []byte("draft two")) {
		t.Fatalf("the new revision must open to the new page: %v", err)
	}
	if _, err := exp.Units.Read(first.Unit, vault.UnitMessage, state.SealedUnit{Rev: 2, SHA256: first.Sealed.HTML.SHA256, Size: first.Sealed.HTML.Size}); err == nil {
		t.Fatal("revision 1's digest must not validate revision 2's file")
	}
}

// covers: MA-723, R37
func TestTheOriginalIsCarriedByUnitAndADroppedZipGoesAfterTheSave(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	key, first := onlyRecord(t, exp.Manifest)

	// The record's logical stem changes (a naming rule, the path budget) while the
	// message stays the same one; the re-capture has no original of its own and its
	// attachment arrives empty, so nothing is archivable.
	moved := first
	moved.Path = "store/Inbox/2026-06-01_1200_an-older-naming-rule_00000000.html"
	exp.Manifest.Add(key, moved)
	m := fullMessage("v2")
	m.Raw = nil
	m.Attachments[0].WriteTo = func(io.Writer) (int64, error) { return 0, nil }
	exp.Mode = Full
	if _, err := exp.Export("store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	_, rec := onlyRecord(t, exp.Manifest)
	if rec.Sealed.EML == nil || *rec.Sealed.EML != *first.Sealed.EML || rec.Fixity.EML == nil || *rec.Fixity.EML != *first.Fixity.EML {
		t.Fatal("the original must be carried by unit, with its digests, whatever the stem now is (R20, MC8)")
	}
	if rec.Sealed.Zip != nil || rec.Fixity.Zip != nil {
		t.Fatal("a re-capture with nothing archivable drops the zip")
	}
	zip1 := filepath.Join(out, exp.Units.Name(first.Unit, vault.UnitAttachment, 1))
	if _, err := os.Stat(zip1); err != nil {
		t.Fatal("before the save the dropped zip is still the durable manifest's")
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(zip1); !os.IsNotExist(err) {
		t.Fatal("after the save the dropped zip is gone")
	}
	if _, err := exp.Units.Read(rec.Unit, vault.UnitOriginal, *rec.Sealed.EML); err != nil {
		t.Fatalf("the carried original must still open: %v", err)
	}
}

// covers: MA-723, R37
func TestADistinctMessageReusingAMessageIDGetsItsOwnUnit(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("one")); err != nil {
		t.Fatal(err)
	}
	other := fullMessage("two")
	other.Subject = "A different message entirely" // a different envelope: a distinct reuse
	if _, err := exp.Export("store", []string{"Inbox"}, other); err != nil {
		t.Fatal(err)
	}
	units := map[string]bool{}
	for _, r := range exp.Manifest.All() {
		units[r.Unit] = true
	}
	if len(units) != 2 {
		t.Fatalf("two messages, %d distinct units: a #fp-split must draw its own unit", len(units))
	}
}

// covers: MA-724, R37
func TestTheUnitStoreRefusesPlantedFilesAndSwappedRevisions(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	_, r1 := onlyRecord(t, exp.Manifest)
	exp.Mode = Full
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v2")); err != nil {
		t.Fatal(err)
	}
	_, r2 := onlyRecord(t, exp.Manifest)
	page2 := filepath.Join(out, exp.Units.Name(r2.Unit, vault.UnitMessage, 2))
	good, err := os.ReadFile(page2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exp.Units.Read(r2.Unit, vault.UnitMessage, *r2.Sealed.HTML); err != nil {
		t.Fatalf("positive twin: the current revision opens: %v", err)
	}

	old, _ := os.ReadFile(filepath.Join(out, exp.Units.Name(r1.Unit, vault.UnitMessage, 1)))
	for label, plant := range map[string][]byte{
		"an older revision swapped in": old,
		"a truncated unit":             good[:len(good)-1],
		"an over-long unit":            append(append([]byte{}, good...), 'x'),
	} {
		if err := os.WriteFile(page2, plant, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := exp.Units.Read(r2.Unit, vault.UnitMessage, *r2.Sealed.HTML)
		if !errors.Is(err, ErrUnitStore) {
			t.Fatalf("%s: want ErrUnitStore, got %v", label, err)
		}
	}
	os.WriteFile(page2, good, 0o644)
	if _, err := exp.Units.Read(r2.Unit, vault.UnitMessage, *r2.Sealed.HTML); err != nil {
		t.Fatalf("restored, the unit opens again: %v", err)
	}
}

// covers: MA-724, R37
func TestAttachmentsOverTheCeilingAreATerminalGap(t *testing.T) {
	orig := encryptedZipCeiling
	encryptedZipCeiling = 64
	t.Cleanup(func() { encryptedZipCeiling = orig })
	out := t.TempDir()
	exp := sealedExporter(t, out)
	m := fullMessage("big")
	m.Attachments = []model.Attachment{{Filename: "huge.bin", WriteTo: func(w io.Writer) (int64, error) {
		n, err := w.Write(bytes.Repeat([]byte{0x42}, 4096))
		return int64(n), err
	}}}
	if _, err := exp.Export("store", []string{"Inbox"}, m); err != nil {
		t.Fatalf("a message over the ceiling is still archived: %v", err)
	}
	_, rec := onlyRecord(t, exp.Manifest)
	if rec.Sealed.Zip != nil || rec.Sealed.HTML == nil {
		t.Fatal("over the ceiling: the page is written and no zip unit is")
	}
	if len(rec.Terminal) != 1 || rec.Terminal[0] != "huge.bin" {
		t.Fatalf("the attachment must be a terminal gap: Terminal %v Missing %v", rec.Terminal, rec.Missing)
	}
	if !hasIssue(exp.Issues, "attachments-too-large", "huge.bin") {
		t.Fatalf("the issue list must name it: %+v", exp.Issues)
	}
}

// covers: MA-724, R37
func TestAnEncryptedExportRefusesARecordWithNoUnit(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	plain := &Exporter{OutDir: t.TempDir(), Manifest: exp.Manifest, Log: log.New(io.Discard, "", 0)}
	if _, err := plain.Export("store", []string{"Inbox"}, fullMessage("x")); err != nil {
		t.Fatal(err)
	}
	exp.Mode = Full
	_, err := exp.Export("store", []string{"Inbox"}, fullMessage("x"))
	if !errors.Is(err, errUnitlessRecord) {
		t.Fatalf("a unit-less record in an encrypted export: want errUnitlessRecord, got %v", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatal("the refusal must write nothing")
	}
}
