package app

import (
	"io"
	"log"
	"strconv"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-187, R8
// OPEN GAP, NOT accepted, found in round 3 (R3-AF2, pre-existing, low) and
// encoded the kit's way: a t.Skip that FAILS if un-skipped today. The page
// header writes the sender as `Name <email>` (export.formatSender) and the
// rebuild reverses it by cutting at the LAST " <" (splitSender), which is not
// invertible when the address itself holds " <": net/mail decodes a quoted
// local part, so `From: Mallory <"x <ceo"@bank.example>` is the address
// `x <ceo@bank.example`. The live index stores that address; the rebuild stores
// the tail after the embedded " <", `ceo@bank.example`, with the name `Mallory
// <x` — a sender_email exactly as the mail chose it, which MA-187's "cannot
// forge or erase its own indexed sender" does not allow; and with no display
// name the live address becomes the rebuilt NAME and the rebuilt email is empty.
// Display-level only: the FTS sender column tokenizes the same either way and
// `from:` matches both columns by substring.
//
// The fix needs an owner decision, so it is NOT made here: the header text
// cannot tell the address from the name without a change to what the page
// writes (a quoted local part in the header, which splitSender would then read
// quote-aware, or the address in its own marked element; either changes the page
// format old pages do not have, and the visible header text of such an address),
// or the live index would have to take the same lossy round trip, which erases
// the sender's real address. When it is ruled and this passes, REMOVE the Skip.
func TestRebuildRecoversSenderWithAnEmbeddedAngleBracket(t *testing.T) {
	t.Skip("MA-187 open gap (R3-AF2): splitSender cuts `Name <email>` at the last ' <', so a quoted local part holding ' <' is re-split on rebuild; the fix changes the page's sender format, an owner decision (docs/review-sanitizer-roundtrip-adversarial.md)")

	out := tmpDir(t)
	date := time.Date(2025, 4, 5, 9, 0, 0, 0, time.UTC)
	senders := []struct{ subject, name, email string }{
		{"senderquoteone", "Mallory", `x <ceo@bank.example`},
		{"senderquotetwo", "", `ceo@bank.example> <mallory@evil.example`},
	}
	var msgs []rbMsg
	for i, s := range senders {
		msgs = append(msgs, rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: s.subject, SenderName: s.name, SenderEmail: s.email,
			InternetMessageID: "<sender-" + strconv.Itoa(i) + "@example.com>",
			Received:          date.Add(time.Duration(i) * time.Minute), HTMLBody: "<p>body</p>",
		}})
	}
	buildRebuildArchive(t, out, msgs)
	live := map[string][2]string{}
	for _, s := range senders {
		r := firstResult(t, out, s.subject)
		live[s.subject] = [2]string{r.SenderName, r.SenderEmail}
	}
	assure.Reached(t, live["senderquoteone"][1], "the live sender email")

	deleteSearchDB(t, out)
	if _, err := Rebuild(out, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	for _, s := range senders {
		r := firstResult(t, out, s.subject)
		if got := ([2]string{r.SenderName, r.SenderEmail}); got != live[s.subject] {
			t.Errorf("%s: sender after the rebuild = %q, live = %q: the mail chose its rebuilt sender (MA-187)", s.subject, got, live[s.subject])
		}
	}
}
