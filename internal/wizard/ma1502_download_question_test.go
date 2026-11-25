package wizard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// namedProfile is a profile shaped like the owner's (ownerShaped's five mail accounts and Local
// Folders) with one more IMAP account named "Work: Gmail", whose folder name cannot hold its ":", and
// a folder no account uses now; every store holds mail. It returns the stores in discovery's order.
func namedProfile(t *testing.T) []string {
	t.Helper()
	var lines []string
	for _, l := range ownerShaped(true) {
		if strings.Contains(l, "mail.accountmanager.accounts") {
			l = `user_pref("mail.accountmanager.accounts", "account1,account2,account3,account4,account5,account6,account7");`
		}
		lines = append(lines, l)
	}
	lines = append(lines, pickServer("7", "imap", "imap.work.example", "ImapMail/imap.work.example", "Work: Gmail")...)
	stores := append(append([]pickStore(nil), ownerStores...),
		pickStore{"ImapMail/imap.old.example", 2_000}, pickStore{"ImapMail/imap.work.example", 3_000})
	p := pickProfile(t, t.TempDir(), "a1b2c3d4.default-release", lines, stores...)
	return storesOf(p, stores)
}

// accountLines are the questions' account lines ("Account 1 of 3: NAME"), each with its offer's name.
func accountLines(t *testing.T, questions []string) []string {
	t.Helper()
	var out []string
	for _, q := range questions {
		head, _, _ := strings.Cut(q, "\n\n")
		_, name, ok := strings.Cut(head, ": ")
		// "Account 1 of 3: NAME", or with one account asked "Account: NAME" (F14).
		if !ok || !strings.HasPrefix(head, "Account ") && !strings.HasPrefix(head, "Account: ") {
			t.Errorf("the question does not start with its account line:\n%s", q)
		}
		if !strings.HasSuffix(q, "\n\nPrepare a full offline download of "+name+" first?") {
			t.Errorf("the offer does not name %q:\n%s", name, q)
		}
		out = append(out, head)
	}
	return out
}

// covers: MA-1502, MA-947, MA-1267
// The download question names the account and its number (docs/design-per-mailbox-archives.md §8,
// the owner's QA "Yes, name it (Recommended)" of 2026-11-23): "Account 1 of 3: NAME", today's
// explanation unchanged, "Prepare a full offline download of NAME first?", with the buttons
// unchanged. The name is the one the list showed (an account named "Work: Gmail" is asked as that,
// though its folder is "Work Gmail"); the count counts the accounts asked this run. Positive twin
// first, the one-archive path: All of them asks each live IMAP account in the order found, never the
// folder no account uses nor Local Folders. A folder chosen by hand (no list) is named as the list
// would name it alone, "Account 1 of 1". A set asks, in the plan's order, each account that gets a
// new archive, and the plan's last line names the same accounts in the same words; on its re-run the
// archives brought up to date are not asked again and the count drops; a row saved once is never
// asked. Every answer here is "No, export what's cached", so nothing is prepared and nothing written.
func TestTheDownloadQuestionNamesTheAccountAndItsNumber(t *testing.T) {
	ownSettings(t)
	found := namedProfile(t)
	pick := &pickScript{answers: []string{allOfThem}}
	pick.install(t)
	p, err := pickAutoChoice(found, false)
	if err != nil || !p.auto || !reflect.DeepEqual(p.inputs, found) {
		t.Fatalf("All of them: %+v %v", p, err)
	}

	// The one-archive path.
	// Amended 2026-11-25 (the friction review's F14; nothing weakened): All of them's questions come
	// in the order the list showed the accounts (the flow asks askDownloads(inListOrder(choice.inputs,
	// order), names)), its inputs still in the order found; they came in the order found before
	// ("Account 1 of 6: qzsam.walker@gmail.com", "2: qzsam.work@gmail.com", "3: qzwork@qzwork.example",
	// "4: qzsam@qzname.example", "5: qz.student@mail.qzuni.example", "6: Work: Gmail"). One account
	// asked has no count: "Account: NAME" ("Account 1 of 1: NAME" before).
	s := &setScript{}
	s.install(t)
	if !reflect.DeepEqual(p.inputs, found) {
		t.Fatalf("All of them's inputs are no longer in the order found: %q", p.inputs)
	}
	prepared, err := askDownloads(inListOrder(p.inputs, p.order), p.names)
	if err != nil || prepared {
		t.Fatalf("No to each: prepared %v, %v", prepared, err)
	}
	want := []string{
		"Account 1 of 6: qz.student@mail.qzuni.example",
		"Account 2 of 6: qzsam.walker@gmail.com",
		"Account 3 of 6: qzsam.work@gmail.com",
		"Account 4 of 6: qzsam@qzname.example",
		"Account 5 of 6: qzwork@qzwork.example",
		"Account 6 of 6: Work: Gmail",
	}
	if src, err := os.ReadFile("wizard.go"); err != nil || !strings.Contains(string(src), "prepared, err := askDownloads(inListOrder(choice.inputs, order), names)") {
		t.Errorf("the flow no longer asks in the list's order: %v", err)
	}
	if got := accountLines(t, assure.Reached(t, s.questions, "the download questions")); !reflect.DeepEqual(got, want) {
		t.Fatalf("the one-archive path asks:\n%q\nwant\n%q", got, want)
	}
	for _, q := range s.questions {
		head, rest, _ := strings.Cut(q, "\n\n")
		if rest != strings.TrimSuffix(imapPrepQuestion, imapPrepOffer)+"Prepare a full offline download of "+strings.SplitN(head, ": ", 2)[1]+" first?" {
			t.Errorf("today's explanation is not there unchanged:\n%s", q)
		}
	}
	if len(s.notes) != 0 || len(s.lists) != 0 || len(s.chooser) != 0 {
		t.Errorf("No asks nothing more: notes %q, lists %d, chooser %d", s.notes, len(s.lists), len(s.chooser))
	}

	// A folder chosen by hand: no list was shown, so it is named as the list would name it alone.
	s = &setScript{}
	s.install(t)
	if _, err := askDownloads([]string{found[0]}, nil); err != nil {
		t.Fatal(err)
	}
	if got := accountLines(t, s.questions); !reflect.DeepEqual(got, []string{"Account: qzsam.walker@gmail.com"}) {
		t.Errorf("a folder chosen by hand: %q", got)
	}

	// One archive for each mailbox: the plan's order (the list's), only new archives' accounts.
	rows := setRows(listRows(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), "darwin"), "darwin")
	parent := t.TempDir()
	plan, err := planSet(parent, rows, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	s = &setScript{}
	s.install(t)
	var changed []string
	got, err := askSetDownloads(plan, &changed)
	if err != nil || len(got) != 6 || len(changed) != 0 {
		t.Fatalf("a set's questions: %v %v, changed %q", got, err, changed)
	}
	setWant := []string{
		"Account 1 of 6: qz.student@mail.qzuni.example",
		"Account 2 of 6: qzsam.walker@gmail.com",
		"Account 3 of 6: qzsam.work@gmail.com",
		"Account 4 of 6: qzsam@qzname.example",
		"Account 5 of 6: qzwork@qzwork.example",
		"Account 6 of 6: Work: Gmail",
	}
	if lines := accountLines(t, s.questions); !reflect.DeepEqual(lines, setWant) {
		t.Fatalf("a set asks:\n%q\nwant\n%q", lines, setWant)
	}
	text := setPlanText(plan)
	// (F11: "\nAsked about a full offline download: …" before.)
	if !strings.HasSuffix(text, "\nNext you are asked whether to prepare a full offline download of: qz.student@mail.qzuni.example, qzsam.walker@gmail.com, qzsam.work@gmail.com, qzsam@qzname.example, qzwork@qzwork.example, Work: Gmail") {
		t.Errorf("the plan's last line does not name the accounts asked, as asked:\n%s", text)
	}
	if !strings.Contains(text, "\nWork Gmail\n") || !strings.Contains(text, "imap.old.example (not in Thunderbird now): saved once and not kept current, since no account uses it") {
		t.Errorf("the plan's folders: %s", text)
	}

	// The re-run: two archives brought up to date are not asked again; the count drops to 4.
	byName := map[string]setRow{}
	for _, r := range rows {
		byName[r.name] = r
	}
	archiveHolding(t, filepath.Join(parent, "qzsam.walker@gmail.com"), idOf(byName["qzsam.walker@gmail.com"]))
	archiveHolding(t, filepath.Join(parent, "Work Gmail"), idOf(byName["Work Gmail"]))
	again, err := planSet(parent, rows, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	s = &setScript{}
	s.install(t)
	if _, err := askSetDownloads(again, &changed); err != nil {
		t.Fatal(err)
	}
	reWant := []string{
		"Account 1 of 4: qz.student@mail.qzuni.example",
		"Account 2 of 4: qzsam.work@gmail.com",
		"Account 3 of 4: qzsam@qzname.example",
		"Account 4 of 4: qzwork@qzwork.example",
	}
	if lines := accountLines(t, s.questions); !reflect.DeepEqual(lines, reWant) {
		t.Fatalf("the re-run asks:\n%q\nwant\n%q", lines, reWant)
	}
	for _, q := range s.questions {
		if strings.Contains(q, "imap.old.example") || strings.Contains(q, "Local Folders") {
			t.Errorf("a row saved once, or Local Folders, was asked: %s", q)
		}
	}
	if !strings.HasSuffix(setPlanText(again), "\nNext you are asked whether to prepare a full offline download of: qz.student@mail.qzuni.example, qzsam.work@gmail.com, qzsam@qzname.example, qzwork@qzwork.example") {
		t.Errorf("the re-run's plan line:\n%s", setPlanText(again))
	}

	// Control characters in a name never reach the question.
	if q := imapPrepQuestionFor(prepAsk{name: "a\x1b[2J@x\x07", i: 1, n: 1}); strings.ContainsAny(q, "\x1b\x07") {
		t.Errorf("control characters reach the question: %q", q)
	}
}
