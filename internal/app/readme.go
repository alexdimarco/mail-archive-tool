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

Housekeeping files (safe to leave alone):
  .mailarchive-manifest.json   which messages are archived (used for
                               incremental updates); do not edit
  attachments-report.tsv       messages whose attachments or bodies could
                               not be captured (opens in a spreadsheet);
                               absent when there is nothing to report
  .mailarchive.lock            held only while an archive run is in progress
`

// writeArchiveReadme writes README.txt atomically; failures are logged, never
// fatal (the archive is complete without it).
func writeArchiveReadme(out string, logger *log.Logger) {
	if err := export.WriteFileAtomic(filepath.Join(out, "README.txt"), []byte(archiveReadme)); err != nil && logger != nil {
		logger.Printf("warning: could not write README.txt: %v", err)
	}
}
