package archdelete

import (
	"errors"
	"path/filepath"

	"mail-archive-tool/internal/delmark"
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
func UnfinishedAt(logPath, folder, goos string) (*Unfinished, error) {
	st, err := delmark.Present(folder)
	if err != nil {
		return nil, err
	}
	var u *Unfinished
	var lerr error
	if logPath != "" {
		u, lerr = UnfinishedDelete(logPath, folder, goos)
	}
	return withMarker(u, lerr, folder, st)
}

// withMarker is the log's answer (u, lerr) for folder with the marker's state st beside it.
func withMarker(u *Unfinished, lerr error, folder string, st delmark.State) (*Unfinished, error) {
	if st == delmark.Absent || st == delmark.Unknown {
		return u, lerr
	}
	var d delmark.Details
	if st == delmark.Regular {
		d = delmark.Read(folder)
	}
	if lerr == nil && u != nil {
		u.Marker, u.MarkerDetails = st, d
		return u, nil
	}
	abs := cleanAbs(folder)
	return &Unfinished{Folder: abs, Row: abs, Marker: st, MarkerOnly: true, MarkerDetails: d}, nil
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
	begun := "a delete of this archive did not finish."
	if b := MarkerBegun(u); b != "" {
		begun = "a delete of this archive begun " + b + " did not finish."
	}
	return "Delete not finished: " + begun + " This computer's deletion log has no record of it (it was begun on another computer or user account). Click Delete… to finish it."
}

// markerPlanRefusal is Delete's plan's refusal of a marker name that is not a regular file
// (§15.3): a link by the link row, anything else (a folder, a device, a pipe) in delmark's words.
func markerPlanRefusal(folder, name string, link bool) error {
	if link {
		return linkRefusal(filepath.Join(folder, name))
	}
	return refuse(CaseMarkerNotRegular, "%s", delmark.MsgPlanNotRegular)
}
