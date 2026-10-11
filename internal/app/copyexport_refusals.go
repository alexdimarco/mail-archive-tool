package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// The refusals and stops of the copy export, worded as design §7.6 words them
// (X2's voice: what, why, remedy). None echoes a token, a Graph URL with its query
// or a stack trace (MA-438); a destination or state path they name is the user's
// own. The page's own text for the refusals only E3 raises is E3's.

// Stop classes (design §4.13 table).
const (
	stopClassSignIn      = "sign-in"
	stopClassDestination = "destination"
	stopClassState       = "state directory"
	stopClassConnection  = "connection lost"
	stopClassServer      = "Microsoft 365 having problems"
	stopClassThrottled   = "throttled"
	stopClassRedirected  = "redirected"
	stopClassSignInMoved = "sign-in cleared or changed"
	stopClassPosture     = "posture"
	stopClassInternal    = "internal error"
)

func ofTotal(done, total int) string { return commas(done) + " of " + commas(total) }

// refusalTexts builds the message of run-fatal stop id for a run that stopped after
// done of total messages. extra is the one value some texts name: the redirect's
// host, the last destination error, the new signer, the state directory.
func refusalText(id string, done, total int, extra string, plan *CopyPlan, statePath string) string {
	at := ofTotal(done, total)
	switch id {
	case "R-c":
		return fmt.Sprintf("Microsoft 365 refused the request (your sign-in expired or access was revoked) — the export stopped after %s messages. Sign in again (click Sign in on Export activity), then click 'Export the remaining messages' there.", at)
	case "R-d":
		return fmt.Sprintf("MailArchive Desktop couldn't record this export in %s (disk full or access denied) — nothing was exported. Free some space there, then click Start export again.", statePath)
	case "R-d2":
		return fmt.Sprintf("MailArchive Desktop couldn't record this export's progress in %s (disk full or access denied) — the export stopped after %s messages and the folder is marked unfinished. Free some space there, then click 'Finish as partial' or 'Export the remaining messages' on Export activity.", statePath, at)
	case "R-g":
		why := ""
		if extra != "" {
			why = " (" + extra + ")"
		}
		return fmt.Sprintf("The destination is full or no longer available%s — the export stopped after %s messages and the folder is marked unfinished. Free some space or choose another destination, then click 'Export the remaining messages' (the page lets you choose another destination before you start it).", why, at)
	case "R-u":
		return fmt.Sprintf("The connection to Microsoft 365 has been down for 5 minutes — the export stopped after %s messages. Check your network, then click 'Export the remaining messages', which also tries again the message that was downloading.", at)
	case "R-u2":
		return fmt.Sprintf("Microsoft 365 is having problems (25 messages in a row returned a server error) — the export stopped after %s messages. Try again later with 'Export the remaining messages'.", at)
	case "R-u3":
		return fmt.Sprintf("Microsoft 365 redirected a download to another server (%s). MailArchive doesn't follow redirects away from Microsoft 365, so your sign-in was never sent there — the export stopped after %s messages. If it happens again, something on your network may be redirecting Microsoft 365 traffic: check with your IT team, then click 'Export the remaining messages'.", extra, at)
	case "R-v2":
		return fmt.Sprintf("Microsoft 365 is limiting this app's requests right now (throttled) — nothing downloaded for 5 minutes, so the export stopped after %s messages. Try again in a few minutes with 'Export the remaining messages', which also tries again the message that was downloading.", at)
	case "R-w2":
		return fmt.Sprintf("Other accounts can read what you export to %s (%s) — the preview said only you could, so nothing was exported. Check the folder's permissions or choose another destination, then click Preview copy export again.", plan.Dest, extra)
	case "R-z":
		signer := plan.Source.Mailbox
		now := extra
		if now == "" {
			now = "signed out"
		}
		return fmt.Sprintf("The Microsoft 365 sign-in was cleared or changed (now: %s) — the export of %s's mailbox stopped after %s messages, so nothing is read with the wrong account. Sign in as %s (click Sign in on Export activity), then click 'Export the remaining messages' there.", now, signer, at, signer)
	case "internal":
		return fmt.Sprintf("MailArchive Desktop stopped this export after %s messages because of an internal error. The folder is marked unfinished: click 'Finish as partial' or 'Export the remaining messages' on Export activity.", at)
	}
	return fmt.Sprintf("The export stopped after %s messages (%s).", at, id)
}

// stopReason is the short reason a "Stopped: <reason>" label carries for a stop id.
func stopReason(id string) string {
	switch id {
	case "R-c":
		return "the Microsoft 365 sign-in was refused"
	case "R-d2":
		return "MailArchive Desktop couldn't record this export's progress"
	case "R-g":
		return "the destination is full or no longer available"
	case "R-u":
		return "the connection to Microsoft 365 was lost"
	case "R-u2":
		return "Microsoft 365 is having problems"
	case "R-u3":
		return "Microsoft 365 redirected a download to another server"
	case "R-v2":
		return "Microsoft 365 is limiting this app's requests"
	case "R-w2":
		return "other accounts can read this export folder"
	case "R-z":
		return "the Microsoft 365 sign-in was cleared or changed"
	case "internal":
		return "an internal error"
	}
	return "the export stopped"
}

func refuse(id, msg string) *CopyRefusal { return &CopyRefusal{ID: id, Message: msg} }

// refusalG2 is Start's free-space refusal (R-g2, XC11): the estimate plus the
// reserve does not fit.
func refusalG2(dest string, free uint64, need int64) *CopyRefusal {
	f, n := humanBytes(int64(free)), humanBytes(need)
	if f == n { // both round to the same text: show the bytes, so the sentence does not contradict itself
		f, n = commas(int(free))+" bytes", commas(int(need))+" bytes"
	}
	return refuse("R-g2", fmt.Sprintf("%s has %s free, but this export needs about %s plus room to finish (the manifest and checksums) — nothing was written. Free some space or choose another destination.",
		dest, f, n))
}

// refusalT is the refusal of a destination in which every candidate root name is taken (R-t).
func refusalT(dest, first string) *CopyRefusal {
	return refuse("R-t", fmt.Sprintf("Couldn't create a new export folder in %s (%s through -99 already exist) — choose another destination.", dest, first))
}

// refusalW is the refusal of a destination the OS will not let the export create a
// folder in (R-w, XC12).
func refusalW(dest, why string) *CopyRefusal {
	// The advice for this computer's own system comes first; the other one is kept after it.
	win := "allow MailArchive Desktop in Windows Security → Controlled folder access"
	mac := "allow it in System Settings → Privacy & Security → Files and Folders"
	var fix string
	switch runtime.GOOS {
	case "windows":
		fix = win + " (macOS: " + mac + ")"
	case "darwin":
		fix = mac + " (Windows: " + win + ")"
	default:
		fix = "check the folder's permissions (Windows: " + win + "; macOS: " + mac + ")"
	}
	return refuse("R-w", fmt.Sprintf("MailArchive couldn't create a folder in %s (%s) — choose another folder, or %s. Nothing was written.", dest, why, fix))
}

func refusalD(statePath string) *CopyRefusal {
	return refuse("R-d", refusalText("R-d", 0, 0, "", nil, statePath))
}

// postureNames words the readers a refusal names.
func postureNames(p CopyPosture) string {
	if p.Kind == postureKindUnknown {
		return "couldn't check who can read this folder"
	}
	if len(p.Readers) == 0 {
		return p.Line
	}
	return strings.Join(p.Readers, ", ")
}

// rootDisplay is the export root's name for a message.
func rootDisplay(path string) string { return filepath.Base(path) }
