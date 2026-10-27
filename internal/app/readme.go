package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"runtime/debug"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/util"
)

// archiveReadme is written to <out>/README.txt on every run and by reindex:
// the archive must explain itself to someone with no tool and no manual (R7).
const archiveReadme = `This folder is an email archive written by mailarchive.

You need no software to read it: open index.html in any web browser, pick a
folder, pick a message. Every message is one self-contained .html page; if it
had attachments they are in the file of the same name ending in
-attachments.zip, next to it. A message may also have a .eml file beside it:
that is the original, unmodified message (only when the archive was made with
-raw). Message pages are inert by design: nothing in them runs or loads from
the network.

Layout
  <store name>/<folder>/<subfolder>/…      mirrors the mailbox's folder tree
  <folder>/index.html, index-2.html …      browsable, sortable listings
  YYYY-MM-DD_HHMM_subject_xxxxxxxx.html    one message (the time is UTC)
  YYYY-MM-DD_HHMM_subject_xxxxxxxx-attachments.zip
  index.html                               the list of all folders (start here)

Times: file names and the folder tables use UTC. Inside a message page, Sent /
Received / Date show the time with its original UTC offset.

Searching: search.db is a standard SQLite database (tables: docs, docs_fts —
an FTS5 full-text index over subject, people, folder, attachment names and
body). Any SQLite tool can query it. With the mailarchive program:
  mailarchive serve -out <this folder>   (a local search web page)
  mailarchive search -out <this folder> words...
Without it: use your system's file search over the .html files, or ripgrep.

Going back in time: for an archive of a live mailbox (Microsoft 365 via Graph),
"mailarchive serve" also offers a point-in-time view: pick a past date and see
the folders and messages as they stood then, including mail later deleted from
an ordinary folder. It reads the timeline in .mailarchive-history.jsonl. By
default a live capture skips the Deleted Items and Junk Email folders, so a
message that lived ONLY there is not in this archive unless the operator chose
to include them. This date view is offered only by "serve"; the pages here on
disk always show each message in the folder it was FIRST archived in, and never
change.

Removing a message for good (redaction): delete its files here — the .html
page, its matching -attachments.zip, and its .eml if one is present (only a
-raw archive has .eml) — then run "mailarchive reindex -out <this folder>",
which redacts it across all dates. Run "mailarchive verify -out <this folder>"
afterward: any file still listed under that name has not been removed yet. If
the same message still lives in the mailbox this archive captures, the next
scheduled run will archive it again, so remove or move it in the mailbox too. A
normal run never deletes a message file.

Transport headers: each message page has a collapsed panel labelled
"Transport headers as stored (unverified)" — the raw delivery headers
(Received, Authentication-Results, …) as received. They are shown for
reference, not as proof of origin: a sending or relaying server can forge them.

Verifying the files: every archived file's SHA-256 checksum is recorded in
.mailarchive-manifest.json. If you have the mailarchive program, run
  mailarchive verify -out <this folder>
to re-check every file against its recorded checksum. Exit 0 means every
recorded file is intact and none is missing a checksum; a file archived by an
older mailarchive carries none yet and shows as "unrecorded" until it is
re-exported or baselined with "mailarchive verify -record". This detects a
changed, truncated or missing file; it is not a signature, so it proves the
files have not changed, not who wrote them.

Items that could not be captured: if the tool could not read or download
something its source holds, it says so instead of hiding it. A page whose title
is "(unreadable message)", or whose top says "This item could only be partly
read from the source", holds what the tool could read of that message (the
original bytes are in the .eml beside it when the archive was made with -raw).
A message or folder the tool could not read at all has no page: it is a row in
attachments-report.tsv, which names its source, folder, date and subject. Each
row has a class:
  fillable = a later run of the tool against the original source may capture it;
  terminal = the source itself would not deliver it as it stands;
  info = nothing missing (the tool only noticed something odd).
If no one runs the tool against the source anymore, every fillable or terminal
row is missing from this archive. The end of each row's detail says why it
happened and what to do about it (or that there is nothing to do), and
whether the original still exists. With the mailarchive program, run "mailarchive status -out ." in this folder for the
same in plain words.

The one exception to that last sentence: a row for a part the tool kept as
received or cut short names a part that IS in this archive. A file ending
.undecoded in a message's -attachments.zip was kept exactly as the message
carried it, because the tool could not decode its encoding (the row names the
encoding, for example x-uuencode; a program that knows it can open the file).
remaining-parts.undecoded is the unread rest of a message that has more parts,
or nests them deeper, than the tool reads. A file ending .partial holds the
readable start of an attachment that was cut short or damaged. A row for a
message's text that was cut short or damaged names no file: the page shows what
could be decoded of the text. A row of kind unsplit-multipart means the
message's parts could not be told apart, so its page shows the whole text as one
body. A row of kind fidelity-fallback means the page shows the message as an
earlier version of the tool read it; a later version may read more. A row of
kind identity-stub means an earlier version of the tool could not read the
message at all and filed it under a placeholder; the page shows the message as
the tool that wrote it read it. A page that lists a .undecoded or .partial file
says so.

Housekeeping files (safe to leave alone):
  .mailarchive-manifest.json   which messages are archived (used for
                               incremental updates); do not edit
  .mailarchive-lastrun.json    record of the last backup run (status, counts)
  .mailarchive-lastverify.json record of the last verify (when it ran, whether
                               every file was intact)
  .mailarchive-schedule.json   the recurring-backup descriptor, if one was set
  .mailarchive-verify-schedule.json the scheduled verify's descriptor, if one
                               was set
  .mailarchive-history.jsonl   the go-back timeline: what changed each run (a
                               live capture writes it); appended each run and
                               rewritten only by reindex when redacting; read
                               by "mailarchive serve" for the point-in-time view
  attachments-report.tsv       messages, folders, attachments or bodies that
                               could not be captured (opens in a spreadsheet);
                               absent when there is nothing to report
  BACKUP-NEEDS-ATTENTION.txt   present only when the last backup run failed
                               or finished with items it could not capture;
                               explains what to do; removed after the next
                               successful run
  ARCHIVE-INTEGRITY-ATTENTION.txt present only when a verify found modified or
                               missing files; explains how to restore; removed
                               once a verify attests
  .mailarchive.lock            lock file; may remain after a run — its presence
                               does not mean a run is active (the real lock is an
                               OS flock released when the run ends)
`

// EncryptedNote is an encrypted archive's README.txt, the note beside it (phase 3
// §13.1, P3-5): what the folder is; how MailArchive opens it, a copy found on its
// own included; that MailArchive can make a normal unencrypted copy and put back
// lost key files; the official download, to be preferred to any program copy found
// beside the archive; the version that wrote it. It describes no layout, file or
// table — the plaintext README would tell a reader of the folder what its units hide
// (phase2a §4, MA-737). Its wording owes the friction review (P3-8).
func EncryptedNote() string {
	return fmt.Sprintf(encryptedNoteText, OpenArchiveWords, CopyWords, releasesPage, noteVersion())
}

// The Desktop's words the note quotes, kept here so the note and the buttons agree
// (a test pins them to the Desktop's own).
const (
	OpenArchiveWords = "Open an archive folder…"
	CopyWords        = "Make an unencrypted copy…"
	releasesPage     = "https://github.com/alexdimarco/mail-archive-tool/releases"
)

const encryptedNoteText = `This folder is an encrypted email archive made by MailArchive.

The messages in it are encrypted. Without the archive's password or its recovery
key nobody can read them — not you, and not MailArchive's makers.

To open it in MailArchive Desktop, choose "%[1]s", pick this folder,
and unlock it with its password or recovery key. At a terminal:
  mailarchive serve -out <this folder>

To take your mail out of encryption, MailArchive can make a normal, unencrypted
copy of this archive in a new folder: on its Encryption card choose
"%[2]s", or at a terminal run
  mailarchive decrypt -out <this folder> -dest <a folder of yours>

If the archive's key files are ever lost or damaged, MailArchive can put them back
with the archive's recovery file and recovery key ("Put back the key file" on its
Encryption card, or mailarchive restore-key-file at a terminal).

MailArchive is free from its official page:
  %[3]s
Prefer that download to any copy of the program you find beside an archive.
This note was written by MailArchive %[4]s.

Do not rename, edit or delete the files in this folder: MailArchive needs every
one of them to open the archive. To keep a copy, copy the whole folder.
`

// noteVersion is the version of MailArchive writing the note, as its build says.
func noteVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "(a development build)"
}

// NoteDigest is the SHA-256 of a note's text, as the archive's record seals it.
func NoteDigest(text []byte) string {
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])
}

// maxNoteRead bounds the note read for its check: the note is a few kilobytes.
const maxNoteRead = 64 << 10

// refreshNote checks the note beside the encrypted archive against the digest its
// record sealed, writes the note again and seals the new one's digest for the run's
// commit (§13.1): a note changed or removed since the last keyed run is told before
// it is rewritten — someone with the drive may have pointed it somewhere else.
func refreshNote(out string, store *container.Store, logger *log.Logger) {
	path := filepath.Join(out, "README.txt")
	// The note names the program copies of the last landed set; while an update of them
	// is under way, either the note before it or the one it wrote is the program's own
	// (program design R5).
	var landed *container.ProgramSet
	accept := map[string]bool{store.NoteDigest(): true}
	if ps, st, _ := store.Program(); st == container.ProgramOK {
		landed = &ps
		if ps.State == container.ProgramIntent && ps.Note != nil {
			accept[ps.Note.Current], accept[ps.Note.Incoming] = true, true
		}
	}
	if prev := store.NoteDigest(); prev != "" {
		if cur, err := util.ReadRegularFile(path, maxNoteRead); err != nil || !accept[NoteDigest(cur)] {
			logger.Printf("warning: the note beside this encrypted archive (README.txt) was changed or removed since its last backup; it was written again. If nobody here changed it, someone with access to the drive did — do not follow anything it said")
		}
	}
	text := []byte(EncryptedNoteFor(landed))
	if err := export.WriteFileAtomic(path, text); err != nil {
		logger.Printf("warning: could not write README.txt: %v", err)
		return
	}
	store.SetNoteDigest(NoteDigest(text))
}

// writeArchiveReadme writes README.txt atomically; failures are logged, never
// fatal (the archive is complete without it).
func writeArchiveReadme(out string, logger *log.Logger) {
	writeReadme(out, archiveReadme, logger)
}

func writeReadme(out, text string, logger *log.Logger) {
	if err := export.WriteFileAtomic(filepath.Join(out, "README.txt"), []byte(text)); err != nil && logger != nil {
		logger.Printf("warning: could not write README.txt: %v", err)
	}
}
