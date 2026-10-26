package keyring

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

const fixture = "../../testdata/support.pst"

var quiet = log.New(io.Discard, "", 0)

// plaintextFiles lists the files in out a plaintext archive writes and an
// encrypted one never does: pages, originals, attachment zips, the index.
func plaintextFiles(t *testing.T, out string) []string {
	t.Helper()
	var found []string
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch n := d.Name(); {
		case strings.HasSuffix(n, ".html"), strings.HasSuffix(n, ".eml"), strings.HasSuffix(n, ".zip"), n == "search.db",
			n == ".mailarchive-manifest.json" && vault.ManifestIsPlaintext(p):
			found = append(found, p)
		}
		return nil
	})
	return found
}

// covers: MA-780, R37
func TestABackupThroughTheKeyringIsSealedAndItsFolderStaysEncrypted(t *testing.T) {
	freshRecord(t)
	out, _, u := made(t, nil)
	opts := app.Options{Inputs: []string{fixture}, Out: out}
	done, err := Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	res, err := app.Run(context.Background(), opts, quiet, nil)
	done()
	if err != nil {
		t.Fatalf("the keyed backup: %v", err)
	}
	assure.Reached(t, res.ManifestSize, "messages backed up")
	if p := plaintextFiles(t, out); len(p) > 0 {
		t.Fatalf("a keyed backup wrote plaintext: %v", p)
	}

	// Its key files deleted, the folder stays encrypted to a keyless backup: this
	// computer recorded it (A15).
	for _, name := range []string{vault.FileName, vault.BackupName} {
		if err := os.Remove(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = app.Run(context.Background(), app.Options{Inputs: []string{fixture}, Out: out}, quiet, nil)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "key files are missing"),
		assure.NoSideEffect(func() bool { return len(plaintextFiles(t, out)) == 0 }))
	if !errors.Is(err, app.ErrEncryptedNeedsKey) {
		t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
	}
}

// covers: MA-780, R37
// E2: an encrypted archive's -outlook PSTs go to MailArchive's working folder,
// never into the archive in plaintext, and are removed when the run ends; a locked
// archive gives none.
func TestAnEncryptedArchivesOutlookPSTsStayOutOfIt(t *testing.T) {
	freshRecord(t)
	out, _, u := made(t, nil)
	dir, cleanup, err := OutlookPSTDir(u)
	if err != nil {
		t.Fatal(err)
	}
	if util.Within(util.ResolveExisting(out), util.ResolveExisting(dir)) || filepath.Base(dir) != "_outlook-pst" {
		t.Fatalf("the PST folder %s is inside the archive %s", dir, out)
	}
	assure.Reached(t, exists(dir), "the working PST folder")
	cleanup()
	if exists(dir) {
		t.Fatal("cleanup left the PST folder")
	}
	u.Lock()
	if _, _, err := OutlookPSTDir(u); !errors.Is(err, ErrLocked) {
		t.Fatalf("a locked archive gave a PST folder: %v", err)
	}
}

// covers: MA-780, R37
func TestAKeyedRunRefusesAKeyThatIsNotTheRecordedArchives(t *testing.T) {
	freshRecord(t)
	outA, _, a := made(t, nil)
	_, _, b := made(t, nil)
	opts := app.Options{Inputs: []string{fixture}, Out: outA}
	done, err := Apply(&opts, a)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	// The folder holds A's key file and A's key, but the run is told B was recorded.
	opts.RecordedID = b.ArchiveID()
	_, err = app.Run(context.Background(), opts, quiet, nil)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(outA, b.ArchiveID().String()),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(outA, "units")) }))
}
