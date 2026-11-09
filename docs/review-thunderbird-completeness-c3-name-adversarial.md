# Light adversarial look: slice C3's account name (the read, the record, every surface)

**Verdict: NOT CLEAN (1 MEDIUM, 3 LOW; no HIGH).** The name never changes the account match, the
verdict, the reason, the store id (the streak and carry-forward key) or the run's lead. A planted
name cannot lower the posture or drop a WARN in `status`. The CLI, the `-log` file, the Desktop
page and every message dialog show it with no control, escape or bidi character. `-json` is
valid JSON for any input. An encrypted archive's folder never holds it. The findings:

- **N-01 (MEDIUM, Linux wizard only).** The wizard's launch health view on Linux passes the health
  text to `zenity --list --text`. zenity sets that text with
  `gtk_label_set_markup(g_strcompress(text))`, and `--no-markup` does not apply to a list. The
  account's name now reaches that text raw. An ordinary name such as `AT&T Mail` or
  `Sam <sam@work.example>` is broken markup. A name holding a backslash and `n` (prefs.js `\\n`)
  becomes a real line break. This is an older class of problem: the archive path and IMAP folder
  names such as `Bills & Receipts` already reach this text. The name widens it, and is the likeliest
  of the three to hold `&` or `<`.
- **N-02 (LOW).** A name that spells another account's folder words makes two accounts read as one:
  account A named `the Thunderbird account kept in the folder imap.b.example` beside account B with
  no name. The shared-name rule only compares names with names.
- **N-03 (LOW).** Names that look the same but differ in code points pass the shared-name rule:
  NFC `josé` beside NFD `jose` + U+0301, and a Cyrillic `а` in `sаm@x.example`.
- **N-04 (LOW, privacy; the lead's or owner's call).** The command line's summary writes the
  address to the run's `-log` wherever that file is. The design's privacy note says a scheduled
  run's `-log` "sits in the archive folder too". That holds only for the default path. A
  `-log` outside the archive (a custom `schedule -log`, or any hand run) gets the address, and so
  does a keyed (encrypted) command-line run, where `RefuseLogInside` allows any log outside the
  archive.

No product code was changed by this pass.

## Header

- **Reviewed:** branch `tb-completeness` at `a2bd246` (origin tip; `git pull --rebase` in the
  worktree said "Already up to date"). Scope: `git show b8f9229 -- internal/thunderbird/storefacts.go
  internal/app/tbcompleteness.go internal/wizard/ internal/desktop/runhistory.go` and `git show
  a2bd246`, plus the surfaces they feed (health, the CLI summary, the Desktop page script, the
  zenity module).
- **Process:** `../assurance-kit/process/` adversarial pass, light and narrow, as briefed. One
  reviewer, who wrote neither commit.
- **Clone:** `.claude/attack-c3-name`, a git clone of the worktree at `a2bd246`. Nothing there is
  committed. Attack files: `internal/app/zz_attack_c3name_test.go` (ATK-1 to ATK-6) and
  `cmd/mailarchive/zz_attack_c3name_test.go` (ATK-7). Each test asserts the safe property, so a
  red test is a confirmed finding. Every probe uses real temporary Thunderbird profiles (prefs.js,
  compatibility.ini, folderCache.json, mbox and .msf files) and real archive folders, through
  `app.Run`. No real profile was touched.
- **Command and result** (Go 1.26.0, darwin/arm64, 2026-11-08):
  `go test -count=1 -run TestAtk -v ./cmd/mailarchive/ ./internal/app/`. Red: ATK-1 (N-01),
  ATK-2 (N-02), ATK-3 (N-03), ATK-7 (N-04). Green: ATK-4, ATK-5, ATK-6.
- **Sources outside the repo:** the zenity Go module at v0.10.12 (from the module cache), and
  GNOME zenity's `src/tree.c` and `src/msg.c` (master, read on gitlab.gnome.org on 2026-11-08). No
  Linux desktop was run, so how GTK draws broken markup is not seen here (see N-01).

## 1. The read (prefs.js / user.js through the C0c reader)

**HELD: the name cannot change the match or the verdict.** `accountNameOf` reads
`mail.server.<key>.name`, else `mail.server.default.name`, after the account is matched. It cannot
fail. `wrongType` does not type-check `name`, so a numeric or boolean name is "no name" and never
makes the file uncertain. ATK-4 runs one store with no name, `"sam@k.example"`, 12 KB of `<b>`,
`5`, `true`, `"\\n\\r\" OK"` and `""`. The folder, store id, verdict, reason and lead are the same
every time.

**HELD: escapes, length, control, format and bidi characters.** The reader allows only `\\`, `\"`,
`\n` and `\r`. `unquote` decodes them. `CleanFolderName` then strips C0 and C1 controls (so a
decoded LF or CR is gone and the text joins up), Cf and default-ignorable characters, and
U+2028/2029. It folds space separators and caps the result at 80 characters plus "…". The builder's
tests (`TestTheNameIsCleanedAndCapped`, `TestHostileNamesInTheWizardAndRunNow`) cover this, and the
probes here agree.

**HELD: invalid UTF-8.** `strings.Map` turns invalid bytes into U+FFFD, which is visible. ATK-5
(prefs.js name `"sam\xff\xfe\xc0@u.example"`): the last-run record and the run history are valid
JSON and valid UTF-8, and `json.Marshal(health.JSON(...))` is valid and carries `"name":"sam…`.

**Accepted, as for folder names (TC-7): text inside one line.** A name can hold the words
`Backup complete.` in the middle of a sentence. It cannot start a new line on any surface except
Linux's list dialog (N-01). It is capped at 80 characters. The person or their account setup
chose it.

**CONFIRMED N-02 (LOW): a name can impersonate another account's folder words.**

```go
p := newTBProfile(t,
	tbAcct{n: 1, host: "imap.a.example", typ: "imap", dirRel: "ImapMail/imap.a.example",
		extra: map[string]string{"name": `"the Thunderbird account kept in the folder imap.b.example"`}},
	tbAcct{n: 2, host: "imap.b.example", typ: "imap", dirRel: "ImapMail/imap.b.example"})
// both stores behind (3 of 40); run both
accts, more := TBAccounts(runTB(t, Options{Inputs: []string{sa, sb}, Out: out}))
lines := state.TBLines(accts, more, state.TBSay{})
// lines[0] == lines[1] ==
// "Thunderbird has downloaded about 3 of 40 messages in the Thunderbird account kept in the
//  folder imap.b.example so far. Furthest behind: Inbox 3 of 40."
```

The same holds for the lines read back from the record. Positive twin: a name of `sam@a.example`
reads apart. `dropSharedNames` compares names only with names, never with the words another account
is said by (`tbName`: `tbFolderPrefix + folder`, or "this account"). It needs a name the person (or
whoever wrote the profile) chose, or a planted record. A fix would compare each account's *said*
name (`tbName`) instead of its `Name`. That is a builder's call.

**CONFIRMED N-03 (LOW): look-alike names pass the shared-name rule.**

```go
for what, pair := range map[string][2]string{
	"NFC vs NFD":          {"josé@x.example", "josé@x.example"},
	"Cyrillic look-alike": {"sam@x.example", "sаm@x.example"},
} {
	accts := []state.TBAccount{{Account: "imap.x.example", Name: pair[0]}, {Account: "imap.x.example-1", Name: pair[1]}}
	state.TBDistinctNames(accts)
	// both names kept: two accounts that look like one
}
```

`CleanFolderName`'s doc says "two names that look the same are the same". That holds for spacing
and invisible characters but not for canonical equivalence. Folder names have the same gap from
C2. Normalising to NFC before comparing would close the first case. Confusables (the second case)
are a judgement call. Two real Thunderbird accounts rarely differ like this, so this is LOW.

## 2. The record (a planted `name` in `.mailarchive-lastrun.json` / the run history)

**HELD: cleaned, capped and the shared-name rule again on read.** `saneTB` and `saneTBCompact`
clean `Name` and apply `TBDistinctNames` / `dropSharedNames`. The builder's
`TestAPlantedNameIsCleanedOnRead` covers 10 KiB, control, bidi, zero-width, nothing visible, and
two names that look alike.

**HELD: a planted name cannot lower the posture or suppress a WARN.** ATK-6 writes a real run's
record (one account behind), then replaces its `name` with `x.\nOK: Backup complete.\n`,
`\x1b[2K\rOK`, 1,000 U+202E characters, and `OK`. Each time the posture, the number of reasons and
the number of `WARN: ` reasons stay the same, and no reason holds `\n`, `\r`, ESC or U+202E. No
product code branches on the words (a search of desktop, health, wizard and the CLI for
`Contains`/`HasPrefix` on "Backup complete", "Backup finished", "Backed up what" or "still
downloading" finds tests only). Desktop's one text substitution (`wizardReadThem`, the Open archive
hint) replaces only the first match, and that match is in the lead, which comes before any account
line.

**HELD: the folder stays the key.** The streak and carry-forward key on `StoreID`, and `-json`'s
`account` is the folder. ATK-4 shows the name never reaches them. A planted name can make an
account *read as* another address. Anyone who can write the record can already write any words
there, which is the record's existing trust (it is not signed).

## 3. Every surface that shows a name

| Surface | How the text gets there | Result |
|---|---|---|
| Wizard end summary, Linux | `zenity.Question` → `zenity --question --text … --no-markup` → `gtk_label_set_text` | HELD |
| Any message dialog, macOS | JSON-encoded into the JXA script, script on stdin | HELD |
| Any message dialog, Windows | native MessageBox (plain text) | HELD |
| **Wizard launch health view, Linux** | `zenity.List("Backup health — …"+text)` → `zenity --list --text` (no `--no-markup` in `list_unix.go`) → `gtk_label_set_markup(g_strcompress(text))` (zenity `tree.c`) | **N-01** |
| Health list, macOS / Windows | JSON into JXA / native list | HELD |
| Desktop page (card, Recent backups, Status & health, Run now note) | html/template for the page; the page script builds nodes with `createElement`/`textContent`. The only `innerHTML` in product code is a fixed "has stopped" string (`dashboard.go:2539`) | HELD (and `TestAHostileAccountNameOnTheDesktop`) |
| CLI `status`, the run summary, `-log` | `CleanFolderName` strips C0/C1 (ESC, CR, LF) | HELD (`TestStatusSaysThunderbirdsNameForTheAccount`) |
| `status -json` | `encoding/json` | HELD (ATK-5) |

**CONFIRMED N-01 (MEDIUM, Linux wizard only).**

```go
_, store := imapProfile(t, "imap.att.example",
	map[string]string{"name": `"AT&T <sam@att.example>\\nBackup complete."`}, atkInbox())
runTB(t, Options{Inputs: []string{store}, Out: out})
in := health.Gather(out, "")
text := strings.Join(health.Summary(in, health.Assess(in, time.Now())), "\n")
// text holds, raw:
//   WARN: Thunderbird has downloaded about 3 of 40 messages in AT&T <sam@att.example>\nBackup complete. so far. ...
```

`wizard.go:1248` hands exactly that text to `zenity.List`. On Linux, ncruces/zenity v0.10.12
builds `--list --hide-header --text TEXT` (`list_unix.go:8`) with no `--no-markup`. GNOME zenity's
`tree.c` then calls `gtk_label_set_markup(GTK_LABEL(text), g_strcompress(tree_data->dialog_text))`
and has no no-markup branch. (`msg.c` has one, which is why the message dialogs hold.) So, on a
Linux desktop:

- `&` or `<` in the name (`AT&T`, `Sam <sam@work.example>`, both plausible names a person types) is
  broken Pango markup. GTK then refuses the whole label's markup. By GTK's documented behaviour the
  health text, its posture line and every WARN would then not be shown. **Not seen here: no Linux
  desktop was run.** The lab check: on Linux, name an account `AT&T Mail` in Thunderbird, back it
  up, open `mailarchive wizard`, and look at the "Backup health" list.
- A hostile but valid name (`<span size="xx-large">…</span>`, `<s>…</s>`) restyles the text.
- A backslash then `n` in the name (`\\n` in prefs.js, which the C0c reader allows) becomes a real
  line break through `g_strcompress`. That forges a line, such as a fake `Backup complete.`, in this
  one dialog. `\033` would become ESC there too, though GTK draws no terminal codes.

The class is older than this slice. The archive path (`Archive: …`) and IMAP folder names in
"Furthest behind" (C3, `tbFolderName`) already reach this text unescaped, so a Gmail label
`Bills & Receipts` that is behind does it too. The name widens it with the text most likely to hold
`&` or `<`. A fix, for the builder: escape `&`, `<`, `>` and `\` in the text the wizard gives to
`zenity.List` on Linux and the BSDs, or show the health text in a message dialog. Either way, a
red-first test on the argv the list is given (mocking the external binary is allowed for argv
hygiene).

**INFO (older; not a finding of this slice).** On Linux the dialog text is argv to `zenity`, so
another local user can read the address in the process list while the dialog is open. Paths and
folder names have been there since the wizard began.

## 4. `-json` `name`

**HELD.** ATK-5 (above): valid JSON and valid UTF-8 for an invalid-UTF-8 name, from the record and
from `health.JSON`. On write the name is already valid UTF-8 (`strings.Map`). On read,
`encoding/json` turns invalid bytes into U+FFFD, and `CleanFolderName` runs again.

## 5. Privacy: where the address is written

**HELD:**
- the last-run record and the run history, in the archive folder, plaintext archives only (the
  builder's `TestAnEncryptedArchiveWritesNoName`, `TestAnEncryptedRunWritesNoName`: no file in an
  encrypted archive's folder holds the name);
- no alert email: `internal/backupalert` uses no completeness type, so the emails keep the folder
  name, as the design says;
- a scheduled run-job's `mailarchive.log` (`RunHeadless` → `logSummary`) prints only counts. Run
  now's note is never logged (`jobrun.go` logs only its start lines);
- the Desktop writes no Run now note or account words to the config folder (its product file
  writers are `launch.go` and `archivedelete.go`, neither with completeness words). A flat schedule
  into an encrypted archive, or with its log inside one, is refused (`schedule/encrypted_test.go`);
- the shared Windows wrapper sends only stderr (crashes) to `<log>.stderr.log`.

**CONFIRMED N-04 (LOW, privacy; the lead's or owner's call).** `printSummaryFor`
(`cmd/mailarchive/main.go:2406`) writes the account lines, with the name, to the run's logger, and
the logger writes to `-log`. It cannot tell an encrypted run (it gets only `app.Result`), and the
keyed export path calls it too (`main.go:444`, after `keyring.Apply`).

```go
logPath := filepath.Join(t.TempDir(), "elsewhere.log") // outside the archive
r := app.Result{Thunderbird: []tbcomplete.Result{{Account: "imap.qz.example", Name: "qzsam@qz.example",
	StoreID: "id", Verdict: tbcomplete.Behind, OnDisk: 3, Listed: 40}}}
printSummaryFor(log.New(f, "", 0), r, false, false, out, "Done.", "darwin")
// elsewhere.log:
//   Thunderbird: Backed up what Thunderbird has so far.
//     Thunderbird has downloaded about 3 of 40 messages in qzsam@qz.example so far. ...
```

So the address reaches:
- a plaintext archive's custom `-log` outside the archive folder (`mailarchive schedule … -log
  ~/logs/x.log`, or a hand run). The design note's "a scheduled run's -log sits in the archive
  folder too" is true only for the default path;
- the `-log` of a keyed command-line run into an encrypted archive. `RefuseLogInside` keeps the log
  out of the archive, so the log is a plaintext file elsewhere. Before this slice that log already
  held the store's path and folder (`imap.gmail.com`), but not the address. O1 lets an encrypted
  run show its verdicts on screen. Whether a `-log` copy of that screen may carry the address is
  not settled by the owner's ruling ("records … keep the folder name") or by the design note.

The person chose where these files go, and the mail itself is not in them, so this is LOW. Two
options for the lead: keep it, and correct the design note's sentence; or have the summary say the
folder words when the run is keyed or the log sits outside the archive.

## 6. Notes for the builder (not findings)

- `internal/tbcomplete/verdict.go:225`: `Result.Name`'s comment still says "for the words on
  screen only, never recorded". Since `a2bd246` the name is recorded for plaintext archives.
- Gate state, unchanged by this pass: the friction re-look of C3's changed words is still owed (the
  builder said so in both commits).
