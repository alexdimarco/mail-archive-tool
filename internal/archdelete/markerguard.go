package archdelete

import (
	"errors"
	"path/filepath"
	"runtime"

	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/util"
)

// The delete marker's refusals (docs/design-archive-delete.md §15.3, §15.4; §15.10 slice 2):
// "did a delete of this folder not finish?" is answered by this computer's deletion log OR the
// marker in the folder, which travels with the folder to another computer, another user account
// and a remounted drive (MED-1, MED-2).
const (
	// CaseMarkerNotRegular is a refusal because the marker's name is not a regular file (a link,
	// a folder, a device, a pipe): never followed or opened, still a marker (§15.3).
	CaseMarkerNotRegular Case = "delete-marker-not-regular"
	// CaseCouldNotCheck is a refusal because the marker's lookup failed at a folder that is there
	// (DM-23): fail closed, kind archive_unwritable.
	CaseCouldNotCheck Case = "could-not-check"
)

// UnfinishedAt is UnfinishedDelete with the marker asked first (§15.3): the delete of folder that
// did not finish by this computer's deletion log at logPath ("" asks no log), or by the marker in
// the folder. The marker is a single lookup in the folder (delmark.Present: never opened or
// followed). When it is there the answer is never nil, whether or not the log can be read: the
// log's own line with the marker beside it when the log has one, else a delete only the marker
// knows (MarkerOnly), its Folder and Row the folder asked about. When it is not there the log is
// asked as UnfinishedDelete asks it (a log that cannot be read is its error, fail closed). A lookup
// that fails at a folder that is there is a *delmark.LookupError (DM-23); a folder that cannot be
// reached has no marker (DM-22).
//
// Three asks stay log-only (DM-21): Remove from list (UnlistRefusal, MA-1029), uninstall
// (PendingDeletes, MA-1007) and the plan's Finishes (planOf; it reads the marker itself, §15.4).
// They never refuse on the marker alone; since slice 5 they ask the log with the marker's run too
// (§15.5), so a delete this log knows stays known after a remount.
//
// The log is asked with the marker's run (§15.5, DeleteLog.UnfinishedFor): after a drive is plugged
// in again, this computer's own line still matches the folder when the marker names its delete
// (ByRun), and a delete only the marker knows whose run this log began says so (BegunHere, DM-10).
func UnfinishedAt(logPath, folder, goos string) (*Unfinished, error) {
	st, err := delmark.Present(folder)
	if err != nil {
		return nil, err
	}
	var d delmark.Details
	if st == delmark.Regular {
		d = delmark.Read(folder)
	}
	var u *Unfinished
	var dl *DeleteLog
	var lerr error
	if logPath != "" {
		if dl, lerr = ReadDeleteLog(logPath); lerr == nil {
			u = dl.UnfinishedFor(folder, goos, d.Run)
		}
	}
	return withMarker(u, lerr, folder, st, d, dl)
}

// unfinishedRouted is UnfinishedDelete with the folder's marker run (§15.5's routes), for the plan:
// a log that cannot be read is its error. dl is the log read, for DM-10's "begun here".
func unfinishedRouted(logPath, folder, goos string) (*Unfinished, *DeleteLog, error) {
	dl, err := ReadDeleteLog(logPath)
	if err != nil {
		return nil, nil, err
	}
	return dl.UnfinishedFor(folder, goos, delmark.Read(folder).Run), dl, nil
}

// UnfinishedToSay is the delete of folder that a status line says (`mailarchive status`, the
// Desktop's Status & health): UnfinishedAt's answer, except that a marker lookup that failed at a
// folder that is there (*delmark.LookupError) does not hide what this computer's deletion log
// knows (the slice-2 adversarial pass's LOW-3): the log's own line is returned then, its Marker
// Unknown. A failed lookup the log does not answer, and a log that cannot be read, stay errors:
// such a line says nothing of a delete, and every backup and action refuses the folder in its own
// words ("could not check", "could not read its deletion log").
func UnfinishedToSay(logPath, folder, goos string) (*Unfinished, error) {
	u, err := UnfinishedAt(logPath, folder, goos)
	var le *delmark.LookupError
	if !errors.As(err, &le) || logPath == "" {
		return u, err
	}
	// The marker could not be looked up, so its run is not known: the log's own match alone.
	if lu, lerr := UnfinishedDelete(logPath, folder, goos); lerr == nil && lu != nil {
		lu.Marker = delmark.Unknown
		return lu, nil
	}
	return u, err
}

// withMarker is the log's answer (u, lerr) for folder with the marker's state st and details d
// beside it; dl is the log read (nil when none was), which says whether a delete only the marker
// knows was begun here (DM-10).
func withMarker(u *Unfinished, lerr error, folder string, st delmark.State, d delmark.Details, dl *DeleteLog) (*Unfinished, error) {
	if st == delmark.Absent || st == delmark.Unknown {
		return u, lerr
	}
	if lerr == nil && u != nil {
		u.Marker, u.MarkerDetails = st, d
		return u, nil
	}
	abs := cleanAbs(folder)
	only := &Unfinished{Folder: abs, Row: abs, Marker: st, MarkerOnly: true, MarkerDetails: d}
	if lerr != nil {
		only.LogUnreadable = util.StripBidi(cleanErr(lerr))
	} else if d.Known && dl.RunBegunHere(d.Run) {
		only.BegunHere = true
	}
	return only, nil
}

// WithoutSettings is u, a delete UnfinishedAt or UnfinishedToSay found with no deletion log asked,
// marked as one whose log was not asked because MailArchive's settings folder could not be found
// (why): a delete only the marker knows is then never said to have "no record" in this computer's
// log (the slice-2 adversarial pass's LOW-4, its last item). `mailarchive status` asks it when
// DefaultLogPath fails. u itself is returned (nil stays nil).
func WithoutSettings(u *Unfinished, why error) *Unfinished {
	if u != nil && u.MarkerOnly && why != nil {
		u.NoSettings = util.StripBidi(cleanErr(why))
	}
	return u
}

// CouldNotCheck is why the marker's lookup failed at a folder that is there, when err (UnfinishedAt's
// or UnfinishedToSay's) is that failure, in words fit to show; "" otherwise. A status line says it
// (the slice-2 adversarial pass's LOW-4 (c)): every backup and action refuses the folder then.
func CouldNotCheck(err error) string {
	var le *delmark.LookupError
	if errors.As(err, &le) {
		return le.Cause()
	}
	return ""
}

// MarkerRefusal is the refusal of an action on a folder for the marker's part of UnfinishedAt's
// answer (u, err), or nil when the log's own words apply (or nothing stops it). done says what was
// not done ("it was not backed up", "it was not paused", …). A lookup that failed at a folder that
// is there: "MailArchive could not check this archive's folder…" (CaseCouldNotCheck, DM-23); a name
// that is not a regular file: delmark's words for it (CaseMarkerNotRegular); a delete only the
// marker knows: delmark's marker-only words (CaseUnfinished, DM-11), never "click Delete again",
// since this computer's person may never have deleted anything.
func MarkerRefusal(u *Unfinished, err error, done string) error {
	var le *delmark.LookupError
	if errors.As(err, &le) {
		return &couldNotCheckRefusal{r: &Refusal{Case: CaseCouldNotCheck, Message: le.Refusal(done)}, le: le}
	}
	if u == nil {
		return nil
	}
	switch {
	case u.Marker == delmark.NotRegular:
		return refuse(CaseMarkerNotRegular, "%s", delmark.NotRegularWords(done))
	case u.MarkerOnly && u.LogUnreadable != "":
		// The log could not be read: never "has no record of it" (LOW-4 (a)).
		return refuse(CaseUnfinished, "%s", delmark.LogUnreadableWords(done, u.LogUnreadable))
	case u.MarkerOnly && u.NoSettings != "":
		// No log was asked: the settings folder could not be found (LOW-4's last item).
		return refuse(CaseUnfinished, "%s", delmark.NoSettingsWords(done, u.NoSettings))
	case u.MarkerOnly && u.BegunHere:
		// This computer's log began it (DM-10): never "begun on another computer".
		return refuse(CaseUnfinished, "%s", delmark.BegunHereWords(done))
	case u.MarkerOnly:
		return refuse(CaseUnfinished, "%s", delmark.OnlyMarkerWords(done))
	}
	return nil
}

// MarkerActionRefusal is MarkerRefusal of the marker alone, for a caller with no deletion log to
// ask (a Desktop whose settings folder holds none): the marker refuses there too (DM-21).
func MarkerActionRefusal(folder, done string) error {
	u, err := UnfinishedAt("", folder, "")
	return MarkerRefusal(u, err, done)
}

// OnlyMarkerKnows reports a delete of folder that only its marker knows (§15.4, DM-18): a regular
// marker is there, this computer's deletion log at logPath was read ("" asks none) and has no line
// for it. False when the log knows the delete, cannot be read, or the lookup fails, and for a name
// that is not a regular file: those stay refused as ActionRefusal refuses them. The Desktop's
// settings save asks it to allow a save that keeps the location and installs nothing — on a
// computer that began no delete, nothing else clears the current archive, so its person could
// otherwise save no setting at all, not even the alert settings that are mailing "Delete not
// finished".
func OnlyMarkerKnows(logPath, folder string) bool {
	u, err := UnfinishedAt(logPath, folder, runtime.GOOS)
	return err == nil && u != nil && u.MarkerOnly && u.LogUnreadable == "" && u.Marker == delmark.Regular
}

// couldNotCheckRefusal is the "could not check" refusal: a *Refusal that also unwraps to the
// lookup's *delmark.LookupError, so app.RefuseUnfinishedDelete names its kind archive_unwritable.
type couldNotCheckRefusal struct {
	r  *Refusal
	le *delmark.LookupError
}

func (e *couldNotCheckRefusal) Error() string   { return e.r.Message }
func (e *couldNotCheckRefusal) Unwrap() []error { return []error{e.r, e.le} }

// MarkerBegun is when the delete only the marker knows began, as the record writes a moment
// (localStamp), or "" when the marker does not say (§15.3: only a valid began is ever shown).
func MarkerBegun(u *Unfinished) string {
	if u == nil || !u.MarkerDetails.Known {
		return ""
	}
	return localStamp(u.MarkerDetails.BeganAt())
}

// MarkerRowText is a row's line for a delete only the marker knows (§15.4, builder's words), or ""
// when this computer's log knows it (the row's own "Delete not finished: click Delete again").
// A name that is not a regular file says so in delmark's words for it.
func MarkerRowText(u *Unfinished) string {
	switch {
	case u == nil || !u.MarkerOnly:
		return ""
	case u.Marker == delmark.NotRegular:
		return delmark.NotRegularWords("it is not backed up")
	}
	begun := "a delete of this archive did not finish"
	if b := MarkerBegun(u); b != "" {
		begun = "a delete of this archive begun " + b + " did not finish"
	}
	if u.LogUnreadable != "" {
		// The log could not be read (LOW-4 (a); lead's words, owner-delegated 2026-11-02).
		return "Delete not finished: " + begun + ", and MailArchive could not read this computer's deletion log (" + u.LogUnreadable + "). Check that file, then click Delete… to finish it."
	}
	if u.BegunHere {
		// DM-10 (lead's words, owner-delegated 2026-11-02): begun here, though this computer's log no
		// longer matches the folder.
		return "Delete not finished: " + begun + ". It was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name). Click Delete… to finish it."
	}
	return "Delete not finished: " + begun + ". This computer's deletion log has no record of it (it was begun on another computer or user account). Click Delete… to finish it."
}

// markerPlanRefusal is Delete's plan's refusal of a marker name that is not a regular file
// (§15.3): a link by the link row, anything else (a folder, a device, a pipe) in delmark's words.
func markerPlanRefusal(folder, name string, link bool) error {
	if link {
		return linkRefusal(filepath.Join(folder, name))
	}
	return refuse(CaseMarkerNotRegular, "%s", delmark.MsgPlanNotRegular)
}
