package app

import (
	"bufio"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// Finalization (design §4.13): the presence check, README, manifest, directory syncs
// and SHA256SUMS.txt, then the seal-and-finish step, which every finishing path
// shares: normal finalization, Finish as partial and Check and finish. The manifest
// and the sums are built from the run record, never from the destination (XC1), and
// the fingerprint is the SHA-256 of the sums' in-memory bytes (P9).

// copyCounts counts a run record: every planned message is exported, listed as
// failed or not attempted, an amendment replacing the line it amends.
func countRecord(path string) (CopyCounts, error) {
	var c CopyCounts
	h, err := readHeaderOnly(path)
	if err != nil {
		return c, err
	}
	c.Selected = h.Planned
	seen := 0
	err = walkProgress(path, func(m *ceMsgLine) error {
		seen++
		switch m.Status {
		case statusNotAttempted:
			c.NotAttempted++
		case statusFailed:
			c.Failed++
		default:
			c.Exported++
			switch m.Status {
			case statusWithoutOrig:
				c.WithoutOriginal++
			case statusOriginalOnly:
				c.OriginalOnly++
			}
		}
		return nil
	})
	c.NotAttempted += c.Selected - seen // a message with no line was never attempted
	return c, err
}

// finalStatus is the status a run ends with: a run that went through every message
// is complete only if every one was exported with every requested file.
func finalStatus(proposed CopyStatus, c CopyCounts) CopyStatus {
	if proposed == CopyStatusComplete && (c.Failed > 0 || c.NotAttempted > 0 || c.OriginalOnly > 0) {
		return CopyStatusPartial
	}
	return proposed
}

// copyLabel is what the activity page says of a sealed status (design §4.13, §4.16).
func copyLabel(status CopyStatus, c CopyCounts, stop *CopyStop) string {
	reason := "the app stopped while this export was running"
	if stop != nil && stop.RID != "" {
		reason = stopReason(stop.RID)
	}
	switch status {
	case CopyStatusComplete:
		return "Complete"
	case CopyStatusPartial:
		if c.Failed+c.NotAttempted == 0 && c.OriginalOnly > 0 {
			return fmt.Sprintf("Finished with problems — %s exported without a readable copy", commas(c.OriginalOnly))
		}
		return fmt.Sprintf("Finished with problems — %s messages not exported", commas(c.Failed+c.NotAttempted))
	case CopyStatusCancelled:
		return "Cancelled"
	case CopyStatusFinishedAsPartial:
		return "Stopped: " + reason + " (finished as partial)"
	}
	return "Stopped: " + reason
}

// finalize writes the run's README, manifest and sums from its record and seals it.
func (r *CopyRun) finalize(proposed CopyStatus, stop *CopyStop) (*CopyResult, error) {
	e := r.env
	defer r.fs.Close()
	defer r.rec.Close()
	fin := &ceFinish{env: e, fs: r.fs, rec: r.rec, hdr: r.hdr, dirs: r.dirs, started: r.started}
	res, err := fin.run(proposed, stop, false, false)
	if res != nil {
		res.RunID, res.Root, res.RootName = r.runID, r.rootAbs, r.rootName
	}
	if err != nil {
		if sum, serr := readSummary(r.rec.path); serr == nil && sum.Sealed != nil && res != nil {
			// The seal is written, and its ledger line, marker action or journal end is
			// still owed: "Finished writing", which reconcile or Check and finish
			// completes. A state-directory failure is R-d2, which the page shows.
			res.Label = "Finished writing (" + res.Label + ")" + finishingTail
			var se *ceStateErr
			if errors.As(err, &se) && res.Stop == nil {
				s := &CopyStop{RID: "R-d2", Class: stopClassState}
				res.Stop = r.complete(s, int(r.done.Load()), "")
			}
			return res, nil
		}
		// Finalization could not finish: the marker stays and no seal is written, so
		// the run is unsealed and Finish as partial completes the part once the cause
		// is cleared (RR2-C7). The reason is recorded when the state volume takes it.
		s := &CopyStop{RID: "R-d2", Class: stopClassState}
		var se *ceStateErr
		if !errors.As(err, &se) {
			if stop != nil && stop.RID != "" {
				s = stop
			} else {
				s = &CopyStop{RID: "R-g", Class: stopClassDestination}
			}
		}
		if !s.ready {
			extra := ""
			if !errors.As(err, &se) {
				extra = leaveName(err)
			}
			s = r.complete(s, int(r.done.Load()), extra)
		}
		_ = r.rec.append(ceStoppedLine{T: "stopped", RID: s.RID, Class: s.Class, Reason: stopReason(s.RID), At: e.nowTime()})
		_ = e.journalAppend(ceJournalLine{Run: r.runID, Event: "stopped", Reason: stopReasonText(s)})
		return &CopyResult{RunID: r.runID, Root: r.rootAbs, RootName: r.rootName, Status: CopyStatusFailed,
			Outcome: CopyOutcomeFailed, Stop: s, Label: "Stopped: " + stopReason(s.RID)}, nil
	}
	return res, nil
}

// ceFinish carries what finalization needs, from a live run or from a record.
type ceFinish struct {
	env     *CopyExportEnv
	fs      ceFS
	rec     *ceRecord
	hdr     ceHeaderLine
	dirs    []string
	started time.Time
}

// run does finalization steps 1-5, then the seal-and-finish step. replace is true
// where the run is finishing its own earlier, interrupted attempt in its own
// confirmed root (the files it wrote before are replaced, nothing else), and rehash
// asks the presence check to re-hash each recorded file (Finish as partial).
func (f *ceFinish) run(proposed CopyStatus, stop *CopyStop, replace, rehash bool) (*CopyResult, error) {
	e := f.env
	e.step("W7pre")
	// 1. Presence check (XC14): a recorded file gone since its write (an antivirus
	// quarantine, say) makes the run partial and is named in the failures.
	if err := f.presence(rehash); err != nil {
		return nil, err
	}
	counts, err := countRecord(f.rec.path)
	if err != nil {
		return nil, err
	}
	status := finalStatus(proposed, counts)

	finished := e.nowTime()
	// 2. README, whose status is conditional in its own text (XC5).
	readme := f.readmeText(status, counts, stop, finished)
	if err := writeTempFile(f.fs, "", copyReadmeName, []byte(readme), replace); err != nil {
		return nil, finalWriteErr(err)
	}
	e.step("W7")

	// 3. The manifest, streamed from the run record.
	manHash, err := f.writeManifest(status, counts, stop, finished, replace)
	if err != nil {
		return nil, finalWriteErr(err)
	}
	e.step("W8")

	// 4. Directory syncs, batched (XC5).
	for _, d := range append([]string{""}, f.dirs...) {
		// A directory that is gone, or that was swapped for a link out of the root, is
		// the presence check's finding (its files are listed as failures), not a
		// reason to leave the folder unfinished.
		if err := f.fs.SyncDir(d); err != nil && d != "" && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotDirectory) || strings.Contains(err.Error(), "path escapes")) {
			continue
		} else if err != nil {
			return nil, finalWriteErr(err)
		}
	}

	// 5. SHA256SUMS.txt, built from the recorded hashes and the two files just written.
	sums, err := f.buildSums(readme, manHash)
	if err != nil {
		return nil, err
	}
	if err := writeTempFile(f.fs, "", copySumsName, sums, replace); err != nil {
		return nil, finalWriteErr(err)
	}
	e.step("W9")

	fp := sha256.Sum256(sums)
	res := &CopyResult{Status: status, Outcome: status.Outcome(), Counts: counts, Fingerprint: hex.EncodeToString(fp[:]), Stop: stop}
	res.Label = copyLabel(status, counts, stop)
	seal := &ceSeal{env: e, fs: f.fs, rec: f.rec, hdr: f.hdr, status: status, counts: counts, stop: stop, sums: sums, fingerprint: res.Fingerprint}
	if err := seal.run(); err != nil {
		return res, err
	}
	return res, nil
}

// finalWriteErr is a failure to write a finalization file into the destination:
// the marker stays, and Finish as partial completes the part (RR2-C7).
func finalWriteErr(err error) error {
	var se *ceStateErr
	if errors.As(err, &se) {
		return err
	}
	return &ceFinalizeErr{err}
}

// ceFinalizeErr is a destination failure during finalization.
type ceFinalizeErr struct{ err error }

func (e *ceFinalizeErr) Error() string { return "couldn't finish the export folder: " + e.err.Error() }
func (e *ceFinalizeErr) Unwrap() error { return e.err }

// presence is finalization step 1 and, with rehash, Finish as partial's check: each
// recorded file is Lstat-ed through the root (and, for Finish as partial, re-hashed
// against the recorded hash, because a power loss can leave a present file damaged).
// A message whose file is gone is amended to failed (finalization) or not attempted
// (Finish as partial: "a file recorded just before a power loss may be gone"); one
// whose bytes differ is a listed failure, never a sums line.
func (f *ceFinish) presence(rehash bool) error {
	var amend []ceMsgLine
	err := walkProgress(f.rec.path, func(m *ceMsgLine) error {
		if m.Status != statusExported && m.Status != statusWithoutOrig && m.Status != statusOriginalOnly {
			return nil
		}
		for _, file := range m.Files {
			fi, err := f.fs.Lstat(file.Path)
			if err != nil && !fileGone(err) {
				// The volume does not answer (an I/O error, a stick that dropped off): that
				// says nothing about this file, so no message is marked missing on it.
				return finalWriteErr(err)
			}
			if err != nil || !fi.Mode().IsRegular() {
				if rehash {
					amend = append(amend, ceMsgLine{T: "msg", N: m.N, Status: statusNotAttempted, Amend: true,
						Reason: "the file was no longer there when the export was finished"})
				} else {
					amend = append(amend, ceMsgLine{T: "msg", N: m.N, Status: statusFailed, Class: classRetryable, Amend: true,
						Reason: "the exported file " + file.Path + " was no longer in the export folder when it was finished"})
				}
				return nil
			}
			if rehash {
				got, err := hashInRoot(f.fs, file.Path)
				if err != nil && !fileGone(err) {
					return finalWriteErr(err)
				}
				if err != nil || got != file.SHA256 || fi.Size() != file.Size {
					amend = append(amend, ceMsgLine{T: "msg", N: m.N, Status: statusFailed, Class: classRetryable, Amend: true,
						Reason: "damaged when the computer stopped"})
					return nil
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, a := range amend {
		if err := f.rec.append(a); err != nil {
			return err
		}
	}
	return nil
}

// fileGone reports whether an error from looking a recorded file up says the file
// is not there: it does not exist, or its directory was swapped for a link out of
// the root, so the path no longer reaches it.
func fileGone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "path escapes")
}

func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hashInRoot is the SHA-256 of a file in the root.
func hashInRoot(fsys ceFS, rel string) (string, error) {
	fh, err := fsys.Open(rel)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---- README -----------------------------------------------------------------

// The verification commands README gives (design §4.10, XC14).
const (
	readmePowerShell = `$n=0;$bad=0;$gone=0; Get-Content -Encoding UTF8 SHA256SUMS.txt | ForEach-Object { $h,$p = $_ -split '  ',2; $n++; if (-not (Test-Path -LiteralPath $p)) { $gone++; "MISSING $p" } elseif ((Get-FileHash -Algorithm SHA256 -LiteralPath $p).Hash -ne $h.ToUpper()) { $bad++; "MISMATCH $p" } }; if ($bad+$gone -eq 0) { "OK: $n files checked" } else { "$bad changed, $gone missing of $n" }`
	readmeExtraPOSIX = `find . -type f ! -name SHA256SUMS.txt ! -name _EXPORT-NOT-FINISHED.txt | sed 's|^\./||' | sort | comm -23 - <(cut -c67- SHA256SUMS.txt | sort)`
	readmeExtraPS    = `Compare-Object ((Get-ChildItem -Recurse -File | Where-Object { $_.Name -notin 'SHA256SUMS.txt','_EXPORT-NOT-FINISHED.txt' } | ForEach-Object { (Resolve-Path -Relative $_.FullName).Substring(2) -replace '\\','/' }) | Sort-Object) ((Get-Content -Encoding UTF8 SHA256SUMS.txt | ForEach-Object { ($_ -split '  ',2)[1] }) | Sort-Object)`
)

func (f *ceFinish) readmeText(status CopyStatus, c CopyCounts, stop *CopyStop, finished time.Time) string {
	h := f.hdr
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	// The first line is the run id: the folder's identity check reads it (§4.9), so it stays
	// first; what follows is for a person: what this is, whether it is whole, what it holds,
	// how to check it, and last the technical notes.
	line("%s%s", copyRunLinePrefix, h.Run)
	line("MailArchive Desktop copy export")
	line("")
	switch status {
	case CopyStatusComplete:
		line("Status: complete — valid only while no %s is in this folder and the check below passes.", copyMarkerName)
	default:
		line("Status: %s. This status is valid only while no %s is in this folder.", plainStatus(copyLabel(status, c, stop)), copyMarkerName)
		line("This export is not complete: %s of %s selected messages are in it; %s were not exported, %s were not attempted.",
			commas(c.Exported), commas(c.Selected), commas(c.Failed), commas(c.NotAttempted))
	}
	line("")
	switch h.Source.Kind {
	case "m365":
		line("Source: Microsoft 365, the mailbox of %s, read with that account's own sign-in (read-only).", h.Source.Mailbox)
	case "archive":
		what := "the MailArchive archive at"
		if h.Source.Other {
			what = "another MailArchive archive folder, read-only:"
		}
		line("Source: %s %s, read without changing it.%s", what, util.StripControl(h.Source.Archive), storesLine(h.Source.Stores))
	default:
		line("Source: %s", h.Source.Kind)
	}
	line("Exported: %s to %s (UTC)", h.Started.UTC().Format("2006-01-02 15:04:05"), finished.UTC().Format("2006-01-02 15:04:05"))
	line("Counts: %s selected, %s exported, %s failed, %s not attempted.", commas(c.Selected), commas(c.Exported), commas(c.Failed), commas(c.NotAttempted))
	if len(h.Folders) > 0 {
		var names []string
		for i, f := range h.Folders {
			if i == 20 {
				names = append(names, fmt.Sprintf("and %d more", len(h.Folders)-20))
				break
			}
			names = append(names, fmt.Sprintf("%s (%s)", util.StripControl(f.Folder), commas(f.Messages)))
		}
		line("Selected folders: %s.", strings.Join(names, ", "))
	}
	if c.Failed > 0 {
		line("Messages that were not exported: %s. export-manifest.json lists each one under \"failures\", with its subject, folder and the reason.", commas(c.Failed))
		shown := 0
		_ = walkMessages(f.rec.path, func(p *cePlanLine, m *ceMsgLine) error {
			if m != nil && m.Status == statusFailed && shown < 10 {
				shown++
				line("  - %s (%s): %s", util.StripControl(p.Subject), p.FolderPath(), util.StripControl(m.Reason))
			}
			return nil
		})
		if c.Failed > shown {
			line("  - and %s more, in the manifest.", commas(c.Failed-shown))
		}
	}
	if c.NotAttempted > 0 {
		line("Messages that were never tried: %s. export-manifest.json lists their ids under \"not_attempted\".", commas(c.NotAttempted))
	}
	if h.Source.Kind == "archive" {
		line("Layout: messages/<store>/<folder>/<year-month>/<date>_<time>_<subject>_<id>.eml (or .html and -attachments.zip where the archive kept no original), at most %d files to a folder; export-manifest.json lists every message and file; %s lists every file with its SHA-256.", copyFilesPerDir, copySumsName)
	} else {
		line("Layout: messages/<folder>/<year-month>/<date>_<time>_<subject>_<id>.eml, at most %d files to a folder; export-manifest.json lists every message and file; %s lists every file with its SHA-256.", copyFilesPerDir, copySumsName)
	}
	if len(h.ChainParts) > 0 {
		line("This is part %d of a set made by retrying an export. The earlier parts: %s.", len(h.ChainParts)+1, joinChain(h.ChainParts))
	}
	line("")
	line("How to check this export")
	line("Open a terminal in this folder: on Windows, type powershell in the Explorer address bar; on macOS, Finder > Services > New Terminal at Folder; on Linux, your file manager's Open in Terminal.")
	line("- Linux:   sha256sum -c %s", copySumsName)
	line("- macOS:   shasum -a 256 -c %s", copySumsName)
	line("- Windows PowerShell 5.1 or later:")
	line("    %s", readmePowerShell)
	line("  It ends with \"OK: N files checked\" or the counts of what changed or is missing.")
	line("")
	line("What the check does not cover: the check proves every listed file is unchanged, or names the one that changed or is missing. It does not detect files added to this folder. Files not listed in %s are not part of the export.", copySumsName)
	line("Extra-files check, which prints any file that is not listed:")
	line("- Linux and macOS (bash or zsh):  %s", readmeExtraPOSIX)
	line("- Windows PowerShell:  %s", readmeExtraPS)
	line("")
	line("Fingerprint: run `sha256sum %s` (Linux), `shasum -a 256 %s` (macOS) or `Get-FileHash %s` (Windows) and compare it with the fingerprint the sender gave you separately (email, phone). Matching hashes prove the listed files are unchanged since the export only if that fingerprint also matches. There are no signatures: who made this export is not claimed.", copySumsName, copySumsName, copySumsName)
	line("")
	line("What the files are")
	switch h.Source.Kind {
	case "m365":
		line("- Each .eml is the exact byte stream Microsoft 365 returned for the message when it was exported (its MIME content). It is not guaranteed to equal the bytes that arrived over SMTP, and two downloads of the same message are not guaranteed identical. The hashes prove the files are unchanged since the export, nothing earlier.")
	case "archive":
		line("- Each .eml is the original message file the MailArchive archive kept, copied byte-exact and checked against the checksum the archive recorded for it (a file with no recorded checksum is copied as stored, and its entry in export-manifest.json says so). Where the archive kept no original, the message is its archived page (.html) and attachments zip (-attachments.zip), as the archive wrote them: the page's links back to the archive's folder and root pages do not work in this export. A page that does not carry MailArchive's security policy is copied as text (.archived-page.txt) instead of as a page. The hashes prove the files are unchanged since the export, nothing earlier.")
	}
	line("- Open the originals with the same caution as email: they can hold links and attachments.")
	line("- Files whose names end .html, .attachments or .archived-page.txt, where present, are conveniences made from the original, which stays the authoritative copy.")
	line("")
	line("Caveats")
	line("- Windows Explorer and some zip tools cannot open paths over 260 characters: choose a shorter destination, or turn off Preserve folder structure, if a path is too long.")
	line("- On FAT, exFAT and most network shares, file permissions are not kept: the drive or the share decides who can read these files. On Windows this folder inherited its destination's permissions.")
	line("- On removable media: eject the drive before unplugging it.")
	return b.String()
}

// storesLine is the README's sentence naming every store the archive holds and its
// mailbox (XC21); empty when the source recorded none.
func storesLine(stores []CopyStoreInfo) string {
	var parts []string
	for _, s := range stores {
		name := util.StripControl(s.Store)
		if s.Mailbox != "" {
			name += " (" + util.StripControl(s.Mailbox) + ")"
		}
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return ""
	}
	return " It holds: " + strings.Join(parts, ", ") + "."
}

// plainStatus words a sealed status's label for the README's status line: "Stopped: the app
// stopped (finished as partial)" reads "stopped — the app stopped (finished as partial)".
func plainStatus(label string) string {
	if rest, ok := strings.CutPrefix(label, "Stopped: "); ok {
		return "stopped — " + rest
	}
	if label == "" {
		return label
	}
	return strings.ToLower(label[:1]) + label[1:]
}

func joinChain(parts []CopyChainRef) string {
	var s []string
	for _, p := range parts {
		s = append(s, fmt.Sprintf("part %d, %s", p.Part, p.Root))
	}
	return strings.Join(s, "; ")
}

// ---- manifest ---------------------------------------------------------------

// ceHashing is a file being written that hashes its bytes.
type ceHashing struct {
	w io.Writer
	h hash.Hash
}

func (c *ceHashing) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.h.Write(p[:n])
	return n, err
}

// writeManifest streams export-manifest.json from the run record: one entry per
// message, so memory stays bounded (XC1). It returns the file's SHA-256.
func (f *ceFinish) writeManifest(status CopyStatus, c CopyCounts, stop *CopyStop, finished time.Time, replace bool) (string, error) {
	e := f.env
	tmp, name, err := f.fs.CreateTemp("")
	if err != nil {
		return "", err
	}
	cleanup := func() { tmp.Close(); f.fs.Remove(name) }
	hw := &ceHashing{w: tmp, h: sha256.New()}
	bw := bufio.NewWriterSize(hw, 256<<10)
	put := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = bw.Write(b)
		return err
	}
	str := func(s string) []byte { b, _ := json.Marshal(util.StripControl(s)); return b }

	h := f.hdr
	chain := append([]CopyChainRef(nil), h.ChainParts...)
	chain = append(chain, CopyChainRef{Part: len(h.ChainParts) + 1, RunID: h.Run, Root: h.RootName})
	bw.WriteString(`{"format":"mailarchive-copy-export","format_version":1,`)
	bw.WriteString(`"tool":`)
	put(map[string]string{"name": "MailArchive Desktop", "version": e.versionString()})
	bw.WriteString(`,"run":`)
	put(map[string]any{"id": h.Run, "started_utc": h.Started.UTC().Format(time.RFC3339), "finished_utc": finished.UTC().Format(time.RFC3339),
		"status": status.manifestStatus(), "valid_if_absent": copyMarkerName, "retry_of": h.RetryOf, "chain": chain})
	bw.WriteString(`,"source":`)
	src := map[string]any{"kind": h.Source.Kind, "mailbox": h.Source.Mailbox, "tenant": h.Source.Tenant, "immutable_ids": h.Source.ImmutableIDs}
	if h.Source.Kind == "m365" {
		src["original_means"] = "MIME bytes returned by Microsoft Graph /$value at export time"
	}
	if h.Source.Kind == "archive" { // design §4.10: the archive and every store it holds with its mailbox (XC21)
		stores := make([]CopyStoreInfo, 0, len(h.Source.Stores))
		for _, s := range h.Source.Stores {
			stores = append(stores, CopyStoreInfo{Store: util.StripControl(s.Store), Mailbox: util.StripControl(s.Mailbox)})
		}
		src = map[string]any{"kind": "archive", "archive": util.StripControl(h.Source.Archive), "stores": stores,
			"original_means": "the .eml file the archive kept for the message, copied byte-exact"}
		if h.Source.Other {
			src["other_archive_folder"] = true
		}
	}
	put(src)
	bw.WriteString(`,"selection":`)
	sel := h.Folders
	if sel == nil {
		sel = []CopyPlanFolder{}
	}
	put(sel)
	bw.WriteString(`,"options":`)
	put(h.Options)
	bw.WriteString(`,"counts":`)
	put(c)
	bw.WriteString(`,"hash":"SHA-256","messages":[`)
	first := true
	err = walkMessages(f.rec.path, func(p *cePlanLine, m *ceMsgLine) error {
		if !first {
			bw.WriteByte(',')
		}
		first = false
		entry := map[string]any{"n": p.N, "source_id": p.SourceID, "internet_message_id": p.InternetMessageID,
			"subject": util.StripControl(p.Subject), "from": util.StripControl(p.From), "received_utc": p.Received.UTC().Format(time.RFC3339),
			"folder": p.FolderPath(), "status": statusNotAttempted, "files": []ceFileRec{}, "notes": []string{}}
		if p.Mailbox != "" {
			entry["mailbox"] = p.Mailbox
		}
		if m != nil && m.Status != statusNotAttempted {
			entry["status"] = m.Status
			if m.Files != nil {
				entry["files"] = m.Files
			}
			if m.Notes != nil {
				entry["notes"] = m.Notes
			}
		}
		return put(entry)
	})
	if err != nil {
		cleanup()
		return "", err
	}
	bw.WriteString(`],"failures":[`)
	first = true
	err = walkMessages(f.rec.path, func(p *cePlanLine, m *ceMsgLine) error {
		if m == nil || m.Status != statusFailed {
			return nil
		}
		if !first {
			bw.WriteByte(',')
		}
		first = false
		return put(map[string]any{"n": p.N, "source_id": p.SourceID, "subject": util.StripControl(p.Subject), "folder": p.FolderPath(),
			"received_utc": p.Received.UTC().Format(time.RFC3339), "class": m.Class, "reason": util.StripControl(m.Reason)})
	})
	if err != nil {
		cleanup()
		return "", err
	}
	bw.WriteString(`],"not_attempted":[`)
	first = true
	err = walkMessages(f.rec.path, func(p *cePlanLine, m *ceMsgLine) error {
		if m != nil && m.Status != statusNotAttempted {
			return nil
		}
		if !first {
			bw.WriteByte(',')
		}
		first = false
		b, _ := json.Marshal(p.SourceID)
		_, err := bw.Write(b)
		return err
	})
	_ = str
	if err != nil {
		cleanup()
		return "", err
	}
	bw.WriteString("]}\n")
	if err := bw.Flush(); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		f.fs.Remove(name)
		return "", err
	}
	if replace {
		err = f.fs.Replace("", name, copyManifestName)
	} else {
		err = f.fs.Place("", name, copyManifestName)
	}
	if err != nil {
		f.fs.Remove(name)
		return "", err
	}
	return hex.EncodeToString(hw.h.Sum(nil)), nil
}

func (e *CopyExportEnv) versionString() string {
	if e.Version != "" {
		return e.Version
	}
	return "dev"
}

// ---- SHA256SUMS.txt ---------------------------------------------------------

type ceSumLine struct {
	path string
	hash string
}

// buildSums is SHA256SUMS.txt: every file except itself and the marker, in the
// coreutils format (64 lowercase hex digits, two spaces, the path relative to the
// root with forward slashes), sorted by path, UTF-8 with no BOM. It is built from
// the hashes recorded when each file was written, and from the hashes of the README
// and the manifest it has just written, never by re-reading the destination.
func (f *ceFinish) buildSums(readme, manifestHash string) ([]byte, error) {
	rh := sha256.Sum256([]byte(readme))
	lines := []ceSumLine{{copyReadmeName, hex.EncodeToString(rh[:])}, {copyManifestName, manifestHash}}
	err := walkProgress(f.rec.path, func(m *ceMsgLine) error {
		if m.Status == statusFailed || m.Status == statusNotAttempted {
			return nil
		}
		for _, file := range m.Files {
			lines = append(lines, ceSumLine{sumsPath(file.Path), file.SHA256})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].path < lines[j].path })
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.hash)
		b.WriteString("  ")
		b.WriteString(l.path)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// ---- the seal-and-finish step ----------------------------------------------

// ceSeal is steps 6-9 of finalization (design §4.13): the sealed line, the
// fingerprint-ledger line, the sealed status's marker action and the journal end.
// Each part is idempotent, so a kill anywhere in it is completed by reconcile or by
// running the step again.
type ceSeal struct {
	env         *CopyExportEnv
	fs          ceFS
	rec         *ceRecord
	hdr         ceHeaderLine
	status      CopyStatus
	counts      CopyCounts
	stop        *CopyStop
	sums        []byte // the in-memory SHA256SUMS.txt bytes; nil when completing an earlier seal
	fingerprint string
}

func (s *ceSeal) run() error {
	e := s.env
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()

	sum, err := readSummary(s.rec.path)
	if err != nil {
		return err
	}
	// 6. The sealed line, unless it is already there.
	sealed := sum.Sealed
	if sealed == nil {
		if err := e.appendMissingLedgerLocked(s.hdr.Chain, s.hdr.Run); err != nil {
			return err
		}
		sl, err := s.sealedLine()
		if err != nil {
			return err
		}
		if err := s.rec.append(sl); err != nil {
			return err
		}
		sealed = &sl
		e.step("W9a")
	}
	// 7. The ledger line, once per run id.
	if err := e.ledgerFromSealedLocked(s.hdr.Run, sealed); err != nil {
		return err
	}
	e.step("W9b")
	// 8. The marker action, as the sealed status says.
	if err := s.markerAction(sealed); err != nil {
		return err
	}
	e.step("W10")
	// 9. The journal end, unless it is there, with the stop's reason.
	if !e.journalHas(s.hdr.Run, "end") {
		if err := e.journalAppend(ceJournalLine{Run: s.hdr.Run, Event: "end", Status: string(sealed.Status),
			Planned: sealed.Counts.Selected, Exported: sealed.Counts.Exported, Failed: sealed.Counts.Failed,
			Fingerprint: sealed.Fingerprint, Reason: stopReasonText(sealed.Stop)}); err != nil {
			return err
		}
	}
	e.step("W11")
	return nil
}

func stopReasonText(s *CopyStop) string {
	if s == nil || s.RID == "" {
		return ""
	}
	return s.RID + ", " + s.Class
}

// sealedLine builds the seal: the status, counts, the fingerprint of the in-memory
// sums, the stop and the chain's set state, resumed from the latest ledger line of
// the chain that carries one (a `gone` or `refused` line carries none and is
// skipped), with this part's sums bytes written into it.
func (s *ceSeal) sealedLine() (ceSealedLine, error) {
	e := s.env
	sl := ceSealedLine{T: "sealed", Status: s.status, Counts: s.counts, Fingerprint: s.fingerprint, Chain: s.hdr.Chain,
		Stop: s.stop, At: e.nowTime()}
	ledger, err := e.readLedger()
	if err != nil {
		return sl, stateErr(err)
	}
	h := sha256.New()
	var parts []string
	for i := len(ledger) - 1; i >= 0; i-- {
		l := ledger[i]
		if l.Chain == s.hdr.Chain && l.SetState != "" {
			raw, err := base64.StdEncoding.DecodeString(l.SetState)
			if err == nil {
				if u, ok := h.(encoding.BinaryUnmarshaler); ok && u.UnmarshalBinary(raw) == nil {
					parts = append(parts, l.Parts...)
					break
				}
			}
			h = sha256.New()
		}
	}
	h.Write(s.sums)
	state, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return sl, stateErr(err)
	}
	sl.SetState = base64.StdEncoding.EncodeToString(state)
	sl.SetFingerprint = hex.EncodeToString(h.Sum(nil))
	sl.Parts = append(parts, s.hdr.Run)
	return sl, nil
}

// appendMissingLedgerLocked appends the ledger line any other sealed part of the
// chain is still missing (a kill, or a failed append, between steps 6 and 7 of
// another part), copied from that part's sealed line, before this seal computes its
// set state (design §4.13 step 7). The ledger mutex is held.
func (e *CopyExportEnv) appendMissingLedgerLocked(chain, self string) error {
	ledger, err := e.readLedger()
	if err != nil {
		return stateErr(err)
	}
	for _, run := range e.chainRunsLocked(chain) {
		if run == self || ledgerHas(ledger, run) {
			continue
		}
		sum, err := readSummary(e.recordPath(run))
		if err != nil || sum.Sealed == nil {
			continue
		}
		if err := e.ledgerFromSealedLocked(run, sum.Sealed); err != nil {
			return err
		}
		ledger, _ = e.readLedger()
	}
	return nil
}

// ledgerFromSealedLocked appends run's ledger line, copied from its sealed line,
// unless the ledger has one. The mutex is held.
func (e *CopyExportEnv) ledgerFromSealedLocked(run string, sealed *ceSealedLine) error {
	ledger, err := e.readLedger()
	if err != nil {
		return stateErr(err)
	}
	if ledgerHas(ledger, run) {
		return nil
	}
	return e.appendJSONL(e.ledgerPath(), ceLedgerLine{V: 1, Run: run, Chain: sealed.Chain, Kind: "sealed", At: e.nowTime(),
		Status: string(sealed.Status), Fingerprint: sealed.Fingerprint, SetState: sealed.SetState,
		SetFingerprint: sealed.SetFingerprint, Parts: sealed.Parts})
}

// markerAction is step 8: complete or partial removes the app's own marker; a
// cancelled, failed or finished-as-partial run rewrites it CANCELLED or STOPPED by
// one atomic replacing rename of the run's own marker, never removing it first.
func (s *ceSeal) markerAction(sealed *ceSealedLine) error {
	switch sealed.Status {
	case CopyStatusComplete, CopyStatusPartial:
		if nonce := readMarkerNonce(s.fs); nonce != "" && nonce != s.hdr.Run {
			return nil // not this run's marker: never removed
		}
		if err := s.fs.Remove(copyMarkerName); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return finalWriteErr(err)
		}
	default:
		word := "STOPPED"
		if sealed.Status == CopyStatusCancelled {
			word = "CANCELLED"
		}
		reason := ""
		if sealed.Stop != nil && sealed.Stop.RID != "" {
			reason = stopReason(sealed.Stop.RID)
		}
		r := &CopyRun{env: s.env, runID: s.hdr.Run}
		text := r.markerText(word, &sealed.Counts, reason)
		if err := writeTempFile(s.fs, "", copyMarkerName, []byte(text), true); err != nil {
			return finalWriteErr(err)
		}
	}
	if err := s.fs.SyncDir(""); err != nil {
		return finalWriteErr(err)
	}
	return nil
}

// readMarkerNonce reads the run id on the marker's first line, "" when there is none.
func readMarkerNonce(fsys ceFS) string {
	f, err := fsys.Open(copyMarkerName)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	id, _ := strings.CutPrefix(strings.TrimSpace(line), copyRunLinePrefix)
	return strings.TrimSpace(id)
}
