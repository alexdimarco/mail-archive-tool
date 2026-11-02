package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/health"
)

// hostileMbox is two messages from outside senders. The first has an HTML body with a dangling
// cid: image, so the exporter records its subject, and a subject of 180,000 '<' (about 176 KiB,
// under the 1 MiB header cap), which json.Marshal writes in the manifest as about 1.03 MiB.
var hostileMbox = "From a@example.com Mon Jan  1 00:00:00 2024\n" +
	"From: a@example.com\nTo: me@example.com\nSubject: " + strings.Repeat("<", 180000) + "\n" +
	"Message-ID: <hostile@x>\nDate: Mon, 01 Jan 2024 00:00:00 +0000\nContent-Type: text/html\n\n" +
	"<html><body><p>hi</p><img src=\"cid:logo\"></body></html>\n\n" +
	"From b@example.com Mon Jan  1 00:00:00 2024\n" +
	"From: b@example.com\nSubject: ordinary\nMessage-ID: <ok@x>\nDate: Mon, 01 Jan 2024 00:00:00 +0000\n\nbody\n"

// covers: MA-1035, MA-1023, R36, R12
// One email from an outside sender does not make a real archive "not an archive" (the MA-1034
// adversarial pass, finding 1: its subject, written over 1 MiB in the manifest, made
// archindex.IsArchive false, so add-archive refused the folder with "…cannot be read — run
// mailarchive status…" while status found nothing wrong, and a command-line user could not delete
// it). The archive built by the real export is recognised, status reads it, add-archive lists it
// and delete-archive deletes it. Positive twin first: an archive of ordinary mail is listed.
func TestOneHostileEmailDoesNotUnrecogniseTheArchive(t *testing.T) {
	e := newCLIDel(t)
	plain := filepath.Join(t.TempDir(), "Plain")
	e.archiveAt(plain)
	if err := addArchiveWith("", plain, io.Discard); err != nil || !e.listed(plain) {
		t.Fatalf("the positive twin: %v (listed %v)", err, e.listed(plain))
	}

	dir := filepath.Join(t.TempDir(), "Hostile")
	box := filepath.Join(t.TempDir(), "Inbox")
	if err := os.WriteFile(box, []byte(hostileMbox), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{box}, Out: dir, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	os.Remove(filepath.Join(dir, ".mailarchive.lock"))
	data, err := os.ReadFile(filepath.Join(dir, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	escaped := []byte("\\" + "u003c") // '<' as json.Marshal writes it: six bytes
	if !bytes.Contains(data, bytes.Repeat(escaped, 180000)) {
		t.Fatalf("the fixture: the manifest must hold the subject as a value over 1 MiB (%d bytes)", len(data))
	}
	in := health.Gather(dir, "")
	if !in.HasManifest || in.ManifestLoadErr != nil || in.Messages != 2 {
		t.Fatalf("the fixture: status reads the archive: manifest %v, error %v, %d messages", in.HasManifest, in.ManifestLoadErr, in.Messages)
	}
	if !archindex.IsArchive(dir) || archindex.Kind(dir) != "local" {
		t.Fatalf("an archive with one hostile email is not recognised: %v %q", archindex.IsArchive(dir), archindex.Kind(dir))
	}
	var out bytes.Buffer
	if err := addArchiveWith("", dir, &out); err != nil || !e.listed(dir) {
		t.Fatalf("add-archive refused an archive with one hostile email: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Hostile"}}, dir, "", "", &out); err != nil {
		t.Fatalf("delete-archive: %v\n%s", err, out.String())
	}
	if e.listed(dir) || !strings.Contains(out.String(), "Deleted.") {
		t.Fatalf("not deleted:\n%s", out.String())
	}
}

// covers: MA-1023, R36, R12
// add-archive's delete line, when a Desktop runs and when it cannot tell whether one does (a lock
// name that is a folder), offers the Desktop's Delete… and, as a separate choice, delete-archive
// once the Desktop is shut down — never "(with MailArchive Desktop shut down)" over both choices,
// which said Delete… needs the Desktop shut down (the MA-1034 adversarial pass, finding 5).
// Positive twin first: with no Desktop running the line is the command alone.
func TestAddArchivesDeleteLineWhenADesktopRunsOrItCannotTell(t *testing.T) {
	e := newCLIDel(t)
	line := func(dir string) string {
		t.Helper()
		var out bytes.Buffer
		if err := addArchiveWith("", dir, &out); err != nil || !e.listed(dir) {
			t.Fatalf("add-archive %s: %v", dir, err)
		}
		return out.String()
	}
	both := func(dir string) string {
		return "To delete it: click Delete… on its row in Your archives in MailArchive Desktop, or shut MailArchive Desktop down and run mailarchive delete-archive -out " + encmark.CommandPath(dir) + ".\n"
	}

	alone := filepath.Join(t.TempDir(), "Alone")
	e.archiveAt(alone)
	if got := line(alone); !strings.Contains(got, "To delete it: mailarchive delete-archive -out "+encmark.CommandPath(alone)+"\n") || strings.Contains(got, "Delete…") {
		t.Fatalf("the positive twin, no Desktop running:\n%s", got)
	}

	running := filepath.Join(t.TempDir(), "Running")
	e.archiveAt(running)
	stop := e.desktopRunning("")
	if got := line(running); !strings.Contains(got, both(running)) || strings.Contains(got, "(with MailArchive Desktop shut down)") {
		t.Fatalf("while a Desktop runs:\n%s\nwant %q", got, both(running))
	}
	stop()

	unknown := filepath.Join(t.TempDir(), "Unknown")
	e.archiveAt(unknown)
	if err := os.Mkdir(filepath.Join(e.lockDir, "desktop-s7.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, ok := desktopRunning().(*cannotTellError); !ok {
		t.Fatalf("the fixture: a lock name that is a folder must be one the check cannot tell: %v", desktopRunning())
	}
	if got := line(unknown); !strings.Contains(got, both(unknown)) {
		t.Fatalf("when it cannot tell whether a Desktop runs:\n%s\nwant %q", got, both(unknown))
	}
}
