//go:build linux || darwin

package keyring

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/util"
)

// covers: MA-788, MA-775, R37
// Recording by the resolved folder (§4.2, amended for OUT-04, OUT-05): an archive
// made through a linked output folder is recorded where it really is, so re-pointing
// the link later takes no protection away from it; and a recorded archive whose
// parent folder is swapped for a link reads case L — it may have been tampered with —
// and every keyless writer refuses it, never plaintext and never case 4.
func TestARecordedFolderReachedThroughANewLinkReadsAsALink(t *testing.T) {
	freshRecord(t)
	base := t.TempDir()

	// Made through a link: recorded at the target.
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	p, err := Create(filepath.Join(link, "archive"), testPassword, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	m, ok := loadRecord(t).Lookup(filepath.Join(real, "archive"))
	if !ok || m.Path.Spelling != encrecordSpelling(filepath.Join(util.ResolveExisting(real), "archive")) {
		t.Fatalf("an archive made through a link is recorded at %+v, want its real folder", m)
	}
	// Re-pointing the link loses nothing: the real archive stays refused to keyless
	// writers, and the link's new target is just another folder.
	elsewhere := filepath.Join(base, "elsewhere")
	if err := os.MkdirAll(filepath.Join(elsewhere, "archive"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	if res, _ := encmark.At(filepath.Join(real, "archive")); !res.Encrypted || res.Case != encmark.None {
		t.Fatalf("the real archive after its link was re-pointed: %+v", res)
	}

	// The parent of a recorded archive swapped for a link: case L.
	drive := filepath.Join(base, "drive")
	if err := os.Mkdir(drive, 0o700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(drive, "archive")
	if p, err = Create(out, testPassword, CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if u, err = p.Commit(); err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	if res, _ := encmark.At(out); res.Case != encmark.None {
		t.Fatalf("positive twin: the archive in place is case %v", res.Case)
	}
	if err := os.Rename(drive, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(base, "planted")
	if err := os.MkdirAll(filepath.Join(planted, "archive"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(planted, drive); err != nil {
		t.Fatal(err)
	}
	res, _ := encmark.At(out)
	assure.Reached(t, res.Recorded && res.Case == encmark.Link, "case L for a swapped parent")
	err = app.RefuseKeyless("archive into it", out)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("link", "tampered"),
		assure.NoSideEffect(func() bool { e, _ := os.ReadDir(filepath.Join(planted, "archive")); return len(e) == 0 }))
}
