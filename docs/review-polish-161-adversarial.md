# Adversarial pass: polish-161, items 1 and 3 (MA-1151, MA-1153)

**Verdict: NOT CLEAN (low only).** Planted text cannot make any link other than the fixed report
address, cannot add an element, cannot add an email header, and cannot put a control or bidi
character into the computer name. Three LOW findings and one INFO remain, all about the computer
name's words, not about injection. Two have candidate fixes proven red then green in a throwaway
clone; none is built into this branch (this commit is docs only).

## Header

- Reviewed commit: `ec7afba` (branch polish-161), frozen before probing; worked on 2026-11-07.
- Scope: (1) the report address as a real link on MailArchive Desktop (Recent backups rows, Archive
  now's result, Run now's note, a first backup's line, Status & health); (3) the stuck lock
  holder's computer name, read from the archive's lock file, in words on Desktop, `status`, alert
  emails and the attention notice.
- Threat model: anyone who can write into the archive folder (a shared or synced drive) can plant
  the lock file (`.mailarchive.lock`) and the run records (`.mailarchive-runhistory.json`, the
  last-run record). To get the computer name shown, they must also hold the lock's OS lock with
  `started=` a day or more back: a free lock is overwritten by the next backup.
- Not in scope (a writer of the folder can already write these directly): the text of
  `BACKUP-NEEDS-ATTENTION.txt` and of the records themselves.
- File digests (SHA-256, at `ec7afba`): `internal/app/busy.go` 60bbfc30…; `internal/lockfile/lockfile.go`
  c96707e1…; `internal/desktop/runhistory.go` a7e57455…; `internal/desktop/dashboard.go` 681d74d5….
- Finder and skeptic: one model (Claude Opus 5.5) in both roles, one after the other. The roles
  were not separated as METHODOLOGY asks; a second reader would close that gap.
- Toolchain: go1.26.0 darwin/arm64; Linux in Docker `golang:1.26` with `--user 1000:1000 --tmpfs /tmp`.
- Probes: `internal/app/zz_adv161_host_test.go`, `internal/desktop/zz_adv161_link_test.go` and a
  scenario `advReportLink` added to `internal/desktop/testdata/pagescript/harness.js`, all in the
  throwaway clone `.claude/attack-polish` (not committed).
- Tally: **8 attacks tried / 4 confirmed (3 LOW, 1 INFO) / 4 refuted.**

## Read first: the code paths

- The computer name has one producer: `lockfile.HolderHost` → `app.busyWords` (busy.go:58). Every
  surface (the Desktop `busy` field, the last-run record and so `status`, the email's Reason, the
  notice) takes the words from `busyError.msg`. A grep finds no other caller of `HolderHost`.
  `status`'s own lock line uses only `HolderVerb` (health.go:1882). So the sanitiser covers every
  surface that the name reaches.
- `HolderHost` reads 1 KB of the line, removes C0/C1 controls (`readHolderN`), splits on spaces,
  takes the first `host=`, keeps only `[A-Za-z0-9._-]`, cuts to 64 bytes and trims `.-_` from the
  ends.
- The Recent backups template links only a literal built from the constant (dashboard.go:2300);
  `linkParts` splits the words on `state.ReportAddress`, and `{{.Text}}` is escaped by html/template.
- The page script's `sayText` (dashboard.go:2412) splits on the same constant and builds the link
  with `createElement`/properties; the other pieces go in as text nodes.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix | Tombstone test |
|---|---|---|---|---|---|---|
| L1 | outside (page) | Planted history words make a link elsewhere or add an element: the address followed by `@evil.example/x`, inside `https://evil.example/?u=…`, twice touching, after `javascript:`, after `"><a href=…>`, a Cyrillic look-alike, a zero-width split, upper case | **REFUTED** | `TestAdv161PlantedHistoryLinks`: 9 planted entries → exactly 8 `<a>`, every `href` is the constant, no tag other than `ul/li/span/a`; the look-alike and upper-case forms stay words. The extra text after the address shows as plain words; the click still goes to the real address. | none needed | the probe (clone) |
| L3 | outside (page script) | The same hostile words, with `<img onerror>` and `${reportAddress}`, through `sayText` on Archive now's error, Run now's busy note and Status & health | **REFUTED** | harness `advReportLink`, 3 of 3 checks: each element's children are only text nodes and `<a>` to the constant with no child elements; text unchanged. macOS only (the Linux image has no Node). The harness is a fake DOM: it proves the script never parses markup, not how a browser draws it. | none needed | the scenario (clone) |
| H1 | outside (lock line) | Control, bidi, zero-width, fullwidth, CR/LF, NUL, ESC/OSC, HTML, shell text, a leading `--`, 5,000 characters, two `host=` fields in `host=` | **REFUTED** | `TestAdv161PlantedHostNames`, 17 forms: the name in the words is always `[A-Za-z0-9._-]`, at most 64; e.g. `\x1b]0;pwn\x07OFFICE` → `0pwnOFFICE`, `$(reboot);rm${IFS}-rf` → `rebootrmIFS-rfid`, `--help` → `help`, fullwidth → none (the 1.6.1 words). No newline, so nothing reaches a header; the name is never put in a command line, only in a sentence. | none needed | the probe (clone) |
| E1 | outside (email) | The name or the words add an email header (`\r\nBcc:`) or change the subject | **REFUTED** | `TestAdv161HostInTheAlertEmail`: the subject is the fixed "MailArchive backup skipped, the archive is still in use: /vol/A"; the lock reader removes CR/LF before the name is read, and alert.go's `header()` strips CR/LF from the subject anyway; the words go only in the body. | none needed | the probe (clone) |
| H2 | integrity (own name) | busy.go:58 compares the **cleaned** planted name with this computer's **raw** `os.Hostname()`. If this computer's own name has a character outside `[A-Za-z0-9._-]` (a Windows name like `JOSÉ-PC`), its own stuck run is said to be on "another computer, JOS-PC… (restarting this one does not)": the wrong fix, so the stuck lock stays and backups keep being skipped. | **CONFIRMED, LOW** | `TestAdv161OwnNonASCIIHostIsThisComputer` red at `ec7afba` (macOS): "…on another computer, JOS-PC…". Not tried on a real Windows computer with such a name. | candidate (clone): `SameHost` cleans both sides first, `a, b = sanitizeHost(a), sanitizeHost(b)` (lockfile.go:345). Red → green on macOS; green on Linux; `TestAStuckHolderOnAnotherComputerNamesIt` and the lock tests still green. | the probe → MA row to add |
| H3 | outside (email) | A planted dotted name (`your-bank.com`) is kept, and goes into the alert email's body twice ("restart the computer your-bank.com"). Many mail apps underline a bare domain as a link, so a folder writer gets a clickable address into an email sent from the owner's own alert account. | **CONFIRMED that the name reaches the body, LOW**; whether a given mail app links it was not tested | `TestAdv161HostInTheAlertEmail` (assertion "body has no `your-bank.com`") red at `ec7afba`. | candidate (clone): name the computer by its first label only, `host, _, _ = strings.Cut(host, ".")` at busy.go:59 (what a person calls the computer: `Alexs-MacBook-Pro.local` → `Alexs-MacBook-Pro`, a Windows name has no dot; `SameHost` already compares first labels). Red → green on macOS and Linux. | the probe → MA row to add |
| H4 | insider (social) | The words state a planted name as fact: "It started… on another computer, FILESERVER… restart the computer FILESERVER". A folder writer who also holds the lock can steer the owner into restarting a real, important machine. Names like `THIS-COMPUTER` also read as a contradiction. | **CONFIRMED (mechanism in source), LOW** | `TestAdv161PlantedHostNames` logs `THIS-COMPUTER` named as another computer; any valid name is shown as-is by design. | proposed, **not built (a wording call for the lead or owner)**: say where the claim comes from, e.g. "its lock file names the computer FILESERVER". | — |
| L2 | outside (page) | History words keep bidi overrides (U+202E) and zero-width spaces: `ReadRunHistory` uses `StripControl`, not `StripBidi` (state/runhistory.go:92). Next to the new link, an override can reverse or move how the address *looks*. | **CONFIRMED that the characters reach the page, INFO**; the drawing effect was not checked in a real browser | `TestAdv161PlantedHistoryLinks` logs both characters inside the Recent backups list. The link's destination stays the constant, so this is how it looks, not where it goes. The same gap was already there for the words before this commit. | proposed, not built: `util.StripBidi` on `Error` in `ReadRunHistory` and the last-run reader (as friction F16 did for mail text), or `dir="ltr"` on the link so it is drawn on its own. | — |

## Commands and results

- macOS, at `ec7afba` plus the probes: `go test -count=1 -run "TestAdv161|TestAStuckHolder" ./internal/app/`
  → exit 1: H2 red, the email probe red (once its check was tightened from a log line to an
  assertion), H1 green. `go test -count=1 -run "TestAdv161|TestTheReportAddressIsALinkOnThePage|TestRecentBackupsLinksTheReportAddress" -v ./internal/desktop/`
  → exit 0 (L1 and L3 refuted; L2 logged).
- macOS, with the two candidate fixes: `go test -count=1 -run "TestAdv161|TestAStuckHolder|Busy|Lock" ./internal/app/ ./internal/lockfile/`
  → exit 0; `-run "TestAdv161|TestAStuckHolder|Busy|Partial" ./internal/app/` → exit 0.
- Linux (Docker, uid 1000, with the fixes): the app, desktop and lockfile probes plus the commit's
  own MA-1151/1153 tests → exit 0; the two page-script tests skipped (no Node in the image).
- Windows: not run. The code under test is plain string handling and the probes set this
  computer's name directly; the one Windows-specific question (what `os.Hostname` returns for a
  computer name with an accent) needs the VM renamed, which this pass did not do.
- No full unfiltered run: no product code changed in this commit.

## What the lead should decide

1. H2 and H3: take the two one-line fixes, each with a covering test (an MA row each), in the
   usual red-then-green steps.
2. H4: whether the words should say the name comes from the lock file.
3. L2: whether to apply `StripBidi` to the run records' words now or record it as known.

Out of scope, noted only: other lock refusals (verify, reindex) still show the raw holder line
(`lockfile.Holder`: controls removed, bidi and other Unicode kept) in their error text; this commit
did not change that.
