package app

import (
	"context"
	"slices"
	"testing"

	"mail-archive-tool/internal/export"
)

// covers: MA-1324, R45
// An encrypted archive's yes covers only the Gmail stores the question named (the B3 adversarial
// pass, round 2's G6): the Desktop's Run now carries the stores its question was asked for as
// GmailYesStores, which the run applies to an encrypted archive (whose key binds the answer to the
// archive, MA-1315) without an archive id; a Gmail account added to Thunderbird after the question
// was asked stays off and waits for its own answer. Positive twin first: with no yes, the run
// switches nothing.
func TestAnEncryptedArchivesYesCoversOnlyTheStoresItNamed(t *testing.T) {
	_, first := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	_, added := imapProfile(t, "imap.googlemail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	out, key := encryptedOut(t)
	run := func(opts Options) Result {
		t.Helper()
		opts.Out, opts.Key, opts.Mode = out, key, export.Incremental
		res, err := Run(context.Background(), opts, discard(), nil)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	gmailDetectOff = true // an existing archive an older MailArchive made: both stores off
	run(Options{Inputs: []string{first, added}})
	gmailDetectOff = false

	// The Run now that asked read only the first account.
	asked := run(Options{Inputs: []string{first}})
	if len(asked.Gmail.Off) != 1 || len(asked.Gmail.Switched) != 0 {
		t.Fatalf("positive twin: with no yes the run switched %v, off %v", asked.Gmail.Switched, asked.Gmail.Off)
	}
	named := asked.Gmail.Off

	// The Run now that applies the yes reads the account added since, too.
	res := run(Options{Inputs: []string{first, added}, GmailYesStores: named})
	if !slices.Equal(res.Gmail.Switched, named) {
		t.Fatalf("the yes for %v switched %v", named, res.Gmail.Switched)
	}
	if len(res.Gmail.Off) != 1 || slices.Contains(named, res.Gmail.Off[0]) {
		t.Fatalf("the account added after the question was not left off, waiting: off %v", res.Gmail.Off)
	}
	if m := loadSealed(t, out, key); !m.DedupOn(named[0]) || m.DedupOn(res.Gmail.Off[0]) {
		t.Fatal("the sealed record does not hold the named store on and the added one off")
	}
}
