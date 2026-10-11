package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graph"
)

// The workspace's refusals the desktop raises (design §7.6, X2's voice: what, why, what
// to do). The engine words the ones it raises itself (R-d, R-e, R-f, R-f2, R-g, R-g2,
// R-h, R-i, R-q, R-r, R-r2, R-t, R-u..R-w2, R-z). None echoes a token, a Graph URL with
// its query or a stack trace (MA-438; X7).

// refusal is a typed refusal the page shows: its id, its words, and the status it is
// sent with.
type refusal struct {
	ID     string
	Text   string
	Status int
	Extra  map[string]any
}

func (r refusal) body() map[string]any {
	m := map[string]any{"ok": false, "id": r.ID, "error": r.Text}
	for k, v := range r.Extra {
		m[k] = v
	}
	return m
}

// statusRefused is the status a refusal is sent with: the request was understood and
// refused, which is not a crash.
const statusRefused = http.StatusUnprocessableEntity

func refusedWith(id, text string) refusal { return refusal{ID: id, Text: text, Status: statusRefused} }

func refusalA() refusal {
	return refusedWith("R-a", "Microsoft 365 isn't set up yet — the workspace needs your tenant and application ID to read your mailbox. They are saved by MailArchive's command-line tool, a separate download from MailArchive Desktop: run `mailarchive setup` there (or ask whoever installed MailArchive for you to), then come back.")
}

func refusalA2() refusal {
	return refusedWith("R-a2", "This installation can't sign in as you: it is set up for app-only access (a program signing in for itself), and the workspace reads your own mailbox with a device sign-in. Whoever manages your Microsoft 365 app registration needs to turn on 'Allow public client flows' and add the Delegated Mail.Read permission, or register a separate device app (docs/graph-app-setup.md).")
}

func refusalB() refusal {
	return refusedWith("R-b", "You're not signed in to Microsoft 365 — the workspace reads your mailbox with your own sign-in. Click Sign in and enter the code shown.")
}

func refusalC() refusal {
	return refusedWith("R-c", "Microsoft 365 refused the request (your sign-in expired or access was revoked) — nothing was read. Sign in again, then try again.")
}

// retrySignInRefusal rewords a sign-in refusal (R-b, R-c) for a retry from Export activity: the
// remedy there is the Sign in button on that page, as the account the export was made in, and
// then the button that was clicked, not the workspace's "Click Sign in and enter the code
// shown". Any other refusal passes through.
func retrySignInRefusal(ref refusal, mailbox, which string) refusal {
	as := "Click Sign in on this page"
	if mailbox != "" {
		as += " and sign in as " + mailbox
	}
	next := as + ", then click '" + app.RetryButton(which) + "' again."
	switch ref.ID {
	case "R-b":
		whose := ""
		if mailbox != "" {
			whose = " — this export is of " + mailbox + "'s mailbox"
		}
		ref.Text = "You're not signed in to Microsoft 365" + whose + ", so nothing was previewed. " + next
	case "R-c":
		ref.Text = "Microsoft 365 refused your saved sign-in (it expired or access was revoked) — nothing was previewed. " + next
	}
	return ref
}

// retryLRefusal is R-l for a retry: the owner has no selection to start again (the engine's
// §7.6 text says "Load folders again"), the export is of one mailbox and the remedy is to
// sign in as that account and click the button again.
func retryLRefusal(was, now, which string) refusal {
	return refusedWith("R-l", fmt.Sprintf("This export is of %s's mailbox, but you're signed in as %s — nothing was previewed. Click Sign in on this page. Sign in as %s, then click '%s' again.",
		was, now, was, app.RetryButton(which)))
}

func refusalJ(picks int) refusal {
	return refusedWith("R-j", fmt.Sprintf("You've picked %s individual messages — the most one selection can hold. Select the whole folder (Select current folder) and untick the ones you don't want.", commaInt(picks)))
}

func refusalJ2(what string) refusal {
	if what == "folder" {
		return refusedWith("R-j2", "A folder in this selection wasn't listed in this session — the page may be out of date. Load folders again and select again.")
	}
	return refusedWith("R-j2", "A message in this selection wasn't listed in this session — the page may be out of date. Reload the folder and select again.")
}

func refusalK() refusal {
	return refusedWith("R-k", "This preview is out of date — it is older than 30 minutes, you changed the selection or destination, or another window started a new preview. Click Preview copy export again.")
}

func refusalL(was, now string) refusal {
	return refusedWith("R-l", fmt.Sprintf("This selection was made in %s's mailbox, but you're now signed in as %s. Load folders again to start a new selection.", was, now))
}

func refusalM(started time.Time, done, total int) refusal {
	return refusedWith("R-m", fmt.Sprintf("An export is already running (started %s, %s of %s done) — only one runs at a time. Wait for it or cancel it on Export activity.",
		started.Format("15:04"), commaInt(done), commaInt(total)))
}

func refusalN() refusal {
	return refusedWith("R-n", "The folder picker isn't available on this computer — the OS folder dialog needs the `zenity` program on Linux. Type the folder path in the box instead.")
}

func refusalS() refusal {
	return refusedWith("R-s", "This mailbox has more than 10,000 folders — more than the workspace can show. Export from the archive instead, or contact support.")
}

func refusalV(folder string) refusal {
	return refusedWith("R-v", fmt.Sprintf("Microsoft 365 is limiting requests right now (throttled) — the preview couldn't list %s. Try again in a few minutes.", folder))
}

func refusalX(folder string) refusal {
	r := refusedWith("R-x", fmt.Sprintf("This list expired — reloading %s from the newest message.", folder))
	r.Extra = map[string]any{"reload": true}
	return r
}

// refusalY is what a POST after a dashboard restart gets: never a bare 403 (XC18).
func refusalY() refusal {
	return refusal{ID: "R-y", Text: "MailArchive Desktop restarted — reload the page; your selection can't be reused.", Status: http.StatusForbidden}
}

func refusalTimeout() refusal {
	return refusedWith("R-timeout", "Microsoft 365 didn't answer in time — try again in a few minutes.")
}

// commaInt formats n with thousands separators.
func commaInt(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + commaInt(-n)
	}
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// graphRefusal is the refusal for an error a Microsoft 365 call returned while the
// workspace was reading (a listing, a preview): the sign-in classes first (R-b, R-c, never
// a raw text, XC9), then the mailbox's own limits, then a plain "didn't answer". doing
// names what was being read ("Inbox"). configAuth is the saved config's sign-in mode: a
// device sign-in that an app-only registration refuses is R-a2.
func graphRefusal(err error, doing, configAuth string) refusal {
	var (
		rerr *oauth2.RetrieveError
		se   *graph.StatusError
		fc   *graph.FolderCapError
	)
	switch {
	case errors.Is(err, app.ErrNotSignedIn):
		return refusalB()
	case errors.As(err, &fc):
		return refusalS()
	case errors.As(err, &rerr):
		if strings.EqualFold(configAuth, "app") && (rerr.ErrorCode == "unauthorized_client" || rerr.ErrorCode == "invalid_client") {
			return refusalA2()
		}
		switch rerr.ErrorCode {
		case "invalid_grant", "interaction_required", "invalid_client", "unauthorized_client", "consent_required":
			return refusalC()
		}
		return refusalTimeout()
	case errors.As(err, &se):
		switch {
		case se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden:
			return refusalC()
		case se.Code == http.StatusTooManyRequests || se.Code == http.StatusServiceUnavailable:
			return refusalV(doing)
		case se.Code == http.StatusNotFound:
			return refusedWith("R-j2", "That folder is no longer in the mailbox — Load folders again and select again.")
		}
		return refusedWith("R-timeout", fmt.Sprintf("Microsoft 365 returned an error (HTTP %d) while reading %s — try again in a few minutes.", se.Code, doing))
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return refusalTimeout()
	}
	return refusalTimeout()
}
