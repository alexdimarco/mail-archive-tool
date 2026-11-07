package tbengine

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1132, R40
// An elevated MailArchive (administrator on Windows, root on macOS and Linux; the probe of
// the process's token is the seam) is refused with W-ELEVATED and nothing written, at the
// plan and again at Start (a plan made unelevated, started elevated). Positive twin first.
func TestAnElevatedMailArchiveIsRefused(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, p.Messages, "positive twin: not elevated, it plans the archive's messages")
	defer func() { elevatedProbe = func() bool { return false } }()
	elevatedProbe = func() bool { return true }
	before := snapshot(t, pr.dir, out)
	_, err := MakePlan(Request{Out: out, Profile: pr.choice()})
	words := "running as root"
	if runtime.GOOS == "windows" {
		words = "running as administrator"
	}
	refusedAs(t, err, "W-ELEVATED", func() bool { return snapshot(t, pr.dir, out) == before }, words, "could not be changed by Thunderbird")
	_, err = Run(context.Background(), p, Start{})
	refusedAs(t, err, "W-ELEVATED", func() bool { return snapshot(t, pr.dir, out) == before }, words)
}

// covers: MA-1143, R40
// W-STORE (a Thunderbird from the Microsoft Store may keep its mail elsewhere, §3.1 RT-11)
// is the plan's first line — after W-RECAPTURE only, which leads every plan it is in
// (RT-10) — and the Done words say it again, so a restore never claims success to a
// Thunderbird that cannot see it. Positive twin: without it, neither says it. (The
// detection of a Store package is MA-1143's Windows leg, internal/tbrestore.)
func TestWStoreLeadsThePlanAndIsRepeatedWhenDone(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	res := mustRun(t, p, Start{})
	assure.Reached(t, !strings.Contains(codesOf(p), "W-STORE") && !strings.Contains(res.Words, "Microsoft Store"), fmt.Sprintf("positive twin: no Store Thunderbird, no W-STORE"))

	defer func() { storeWarning = func() string { return "" } }()
	storeWarning = tbrestoreStoreWords
	pr = newProfile(t, "")
	p = plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, p.Codes()[0] == "W-STORE", fmt.Sprintf("W-STORE is the plan's first line: %s", codesOf(p)))
	res = mustRun(t, p, Start{})
	assure.Reached(t, res.Done() && strings.HasSuffix(res.Words, tbrestoreStoreWords()), fmt.Sprintf("the Done words repeat W-STORE: %s", res.Words))

	pr = newProfile(t, "")
	p = plan(t, Request{Out: out, Profile: pr.choice(), Inputs: []string{pr.dir}})
	assure.Reached(t, strings.Join(p.Codes()[:2], " ") == "W-RECAPTURE W-STORE", fmt.Sprintf("with W-RECAPTURE, W-STORE comes right after it: %s", codesOf(p)))
}

// tbrestoreStoreWords are W-STORE's words as tbrestore has them (the one source).
func tbrestoreStoreWords() string {
	return "If you use Thunderbird from the Microsoft Store, this restore may not show in it: that version may keep its mail in another place. If you installed Thunderbird from thunderbird.net, ignore this."
}
