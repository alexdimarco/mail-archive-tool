package wizard

import (
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1490
// The question after All of them (§3): asked only after All of them, only in the plaintext flow
// (pickAutoChoice's offerEach, which the flow passes as keyed == nil), only when two or more rows
// get an archive by §5's rule. Positive twin first: One archive for all of them gives exactly what
// All of them gives today (every path found, auto-discovery on). One archive for each mailbox gives
// the set's rows in the list's order. Not asked: in the encrypted flow, after one row, when only one
// row gets an archive (an account and an empty feeds folder). Its words are the design's, with One
// archive first; through the listDialog seam with One archive preselected and an empty pick refused.
// An empty answer, and any text that is not a row, show MA-1465's note and the question again; ten
// end with errNoEachPick; Cancel ends the flow. All through recorders for the dialogs: no real one.
func TestTheQuestionAfterAllOfThemIsAskedOnlyWhereItApplies(t *testing.T) {
	named := pickProfile(t, t.TempDir(), "a1b2c3d4.default-release", ownerShaped(true), ownerStores...)
	found := storesOf(named, ownerStores)

	s := &pickScript{answers: []string{allOfThem, eachOne}}
	s.install(t)
	p, err := pickAutoChoice(found, true)
	if err != nil || !p.auto || p.each || !reflect.DeepEqual(p.inputs, found) {
		t.Fatalf("positive twin: One archive for all of them gave %q auto=%v each=%v %v, want every store found, auto on", p.inputs, p.auto, p.each, err)
	}
	if len(s.lists) != 2 || s.lists[1].text != eachQuestion || !reflect.DeepEqual(s.lists[1].items, []string{eachOne, eachMany}) {
		t.Fatalf("the question shown: %#v", s.lists)
	}
	for _, words := range []string{
		"Keep these mailboxes together in one archive, or give each one its own archive?",
		"One archive: one place to search all your mail. A mailbox you add to Thunderbird later joins it at its next backup.",
		"One archive for each mailbox: each can be searched, paused or deleted on its own, and a problem with one does not stop the others. A mailbox you add to Thunderbird later is not backed up until you run this again, choose One archive for each mailbox, and choose the same folder.",
	} {
		if !strings.Contains(eachQuestion, words) {
			t.Errorf("the question lacks the design's words %q", words)
		}
	}
	if strings.ContainsAny(eachQuestion, `&<>\`) {
		t.Errorf("the question holds a character zenity reads as markup on Linux: %q", eachQuestion)
	}

	s = &pickScript{answers: []string{allOfThem, eachMany}}
	s.install(t)
	p, err = pickAutoChoice(found, true)
	if err != nil || !p.each || p.inputs != nil || p.auto {
		t.Fatalf("One archive for each mailbox: %+v %v", p, err)
	}
	labels, paths := labelsOf(t, found)
	if got := pathsOf(assure.Reached(t, p.set, "the set's rows")); !reflect.DeepEqual(got, paths) || setMakes(p.set) != 6 {
		t.Fatalf("the set's rows are the list's, in its order: %q, want %q (labels %q)", got, paths, labels)
	}

	// Not asked: the encrypted flow; one row picked; only one row that gets an archive.
	s = &pickScript{answers: []string{allOfThem}}
	s.install(t)
	if p, err := pickAutoChoice(found, false); err != nil || !p.auto || p.each || len(s.lists) != 1 {
		t.Errorf("the encrypted flow asks nothing after All of them: %+v %v, lists %d", p, err, len(s.lists))
	}
	ls, _ := pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), runtime.GOOS)
	s = &pickScript{answers: []string{ls[1]}}
	s.install(t)
	if p, err := pickAutoChoice(found, true); err != nil || p.auto || p.each || len(p.inputs) != 1 || len(s.lists) != 1 {
		t.Errorf("one row asks nothing: %+v %v, lists %d", p, err, len(s.lists))
	}
	feeds := append([]string{`user_pref("mail.accountmanager.accounts", "account1,account2");`},
		append(pickServer("1", "imap", "imap.one.example", "ImapMail/imap.one.example", "pat@one.example"),
			pickServer("2", "rss", "Feeds", "Mail/Feeds", "Blogs and News Feeds")...)...)
	oneAndFeeds := []pickStore{{"ImapMail/imap.one.example", 5000}, {"Mail/Feeds", 0}}
	pf := pickProfile(t, t.TempDir(), "f.default", feeds, oneAndFeeds...)
	two := storesOf(pf, oneAndFeeds) // the feeds folder's INBOX is empty: counted in full, size 0
	s = &pickScript{answers: []string{allOfThem}}
	s.install(t)
	if p, err := pickAutoChoice(two, true); err != nil || !p.auto || p.each || !reflect.DeepEqual(p.inputs, two) || len(s.lists) != 1 {
		t.Errorf("one account and an empty feeds folder stay one archive, unasked: %+v %v, lists %d", p, err, len(s.lists))
	}

	// An empty answer and a text that is not a row: the note, then the question again.
	for _, goos := range []string{"darwin", "windows", "linux"} {
		for _, odd := range []string{"", "One archive", "/etc/passwd"} {
			s = &pickScript{answers: []string{odd, eachMany}}
			s.install(t)
			if each, err := askEach(goos); err != nil || !each || len(s.notes) != 1 || s.notes[0] != pickAgain || len(s.lists) != 2 {
				t.Errorf("%s: %q must show the note and ask again: %v %v, notes %q", goos, odd, each, err, s.notes)
			}
		}
	}
	// Ten answers that name no row end the flow, nothing chosen.
	s = &pickScript{}
	s.install(t)
	if each, err := askEach("linux"); each || !errors.Is(err, errNoEachPick) || len(s.lists) != pickTries || len(s.notes) != pickTries-1 {
		t.Errorf("ten empty answers: %v %v, lists %d, notes %d", each, err, len(s.lists), len(s.notes))
	}
	// Cancel ends the flow as Cancel on the list does.
	s = &pickScript{answers: []string{allOfThem}, endErr: zenity.ErrCanceled}
	s.install(t)
	if p, err := pickAutoChoice(found, true); !errors.Is(err, zenity.ErrCanceled) || p.each || p.inputs != nil {
		t.Errorf("Cancel on the question: %+v %v", p, err)
	}

	// The wiring: the question's call through the seam, One archive preselected and an empty pick
	// refused; the flow passes keyed == nil (and nothing until step 3b is built); the Outlook-app
	// route is taken before the list.
	for file, wired := range map[string][]string{
		"set.go": {"listDialog(eachQuestion, rows, zenity.Title(appTitle), zenity.DefaultItems(eachOne), zenity.DisallowEmpty())"},
		"wizard.go": {
			"pickAutoChoice(autoInputs, perMailboxBuilt && keyed == nil)",
			"\tcase useOutlookCOM:\n\tcase srcType == srcAuto:",
		},
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wired {
			if !strings.Contains(string(src), w) {
				t.Errorf("%s no longer holds %q", file, w)
			}
		}
	}
}
