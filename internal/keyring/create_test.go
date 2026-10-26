package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// covers: MA-779, R37
func TestANewArchiveIsCommittedThenRecordedThenRemembered(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out := filepath.Join(t.TempDir(), "archive")
	p, err := Create(out, testPassword, CreateOptions{Remember: true, Store: s})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmtRedacted(p), "redacted") || strings.Contains(fmtRedacted(p), p.RecoveryKey()) {
		t.Fatalf("a pending create must print redacted: %s", fmtRedacted(p))
	}
	// Nothing is written or recorded before Commit.
	if exists(filepath.Join(out, vault.FileName)) || recordedID(t, out) != "" {
		t.Fatal("Create wrote the key file or recorded the folder before Commit")
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	id := u.ArchiveID()
	assure.Reached(t, exists(filepath.Join(out, vault.FileName)) && exists(filepath.Join(out, vault.BackupName)), "both key files")
	if got := recordedID(t, out); got != id.String() {
		t.Fatalf("the folder is recorded under %q, want %s", got, id)
	}
	if _, err := s.Get(vault.Account(id)); err != nil {
		t.Fatalf("the key was not remembered: %v", err)
	}
	if u.How() != ByCreate {
		t.Fatalf("How is %v", u.How())
	}
	// The recovery key shown opens it.
	v, err := Open(out, RecoveryKey(p.RecoveryKey()), OpenOptions{})
	if err != nil {
		t.Fatalf("the recovery key shown does not open the archive: %v", err)
	}
	v.Destroy()
}

func fmtRedacted(p *PendingCreate) string {
	var b strings.Builder
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		b.WriteString(fmt.Sprintf(verb, p))
	}
	return b.String()
}

// covers: MA-779, R37
func TestADiscardedCreateLeavesTheFolderAsItWas(t *testing.T) {
	freshRecord(t)
	out := filepath.Join(t.TempDir(), "archive")
	p, err := Create(out, testPassword, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Discard()
	if _, err := p.Commit(); !errors.Is(err, vault.ErrPendingUsed) {
		t.Fatalf("a discarded create committed: %v", err)
	}
	entries, _ := os.ReadDir(out)
	assure.Reached(t, len(entries) == 0 && recordedID(t, out) == "", "an empty, unrecorded folder after Discard")
}

// covers: MA-779, R37
func TestCreateRefusesItsFoldersBeforeAnyDerivation(t *testing.T) {
	recDir := freshRecord(t)
	creates := countCreates(t)
	settings, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _, _ := made(t, nil)
	recordedEmpty := filepath.Join(t.TempDir(), "was-an-archive")
	change(t, func(r *encrecord.Record) error { return r.Add(strings.Repeat("cd", 16), recordedEmpty, false, now()) })

	// Positive twin: a folder holding only operating-system litter is empty.
	litter := filepath.Join(t.TempDir(), "litter")
	if err := os.MkdirAll(litter, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(litter, ".DS_Store"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	*creates = 0
	p, err := Create(litter, testPassword, CreateOptions{})
	if err != nil {
		t.Fatalf("positive twin: a folder holding only litter: %v", err)
	}
	p.Discard()
	assure.Reached(t, *creates, "the positive twin's derivation")

	notEmpty := filepath.Join(t.TempDir(), "mail")
	if err := os.MkdirAll(notEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notEmpty, "index.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	unfinished := filepath.Join(t.TempDir(), "unfinished")
	if err := os.MkdirAll(unfinished, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unfinished, vault.FileName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		out   string
		names []string
	}{
		"a folder holding files":               {notEmpty, []string{notEmpty, "index.html", "new or empty"}},
		"a folder holding a key file":          {unfinished, []string{unfinished, "without -encrypt", "finish"}},
		"inside an encrypted archive":          {filepath.Join(encrypted, "sub"), []string{encrypted}},
		"a recorded folder":                    {recordedEmpty, []string{recordedEmpty, "used to hold an encrypted archive"}},
		"inside MailArchive's settings folder": {filepath.Join(settings, "archive"), []string{settings, "settings"}},
	}
	for name, c := range cases {
		*creates = 0
		before := loadRecord(t).Seq()
		_, err := Create(c.out, testPassword, CreateOptions{})
		code, msg := refusal(err)
		assure.Refused(t, code, msg, append([]assure.RefuseOption{assure.Code(1), assure.Names(c.names...)},
			assure.NoSideEffect(func() bool {
				return *creates == 0 && loadRecord(t).Seq() == before && !exists(filepath.Join(c.out, vault.BackupName))
			}))...)
		_ = name
	}

	// A weak password is refused by the vault before it derives (C12).
	_, err = Create(filepath.Join(t.TempDir(), "weak"), "short", CreateOptions{})
	if !errors.Is(err, vault.ErrWeakPassword) {
		t.Fatalf("a short password: %v", err)
	}

	// While the record cannot be used, nothing is created (§4.3).
	damageRecord(t, recDir)
	*creates = 0
	out := filepath.Join(t.TempDir(), "new")
	_, err = Create(out, testPassword, CreateOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "record of encrypted archives"),
		assure.NoSideEffect(func() bool { return *creates == 0 && !exists(out) }))
	if !errors.Is(err, encmark.ErrRecordUnusable) {
		t.Fatalf("want ErrRecordUnusable, got %v", err)
	}
}

// covers: MA-779, MA-790, R37
// The record busy at Commit (another MailArchive holds its lock): the key file is
// written and stays — every keyless writer refuses the folder by it — and the error
// is ErrCreatedNotRecorded, telling once, in the record's own words, to finish
// setting up when it is free (the P3-1a pass, FID-12, CRIT-04). The record unusable
// by Commit refuses before anything is written (OUT-03's re-check).
func TestACreateWhoseRecordCannotBeWrittenKeepsItsKeyFileAndSaysToFinish(t *testing.T) {
	recDir := freshRecord(t)
	out := filepath.Join(t.TempDir(), "archive")
	p, err := Create(out, testPassword, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.AcquireAs(filepath.Join(recDir, encrecord.LockName), "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Commit()
	held.Release()
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "busy updating its record", "finish setting up"),
		assure.Forbid("archive is in use"))
	if !errors.Is(err, ErrCreatedNotRecorded) || strings.Count(msg, "busy updating") != 1 {
		t.Fatalf("want ErrCreatedNotRecorded told once, got %v", err)
	}
	if !exists(filepath.Join(out, vault.FileName)) {
		t.Fatal("the key file must stay: every keyless writer refuses the folder by it")
	}
	res, _ := encmark.At(out)
	assure.Reached(t, res.Encrypted, "the folder refused to keyless writers by its own key file")

	out2 := filepath.Join(t.TempDir(), "archive")
	p, err = Create(out2, testPassword, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	damageRecord(t, recDir)
	_, err = p.Commit()
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out2, "record of encrypted archives"),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out2, vault.FileName)) }))
}
