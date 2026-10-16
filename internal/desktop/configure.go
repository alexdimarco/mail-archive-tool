package desktop

import (
	"errors"
	"net/http"
	"strings"

	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/util"
)

// configureM365 writes the Microsoft 365 Graph configuration from the dashboard, so a
// person can set up — or update — their tenant, application id, auth mode and (for
// app-only) the client secret without the command line (owner report 2026-10-13: the
// desktop had no way to reach `mailarchive setup`). It reuses the setup wizard's one
// audited save seam, server.SaveSetup: the non-secret config goes to the config file and
// the secret to the OS credential store, write-only — never echoed back (W-C3). d.api
// gives the endpoint POST-only + the CSRF/Origin/Host guard that keeps it to a
// same-origin request from this machine (design-local-confidentiality §8.4, X9). The
// "Connect" step is the existing sign-in / Archive now device flow; this writes the
// settings those need. A blank secret on app-only keeps the stored one, so an update
// need not retype it.
func (d *dashboard) configureM365(w http.ResponseWriter, r *http.Request) {
	var in server.SetupInput
	if !d.readBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(d.cfg.ConfigPath) == "" || d.cfg.Store == nil {
		d.refuse(w, refusedWith("R-a", "MailArchive Desktop has nowhere to save the Microsoft 365 settings on this computer. Do this: open MailArchive from its shortcut so it can find your profile folder, or reinstall it."))
		return
	}
	res, err := server.SaveSetup(d.cfg.ConfigPath, d.cfg.Store, in)
	in.Secret = "" // drop the plaintext as soon as it is handed to the seam
	if err != nil {
		// SaveSetup returns a *SetupError with a user-safe message (no secret, no stack);
		// anything else is stripped of control characters before it is shown.
		var se *server.SetupError
		if errors.As(err, &se) {
			d.reply(w, map[string]any{"ok": false, "error": se.Error()})
			return
		}
		d.reply(w, map[string]any{"ok": false, "error": "Could not save the Microsoft 365 settings: " + util.StripControl(err.Error())})
		return
	}
	// Never echo the secret — only the auth mode and whether one is configured.
	d.reply(w, map[string]any{"ok": true, "auth": res.Auth, "configured": res.Configured})
}
