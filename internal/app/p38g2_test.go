package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The P3-8 friction review's fixes to the engine and status surfaces (group G2).

// covers: MA-851, R37
// A backup says nothing of the files every save leaves for the next one to remove
// (superseded during the last run): a routine second backup prints no sweep line. A
// file an interrupted backup left is still removed and told, in plain words (P3-8
// friction, B18).
func TestARoutineBackupSaysNothingOfItsSweep(t *testing.T) {
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	_, routine := backup(t, out, key, ProgramCopiesUnset)

	orphan := filepath.Join(out, export.UnitsDir, "ab", "ab0123456789abcdef0123456789abcd")
	os.MkdirAll(filepath.Dir(orphan), 0o755)
	os.WriteFile(orphan, []byte("an interrupted backup's revision"), 0o644)
	crashed := time.Now()
	os.Chtimes(orphan, crashed, crashed)
	time.Sleep(1100 * time.Millisecond) // the next backup starts after the orphan was written
	_, told := backup(t, out, key, ProgramCopiesUnset)
	_, gone := os.Stat(orphan)

	assure.Reached(t, !strings.Contains(routine, "removed") && !strings.Contains(routine, "swept") &&
		strings.Contains(told, "removed 1 file(s) an interrupted backup left in the archive") && os.IsNotExist(gone),
		"routine:\n"+routine+"\ntold:\n"+told)
}

// damagedSealed is a keyed side over a real archive whose one message's page does not
// open, and that names that message by its date, sender and subject.
type damagedSealed struct {
	fakeSealed
	units *export.Units
	bad   string // the damaged record's unit
	name  MessageName
	named bool // NameMessage was asked
}

func (d *damagedSealed) Unit(_ context.Context, rec state.Record, kind vault.UnitKind, s state.SealedUnit) (string, error) {
	rel, err := d.units.Path(rec.Unit, kind, s.Rev)
	if err != nil {
		return rel, err
	}
	if rec.Unit == d.bad && kind == vault.UnitMessage {
		return rel, fmt.Errorf("%w: %s", ErrUnitDoesNotOpen, rel)
	}
	return rel, nil
}

func (d *damagedSealed) NameMessage(_ context.Context, rec state.Record) (MessageName, bool) {
	d.named = true
	if rec.Unit != d.bad {
		return MessageName{}, false
	}
	return d.name, true
}

// damageOneMessage backs up into a fresh encrypted archive and flips a byte of one
// message's sealed page; it returns the archive, its key, the record's manifest and
// the damaged unit.
func damageOneMessage(t *testing.T) (string, *vault.Key, *state.Manifest, string) {
	t.Helper()
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	m := loadSealed(t, out, key)
	units := &export.Units{Root: out, Key: key}
	for _, r := range m.All() {
		if r.Sealed == nil || r.Sealed.HTML == nil {
			continue
		}
		rel, err := units.Path(r.Unit, vault.UnitMessage, r.Sealed.HTML.Rev)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(out, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 0xff
		os.WriteFile(p, b, 0o600)
		return out, key, m, r.Unit
	}
	t.Fatal("no sealed page to damage")
	return "", nil, nil, ""
}

// covers: MA-851, R37
// verify -full names a damaged message as the person knows it — its date, sender and
// subject — beside its sealed file; says the remedy once; and offers the cheaper repair
// first: a backup with -mode full from the original mail (P3-8 friction, B19). The
// positive twin: a side that cannot name it still names it by what its record holds.
func TestADamagedMessageIsNamedAndItsRepairSaidOnce(t *testing.T) {
	out, key, m, bad := damageOneMessage(t)
	src := &damagedSealed{fakeSealed: fakeSealed{m: m, listOK: true}, units: &export.Units{Root: out, Key: key}, bad: bad,
		name: MessageName{Date: "2026-03-14 09:30 UTC", From: "Ada Lovelace <ada@example.com>", Subject: "Quarterly figures"}}
	rep, err := VerifySealed(out, VerifyOptions{}, discard(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Join(VerifySummary(rep), "\n")

	// The twin: a side with no names of its own.
	plain := &fakeSealed{m: m, listOK: true, unitErr: fmt.Errorf("%w: x", ErrUnitDoesNotOpen)}
	rep2, err := VerifySealed(out, VerifyOptions{}, discard(), nil, plain)
	if err != nil {
		t.Fatal(err)
	}
	twin := strings.Join(VerifySummary(rep2), "\n")

	assure.Reached(t, rep.Outcome == state.OutcomeDamaged && src.named &&
		strings.Contains(summary, "dated 2026-03-14 09:30 UTC") && strings.Contains(summary, "from Ada Lovelace <ada@example.com>") &&
		strings.Contains(summary, "“Quarterly figures”") && strings.Count(summary, "restore the whole archive folder") == 1 &&
		strings.Contains(summary, "-mode full") && strings.Contains(summary, "mailarchive verify -full -out "+encmark.CommandPath(out)) && // as F2 prints it, per system
		strings.Contains(twin, "the message dated ") && strings.Contains(twin, ", in "),
		"named:\n"+summary+"\n\ntwin:\n"+twin)
}

// covers: MA-851, R37
// One file is one problem whatever its separator: the keyed side's backslashed path
// (Windows) and the keyless check's forward-slashed one are de-duplicated (FID-04; P3-8
// friction, the Windows lab). The positive twin: another file is a second problem.
func TestAFileNamedWithEitherSeparatorIsListedOnce(t *testing.T) {
	rep := &Report{Problems: []Problem{{Path: "units/2a/2a0123", Kind: "modified"}}}
	f := &fullCheck{rep: rep, shown: 1}
	f.problem(`units\2a\2a0123`, "unreadable", "a message")
	once := len(rep.Problems) == 1 && rep.Problems[0].Kind == "unreadable"
	f.problem(`units\2b\2b0123`, "unreadable", "a message")
	assure.Reached(t, once && len(rep.Problems) == 2 && rep.Problems[1].Path == "units/2b/2b0123", fmt.Sprintf("%+v", rep.Problems))
}

// covers: MA-851, R37
// An incremental backup into an archive whose last check found damaged messages says
// it repaired nothing and names the repair; a full one says it wrote every message
// again and names the check that confirms it (P3-8 friction, B19). The positive twin:
// with no such check, a backup says neither.
func TestABackupIntoADamagedArchiveSaysSo(t *testing.T) {
	out, key := encryptedOut(t)
	src := encryptedSourcePST(t)
	clean := logOf(t, Options{Inputs: []string{src}, Out: out, Key: key})
	when := time.Now().UTC()
	if err := state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Encrypted: true, Tier: state.TierFull, Outcome: state.OutcomeDamaged}); err != nil {
		t.Fatal(err)
	}
	incremental := logOf(t, Options{Inputs: []string{src}, Out: out, Key: key})
	full := logOf(t, Options{Inputs: []string{src}, Out: out, Key: key, Mode: export.Full})
	assure.Reached(t, !strings.Contains(clean, "found messages missing or damaged") &&
		strings.Contains(incremental, "did not repair them") && strings.Contains(incremental, "-mode full") &&
		strings.Contains(full, "wrote every message again") && strings.Contains(full, "verify -full"),
		"clean:\n"+clean+"\nincremental:\n"+incremental+"\nfull:\n"+full)
}

// covers: MA-851, R37
// A cancelled backup is told as cancelled, in status and in the Desktop's health
// (which reads the same lines) — never "the backup failed" (X10; P3-8 friction, B20);
// its time names its zone, as the UTC verify times beside it do; and the line its
// record carries from the last backup that ran the program step ("copies … were
// updated") is not shown as this run's doing (P3-8 friction, low list). The positive
// twin: a finished backup's lines say nothing of a cancel, and do show the copies.
func TestACancelledBackupIsToldAsCancelled(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	okLines := strings.Join(health.Summary(health.Gather(out, ""), health.Assess(health.Gather(out, ""), time.Now())), "\n")
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	Run(cancelled, Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key}, discard(), nil)
	in := health.Gather(out, "")
	in.Surface = "desktop"
	lines := strings.Join(health.Summary(in, health.Assess(in, time.Now())), "\n")
	zone := in.LastRun.Started.Local().Format("MST")
	assure.Reached(t, strings.Contains(lines, "→ cancelled: the next backup finishes what it left") &&
		strings.Contains(lines, "was cancelled; what it saved is kept, and the next backup finishes what it left") &&
		strings.Contains(lines, zone+") was cancelled") &&
		!strings.Contains(lines, "failed") && !strings.Contains(lines, state.ProgramText(state.ProgramUpdated)) &&
		!strings.Contains(okLines, "cancel") && strings.Contains(okLines, state.ProgramText(state.ProgramUpdated)),
		"cancelled:\n"+lines+"\nfinished:\n"+okLines)
}

// covers: MA-851, R37
// status -json on an encrypted archive says its counts are sealed — unknown without
// its key — rather than reporting an empty archive: counts_sealed true and
// indexed_known false (P3-8 friction, B21). The positive twin: a plaintext archive's
// counts are known, with no counts_sealed key.
func TestStatusJSONSaysAnEncryptedArchivesCountsAreSealed(t *testing.T) {
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	doc := func(out string) map[string]any {
		in := health.Gather(out, "")
		b, _ := json.Marshal(health.JSON(in, health.Assess(in, time.Now())))
		var m map[string]any
		json.Unmarshal(b, &m)
		return m
	}
	enc := doc(out)
	plainOut := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: plainOut, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	plain := doc(plainOut)
	_, plainHas := plain["counts_sealed"]
	assure.Reached(t, enc["counts_sealed"] == true && enc["indexed_known"] == false &&
		!plainHas && plain["indexed_known"] == true && plain["messages"].(float64) > 0,
		fmt.Sprintf("encrypted %v\nplain %v", enc, plain))
}

// covers: MA-851, R37
// status of an encrypted archive that cannot be opened says its RED once — the
// "encrypted" note does not repeat it — and does not say its copies of MailArchive
// "were updated" under it; and says an encrypted archive is never backed up on a
// schedule (P3-8 friction). The positive twin: before the key files went, the copies'
// line is there, and a plaintext archive's schedule line is unchanged.
func TestStatusOfAnArchiveThatCannotBeOpenedSaysItOnce(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	summary := func(out string) string {
		in := health.Gather(out, "")
		return strings.Join(health.Summary(in, health.Assess(in, time.Now())), "\n")
	}
	before := summary(out)
	os.Remove(filepath.Join(out, vault.FileName))
	os.Remove(filepath.Join(out, vault.BackupName))
	after := summary(out)
	plainOut := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: plainOut}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	plain := summary(plainOut)
	assure.Reached(t, strings.Contains(before, state.ProgramText(state.ProgramUpdated)) &&
		strings.Count(after, "restore-key-file") == 1 && !strings.Contains(after, state.ProgramText(state.ProgramUpdated)) &&
		strings.Contains(after, "never on a schedule") && strings.Contains(plain, "Schedule:   none recorded"),
		"before:\n"+before+"\nafter:\n"+after+"\nplain:\n"+plain)
}

// covers: MA-851, R37
// The note's program section (P3-8 friction, B22): a Mac self-copy is filed under Mac,
// with the Gatekeeper step and its own file name in each line; Windows is told to open
// mailarchivew.exe for the Desktop; "it cannot be checked" is explained; every terminal
// step is literal. The note says where a secret is likely kept. Positive twin: a Linux
// copy keeps its own steps, and a Mac release copy needs no terminal.
func TestTheNoteGivesLiteralStepsForEachSystem(t *testing.T) {
	keep := true
	section := func(files ...container.ProgramFile) string {
		return programSection(&container.ProgramSet{Format: container.ProgramFormat, Keep: &keep, Files: files})
	}
	macSelf := section(container.ProgramFile{Name: "mailarchive-self-darwin-arm64", Origin: container.OriginSelf, Platform: "darwin-arm64"})
	win := section(container.ProgramFile{Name: "mailarchive-windows-amd64.zip", Origin: container.OriginRelease, Version: "v1.5.0", Platform: "windows-amd64"},
		container.ProgramFile{Name: "mailarchive-self-windows-amd64.exe", Origin: container.OriginSelf, Platform: "windows-amd64"})
	linux := section(container.ProgramFile{Name: "mailarchive-linux-amd64", Origin: container.OriginRelease, Version: "v1.5.0", Platform: "linux-amd64"})
	mac := section(container.ProgramFile{Name: "MailArchive-macos.zip", Origin: container.OriginRelease, Version: "v1.5.0", Platform: "macos"})
	note := EncryptedNote()
	assure.Reached(t, strings.Contains(macSelf, "own Mac program") && !strings.Contains(macSelf, "Linux") &&
		strings.Contains(macSelf, "xattr -d com.apple.quarantine mailarchive-self-darwin-arm64") &&
		strings.Contains(macSelf, "./mailarchive-self-darwin-arm64 serve -out") && strings.Contains(macSelf, "cannot be checked: no\n    release page") &&
		strings.Contains(win, "double-click mailarchivew.exe") && strings.Contains(win, "mailarchive-self-windows-amd64.exe serve -out") &&
		strings.Contains(linux, "chmod +x mailarchive-linux-amd64") && strings.Contains(linux, "./mailarchive-linux-amd64 serve -out") &&
		strings.Contains(mac, "Open Anyway") && !strings.Contains(mac, "chmod") &&
		strings.Contains(note, "password manager") && strings.Contains(note, "recovery key printed or written on paper"),
		"mac self:\n"+macSelf+"\nwindows:\n"+win+"\nlinux:\n"+linux+"\nmac:\n"+mac+"\nnote:\n"+note)
}

// covers: MA-851, R37
// A backup that adds or removes copies of MailArchive — tens of megabytes — says so in
// one line, and the run's result carries it for the Desktop (P3-8 friction, B23). The
// positive twin: a backup that changes nothing says nothing of copies.
func TestABackupThatChangesTheCopiesSaysSo(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	added, addLog := backup(t, out, key, ProgramCopiesOn)
	same, sameLog := backup(t, out, key, ProgramCopiesUnset)
	if err := SealProgramCopies(context.Background(), Options{Out: out, Key: key}, ProgramCopiesOff, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	removed, removedLog := backup(t, out, key, ProgramCopiesUnset)
	assure.Reached(t, strings.HasPrefix(added.Program.Summary, "Copies of MailArchive: wrote 1 file(s)") && strings.Contains(addLog, added.Program.Summary) &&
		strings.HasPrefix(removed.Program.Summary, "Copies of MailArchive: removed 1 file(s)") && strings.Contains(removed.Program.Summary, "as you chose") &&
		strings.Contains(removedLog, removed.Program.Summary) &&
		same.Program.Summary == "" && !strings.Contains(sameLog, "Copies of MailArchive:"),
		"added: "+added.Program.Summary+"\nsame: "+same.Program.Summary+"\nremoved: "+removed.Program.Summary)
}

// covers: MA-851, R37
// The check without the key says, once, that the program folder and README.txt are
// checked only by verify -full (P3-8 friction, T2-3); the check with the key does not
// say it. verify -full's warning that the note was changed is not filed under the
// copies of MailArchive, which keep their own prefix (P3-8 friction).
func TestTheKeylessCheckSaysWhatOnlyTheFullCheckSees(t *testing.T) {
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keyless := strings.Join(VerifySummary(rep), "\n")
	full := strings.Join(VerifySummary(Report{Out: out, Encrypted: true, Tier: state.TierFull, KeyedLoad: true, Outcome: state.OutcomeIntact,
		ProgramFindings: []string{noteChangedFinding, "the copy for Mac is missing"}}), "\n")
	assure.Reached(t, strings.Count(keyless, "checked only by `mailarchive verify -full") == 1 && !strings.Contains(full, "checked only by") &&
		strings.Contains(full, "warning: the note beside the archive (README.txt)") && !strings.Contains(full, "copies of MailArchive: the note") &&
		strings.Contains(full, "warning: copies of MailArchive: the copy for Mac is missing"),
		"keyless:\n"+keyless+"\nfull:\n"+full)
}

// covers: MA-851, R37
// A stopped unencrypted copy is told with "readable" again and says how far it got —
// and its own words never say "stopped", which the front end's prefix already does
// (P3-8 friction). The copy's README says first that it is an unencrypted copy. The
// plan shows the mail's size and the reserve apart. Positive twin: a whole copy's
// README carries the same head.
func TestAStoppedCopySaysReadableOnceAndTheCopySaysWhatItIs(t *testing.T) {
	f := newFakeCopy(t, copyMsg("a"), copyMsg("b"), copyMsg("c"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.during = func(rel string) {
		if rel == "s/Inbox/b.html" {
			cancel()
		}
	}
	f.fail["s/Inbox/b.html"] = context.Canceled
	_, p, err := copyOf(t, f, ctx, nil)
	msg := fmt.Sprint(err)

	whole := newFakeCopy(t, copyMsg("alpha"))
	_, wp, werr := copyOf(t, whole, nil, nil)
	readme, _ := os.ReadFile(filepath.Join(wp.Dest, "README.txt"))
	assure.Reached(t, strings.Contains(msg, "READABLE") && strings.Contains(msg, "1 of 3 messages") && strings.Contains(msg, p.Dest) &&
		!strings.Contains(msg, "stopped") && werr == nil && strings.HasPrefix(string(readme), "THIS FOLDER IS AN UNENCRYPTED COPY") &&
		wp.MailBytes > 0 && wp.ReserveBytes > 0 && wp.NeedBytes == wp.MailBytes+wp.ReserveBytes,
		msg+"\n"+string(readme))
}

// covers: MA-851, R37
// A keyed rebuild of the search index does not say the index "is missing messages"
// on every run: the rebuild was asked for (P3-8 friction). The positive twin: an index
// truly behind still says so.
func TestARebuildDoesNotSayTheIndexIsMissingMessages(t *testing.T) {
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	var rebuilt bytes.Buffer
	if _, err := RebuildEncrypted(context.Background(), Options{Out: out, Key: key}, log.New(&rebuilt, "", 0)); err != nil {
		t.Fatal(err)
	}
	// Twin: a backup with indexing off leaves the index behind, and says so.
	behind := logOf(t, Options{Inputs: []string{writeData(t, tmpDir(t), "more.mbox", []byte(indexMbox("another", "three")))}, Out: out, Key: key})
	assure.Reached(t, !strings.Contains(rebuilt.String(), "missing messages") && !strings.Contains(rebuilt.String(), "holds every message again") &&
		strings.Contains(behind, "missing messages"),
		"rebuild:\n"+rebuilt.String()+"\nbehind:\n"+behind)
}

// logOf runs an encrypted backup of opts and returns its log.
func logOf(t *testing.T, opts Options) string {
	t.Helper()
	var logged bytes.Buffer
	if _, err := Run(context.Background(), opts, log.New(&logged, "", 0), nil); err != nil {
		t.Fatalf("backup: %v\n%s", err, logged.String())
	}
	return logged.String()
}
