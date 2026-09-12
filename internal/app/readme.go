package app

import (
	"log"
	"path/filepath"

	"mail-archive-tool/internal/export"
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
the mailbox. It reads the timeline in .mailarchive-history.jsonl. This date
view is offered only by "serve"; the pages here on disk always show each
message in the folder it was FIRST archived in, and never change. To remove a
message from the archive at every date, delete its files here and then run
"mailarchive reindex -out <this folder>" — that redacts it across all dates. A
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

Housekeeping files (safe to leave alone):
  .mailarchive-manifest.json   which messages are archived (used for
                               incremental updates); do not edit
  .mailarchive-lastrun.json    record of the last backup run (status, counts)
  .mailarchive-lastverify.json record of the last verify (when it ran, whether
                               every file was intact)
  .mailarchive-schedule.json   the recurring-backup descriptor, if one was set
  .mailarchive-history.jsonl   the go-back timeline: what changed each run (a
                               live capture writes it); append-only, read by
                               "mailarchive serve" for the point-in-time view
  attachments-report.tsv       messages whose attachments or bodies could
                               not be captured (opens in a spreadsheet);
                               absent when there is nothing to report
  BACKUP-NEEDS-ATTENTION.txt   present only when the last backup run failed;
                               explains what to do; removed after the next
                               successful run
  ARCHIVE-INTEGRITY-ATTENTION.txt present only when a verify found modified or
                               missing files; explains how to restore; removed
                               once a verify attests
  .mailarchive.lock            lock file; may remain after a run — its presence
                               does not mean a run is active (the real lock is an
                               OS flock released when the run ends)
`

// writeArchiveReadme writes README.txt atomically; failures are logged, never
// fatal (the archive is complete without it).
func writeArchiveReadme(out string, logger *log.Logger) {
	if err := export.WriteFileAtomic(filepath.Join(out, "README.txt"), []byte(archiveReadme)); err != nil && logger != nil {
		logger.Printf("warning: could not write README.txt: %v", err)
	}
}
