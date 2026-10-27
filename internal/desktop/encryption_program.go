package desktop

import (
	"io"
	"log"
	"net/http"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/keyring"
)

// The Encryption card's switch for keeping copies of MailArchive in the archive (program
// design §6, C16): an immediate keyed commit under the archive lock, as the pause note
// is, refused with its reason when the archive is locked, its drive is missing or a run
// is under way. Nothing in program/ changes until the next backup, which the card says.

// programCopiesAction turns keeping the copies on or off for the archive open here.
func (d *dashboard) programCopiesAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	out, e := d.openHere(w)
	if e == nil {
		return
	}
	// A run under way — this Desktop's own or a scheduled one — holds the archive lock,
	// which the commit takes: it is refused there, nothing sealed.
	lease, err := e.u.LeaseForWrite(keyring.OpenOptions{Store: d.cfg.Store})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer lease.Destroy()
	if err := keyring.SetProgramCopies(d.life.Context(), lease, in.On, log.New(io.Discard, "", 0)); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "Nothing was changed: " + jobErrorText(out, err)})
		return
	}
	line := "Copies of MailArchive are written into this archive at its next backup."
	if !in.On {
		line = "Copies of MailArchive are no longer kept: any in this archive are removed at its next backup."
	}
	writeJSON(w, map[string]any{"ok": true, "lines": []string{line}})
}

// programCardLine is what the card says of the copies the sealed set keeps, whether
// keeping them is on, and whether the switch is offered: not for a set a newer
// MailArchive wrote, which is kept as it is (R6).
func programCardLine(ps *container.ProgramSet, st container.ProgramStatus) (line string, on, offer bool) {
	switch st {
	case container.ProgramUnknown, container.ProgramInvalid:
		return "The copies of MailArchive in this archive were set up by a newer MailArchive: they are kept as they are.", false, false
	case container.ProgramAbsent:
		return "No copies of MailArchive are kept in this archive.", false, true
	}
	on = ps.Keep != nil && *ps.Keep
	landed := 0
	for _, f := range ps.Files {
		if f.Platform != container.PlatformChecksums && f.MissingRuns == 0 {
			landed++
		}
	}
	switch {
	case on && landed == 0:
		line = "Copies of MailArchive are written into this archive at its next backup."
	case on:
		line = "This archive keeps copies of MailArchive, so it can be opened years from now."
	case landed > 0:
		line = "Copies of MailArchive are no longer kept: the ones in this archive are removed at its next backup."
	default:
		line = "No copies of MailArchive are kept in this archive."
	}
	return line, on, true
}
