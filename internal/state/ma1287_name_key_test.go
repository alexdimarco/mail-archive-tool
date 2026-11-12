package state

import (
	"strings"
	"testing"
)

// The C3 account-name pass (docs/review-thunderbird-completeness-c3-name-adversarial.md), N-02 and
// N-03, the lead's calls of 2026-11-09: a name that spells the words MailArchive says an account
// by when it has no name is no name, and the shared-name rule compares names after NFKC and
// Unicode case folding.

// covers: MA-1287
// N-02: a name equal to a fallback wording ("this account"), or starting with "the Thunderbird
// account kept in the folder" (any letter case, after cleaning and normalising), is treated as no
// name, on the run's accounts, a record read back and a history entry; so a name can never make
// one account read as another. Positive twin first: an ordinary name is kept.
func TestANameThatSpellsTheFallbackIsNoName(t *testing.T) {
	accts := []TBAccount{{Account: "imap.a.example", Name: "sam@a.example"}, {Account: "imap.b.example"}}
	TBDistinctNames(accts)
	if accts[0].Name != "sam@a.example" {
		t.Fatalf("positive twin: an ordinary name is kept: %q", accts[0].Name)
	}
	for _, name := range []string{
		"the Thunderbird account kept in the folder imap.b.example",
		"THE THUNDERBIRD ACCOUNT KEPT IN THE FOLDER imap.b.example",
		"the Thunderbird account kept in the folder imap.b.example", // a no-break space, made a space by cleaning
		"ｔｈｅ Thunderbird account kept in the folder imap.b.example", // full-width letters (NFKC)
		"the Thunderbird account kept in the folder",
		"This account",
		"this　 account​",
	} {
		accts := []TBAccount{{Account: "imap.a.example", Name: name, Verdict: "behind", OnDisk: 3, Listed: 40}, {Account: "imap.b.example", Verdict: "behind", OnDisk: 3, Listed: 40}}
		TBDistinctNames(accts)
		if accts[0].Name != "" {
			t.Errorf("%q is kept as a name", name)
		}
		if l := TBLines(accts, 0, TBSay{}); len(l) < 2 || l[0] == l[1] {
			t.Errorf("%q: two accounts read as one: %q", name, l)
		}
		// Said directly (a TBAccount no rule has passed over), the words still say the folder.
		if h := TBHeadline(TBAccount{Account: "imap.a.example", Name: name, Verdict: "behind", OnDisk: 3, Listed: 40}, TBSay{}); !strings.Contains(h, "kept in the folder imap.a.example so far") {
			t.Errorf("%q said directly: %q", name, h)
		}
		c := []TBCompact{{Account: "imap.a.example", Name: name, Verdict: "behind", X: 3, Y: 40}}
		if got := saneTBCompact(c); got[0].Name != "" {
			t.Errorf("%q kept in a history entry", name)
		}
		lr := []TBAccount{{Account: "imap.a.example", Name: name}}
		if got, _ := saneTB(lr); got[0].Name != "" {
			t.Errorf("%q kept in a record read back", name)
		}
	}
	// A name that only contains the words, not at its start, is a name the person chose.
	accts = []TBAccount{{Account: "imap.a.example", Name: "Sam (not the Thunderbird account kept in the folder x)"}}
	TBDistinctNames(accts)
	if accts[0].Name == "" {
		t.Errorf("a name holding the words later on is dropped")
	}
}

// covers: MA-1287
// N-03: two names that differ only in Unicode normalisation (NFC beside NFD, a compatibility
// form) or letter case are one name, so both accounts are said by their folders. Positive twin
// first: two different addresses are kept.
func TestNamesThatAreTheSameAfterNormalisingAreShared(t *testing.T) {
	two := func(x, y string) []TBAccount {
		a := []TBAccount{{Account: "imap.x.example", Name: x}, {Account: "imap.x.example-1", Name: y}}
		TBDistinctNames(a)
		return a
	}
	if a := two("sam@x.example", "pat@x.example"); a[0].Name == "" || a[1].Name == "" {
		t.Fatalf("positive twin: two names dropped: %+v", a)
	}
	for what, pair := range map[string][2]string{
		"NFC vs NFD":                {"josé@x.example", "josé@x.example"},
		"letter case":               {"Sam@X.example", "sam@x.EXAMPLE"},
		"a compatibility form":      {"ﬁona@x.example", "fiona@x.example"},
		"case folding beyond ASCII": {"STRASSE@x.example", "straße@x.example"},
	} {
		if a := two(pair[0], pair[1]); a[0].Name != "" || a[1].Name != "" {
			t.Errorf("%s: %q and %q kept as two names", what, a[0].Name, a[1].Name)
		}
		c := saneTBCompact([]TBCompact{{Account: "a", Name: pair[0]}, {Account: "b", Name: pair[1]}})
		if c[0].Name != "" || c[1].Name != "" {
			t.Errorf("%s: kept as two names in a history entry", what)
		}
	}
}

// covers: MA-1287
// N-03's residual (LOW, the lead's call of 2026-11-09, a documented limit): letters from another
// script that look the same (a Cyrillic а in "sаm@x.example") are not folded together; Unicode
// gives no normalisation for them, and two real Thunderbird accounts rarely differ this way. Each
// account is then said by its own name. This encoder goes red the day confusables are folded:
// remove the skip then.
func TestLookAlikeLettersFromAnotherScriptAreALimit(t *testing.T) {
	t.Skip("MA-1287 residual (documented limit, design §13 2026-11-09): confusable letters from another script are not folded")
	a := []TBAccount{{Account: "imap.x.example", Name: "sam@x.example"}, {Account: "imap.x.example-1", Name: "sаm@x.example"}}
	TBDistinctNames(a)
	if a[0].Name != "" || a[1].Name != "" {
		t.Errorf("look-alike names kept as two: %q, %q", a[0].Name, a[1].Name)
	}
}
