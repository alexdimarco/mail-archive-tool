*Filed by the lead, 2026-11-18. The report below is the reviewer's, word for word: an independent
Claude agent that wrote no part of the change, reviewing commit 9e3feae on branch
`picker-account-names` (diff from cee7259). The lead's fold follows it.*

# Light adversarial look: the wizard's list of found mailboxes (S85, MA-1462 to MA-1467)

**Verdict: NOT CLEAN (0 HIGH, 0 MEDIUM, 6 LOW, 5 INFO).** These held up:

- No name can make a row read as the "All of them" row or carry its arrow.
- No row starts with "-", so none reaches the Linux dialog program as an option.
- No row holds a NUL, another control character or a bidi control, so the Windows list cannot be made to crash.
- Every answer maps to its own row or to none. An answer is never taken as a path.
- The list writes nothing.
- When nothing is changing on disk, the size walk follows no link, opens no file and waits on no named pipe.

The findings:

- **SPF-01 (LOW).** A name that starts with a digit followed by a Hebrew or Arabic letter makes its row run right to left on macOS and Linux.
- **SPF-02 (LOW).** Two profiles whose folder names end the same (both `*.default-release`) get the same "profile …" words. Their rows then differ only by size or " (2)". On Windows, where the end of a long row is cut off, they can look exactly the same.
- **SPF-03 (LOW, Linux).** One address set up in both Thunderbird and Evolution gives two rows that differ only by size or " (2)". Nothing says which program each row is.
- **SPF-04 (LOW).** A mail folder named like an account's name takes that name away from the account. The only row showing the address is then the other folder's.
- **MAP-01 (LOW, Linux).** With GLib 2.84 or older and a locale that is not UTF-8, "All of them" cannot be chosen. A row spelled with "?" can also take another row's pick.
- **FS-01 (LOW).** If a folder is swapped for a link while sizes are being counted, the count follows the link out of the store. That contradicts MA-1466's "follows no link".
- **PRV-01 (INFO, owner's call).** On Linux every row, and so every found address and its size, is on the dialog program's command line while the list is open.
- **SPF-05 (INFO).** Look-alike letters from the same script, such as "AII of them" with capital i's, are not caught. The documented limit only mentions other scripts.
- **FS-02 (INFO).** The 3-second limit covers only the size counts, not everything that happens before the list appears. Measured, it is small.
- **FS-03 (INFO).** Many rows that read alike cost time that grows with the square of their number.
- **FS-04 (INFO, older than this change).** If an Evolution account's settings file is a named pipe, the list never appears.

No product code was changed by this pass.

## Header

- **Reviewer:** an independent Claude agent, a fresh instance with no name given. I wrote no part of the change.
- **Date worked:** 2026-11-18, from the system's date.
- **Reviewed:** commit `9e3feae`, detached, in the review copy `.claude/worktrees/picker-adv`. The change is `git diff cee7259 9e3feae`. I read the code and the catalog rows first, then the tests.
- **Process:** a light, narrow pass following `assurance-kit/process/adversarial-review.md`. Each finding is reproduced by a red test. Where a finding cannot run on this Mac, I show the mechanism from the cited source and say so.
- **Attack files**, not committed, all in `internal/wizard/`:
  - `zz_attack_picker_spf_test.go`
  - `zz_attack_picker_map_test.go`
  - `zz_attack_picker_fs_test.go` (Unix only)
  - `zz_attack_picker_prv_test.go` (Unix only)
  - Every test asserts the safe property, so red means a reproduced finding.
  - **They carry no covers marker.** A run of `go test ./...` with no filter in that copy would fail the covers meta-test.
- **Go:** go1.26.0 darwin/arm64.
- **Commands and exit status:**
  - `git status`, `git log`, `git diff --stat cee7259 9e3feae`, `git diff cee7259 9e3feae -- …`: all 0.
  - `go test … ./internal/wizard/` run from the wrong folder: 1, "outside main module" (my mistake; repeated with `go -C`).
  - `go -C <copy> test -count=1 -run '<the change's 10 wizard tests>' ./internal/wizard/`: 0.
  - `go -C <copy> test -count=1 -run TestTheMatchsKindIsReadFromItsError -v ./internal/thunderbird/`: 0.
  - The `-run TestAtkSPF` runs: first 1, a compile error because my editor wrote a literal byte-order mark into my own file (I rewrote it); then 1, as listed below.
  - `-run TestAtkMAP`: 1. `-run 'TestAtkFSHeld|TestAtkFSInfo'`: 0. `-run TestAtkFS01`: 1. `-run TestAtkFS02`: 0. `-run 'TestAtkPRV|TestAtkINFO'`: 1. `-run TestAtkFSHeld_PipedSettingsFilesDoNotHoldTheListBack`: 0. `-run TestAtkSPFHeld`: 0.
  - `GOOS=linux go -C <copy> vet ./internal/wizard/` (type-checks the Linux-only argv test and mine): 0.
  - `go -C <copy> vet ./internal/wizard/`: 0.
  - Final run, `go -C <copy> test -count=1 -run TestAtk -v ./internal/wizard/`: 1.
    - Red: SPF-01, SPF-02, SPF-03, SPF-04 (both sub-tests), SPF-05, MAP-01a, MAP-01b, FS-01, FS-04.
    - Green: both SPF held tests, the MAP held test, both FS held tests, FS-02, FS-03, the PRV held test.
  - No full suite, no Docker, no dialog, no wizard or built binary was run.
- **Sources outside the repo, and how I read them:**
  - I did not read the module cache, because it is in the owner's home folder.
  - I downloaded these with `curl` into the gitignored folder `.claude/worktrees/zz-src/` of the review copy:
    - ncruces/zenity at tag v0.10.12 from raw.githubusercontent.com: `list*.go`, `util*.go`, `zenity_unix.go`, `msg_unix.go`, `internal/zenutil/run_unix.go`, `run_darwin.go`, `osascripts/list.gojs`, `common.gojs`, plus the file list from the GitHub API.
    - GNOME zenity `src/tree.c` (master and 3.44.0) and `src/zenity-tree-column-view.c` (master), from gitlab.gnome.org.
    - GLib `glib/gmessages.c` (2.80.0, 2.84.0, 2.86.0, main) and `glib/gprint.c` (2.86.0, main).
    - Go `src/path/filepath/path.go` and `src/os/types_windows.go` at go1.26.0.
  - I did not compare these downloads with the module copy that go.sum pins.
- **Not checked:**
  - Nothing ran on Linux or Windows.
  - How the toolkits draw text direction, Windows' pixel widths, GLib's behaviour on a real machine, qarma's and matedialog's output, Evolution's default display name, Ubuntu's move of Thunderbird to a snap, and the file browsers' size units are all read from source or stated from general knowledge, as marked.

## Findings

### SPF-01 (LOW): a digit followed by a right-to-left letter turns a row's direction

- **Claim:** MA-1464 says "a right-to-left first letter cannot turn a row's direction". `needsLead` (`internal/wizard/picker.go:405-409`) only checks that the first character is A to Z, a to z or 0 to 9.
  - A digit has no direction of its own (Unicode bidi class EN).
  - A line's direction comes from its first letter that has one (rule P2 of the Unicode Bidirectional Algorithm).
  - So "1שלום@qz.example" gets no lead word, and its first letter with a direction is Hebrew.
- **Reproduction:** `TestAtkSPF01_ADigitThenARightToLeftLetterTurnsTheRow` is RED. These rows start with a digit, but their first letter with a direction runs right to left:
  - `"1שלום@qz.example (1.4 MB)"`
  - `"2024 مرحبا (1.4 MB)"`
  - the folder row `"7שלום"`
  - the Outlook row `"1שלום.pst"`
  - Positive twin, green: `"Account שלום@qz.example (1.4 MB)"`.
- **Weight (reasoned, not seen on screen):** on macOS and Linux a list row's direction follows its text (Pango's automatic direction, AppKit's natural direction). Such a row is drawn right to left, and its size and any " (2)" move to the other end. Windows' list box draws left to right. The row still maps to its own mailbox.
- **Fix:** in `needsLead`, also add the lead word when the first letter with a direction is not left to right (`golang.org/x/text/unicode/bidi`, part of a module the repo already uses), or require an ASCII letter first. Red first with this test.

### SPF-02 (LOW): two profiles whose folder names end alike read alike

- **Claim:** MA-1462 says "each row then says its profile's folder name after the first dot". `profileHint` (`picker.go:385-394`) keeps only the part after the first dot. These cases get the same words:
  - two `*.default-release` profiles, for example an old profile copied in beside the new one;
  - one profile copied whole into a second Thunderbird folder that discovery also searches. Discovery searches `~/.thunderbird` and `~/snap/thunderbird/common/.thunderbird` together (`internal/app/run.go:2086-2091`).
- **Reproduction:** `TestAtkSPF02_TwoProfilesWithOneSuffixReadAlike` is RED for both cases. The rows are `"qzsam@gmail.com (profile default-release, 1.4 MB)"` and `"qzsam@gmail.com (profile default-release, 1.4 MB) (2)"`. With different sizes, the size is the only difference. Positive twin, green: default-release next to default.
- **Weight:** nothing says which row is the profile Thunderbird uses now. Windows is worse (reasoned from source):
  - zenity draws a 241-pixel list box with only a vertical scroll bar (`zenity list_windows.go:89-95`);
  - MA-1467 itself says a row longer than about 38 characters loses its end with no sign;
  - these rows are about 50 characters, so the size and the " (2)" are not drawn and the two rows look identical.
  - Picking the stale copy backs it up, and later runs of that archive keep backing it up. "All of them", which is preselected, covers both.
- **Fix:** when two profiles' words collide, say more: the whole folder name, or the folder the profile sits in. Put whatever tells rows apart where Windows' cut cannot hide it. Builder's call; red first with this test.

### SPF-03 (LOW, Linux): Thunderbird's and Evolution's copies of one address read alike

- **Claim:** only Thunderbird rows get the profile words (`picker.go:366`, `if r.tb`). The name-versus-folder rule also counts only Thunderbird rows (`picker.go:330-334`). An Evolution row says its `DisplayName=` from `~/.config/evolution/sources/<uid>.source` (`internal/source/evolution.go:332-367`). That is usually the address, as I understand Evolution's default (not checked).
- **Reproduction:** `TestAtkSPF03_OneAddressInThunderbirdAndEvolutionReadsAlike` is RED: `"qzsam@gmail.com (1.4 MB)"` (Thunderbird) and `"qzsam@gmail.com (1.4 MB) (2)"` (Evolution). HOME points at the test's own folder. Positive twin, green: the Evolution store says its display name.
- **Fix:** when rows of different mail programs read alike, each one names its program, for example "qzsam@gmail.com (Evolution, 1.4 MB)".

### SPF-04 (LOW): a folder named like an account's name takes the address from the account

- **Claim:** `picker.go:328-347` drops an account's name when it reads like another Thunderbird row's folder. MA-1462 describes this rule.
- **Reproduction:** `TestAtkSPF04_AFolderNamedLikeAnAccountTakesItsAddress` is RED in both sub-tests:
  - **(a)** A second profile in the same Profiles folder has one hand-edited prefs.js line, so nothing in it is read with certainty, and a folder `Mail/qzsam.walker@gmail.com`. The rows are `"imap.gmail-1.com (1.4 MB)"`, which is the real account, and `"qzsam.walker@gmail.com (6.3 MB)"`, which is the planted folder and carries no "Not in Thunderbird now:".
  - **(b)** A leftover folder of that name sits in the account's own profile. The live account's row becomes `"imap.gmail-1.com (1.4 MB)"`, and the only row with the address is `"Not in Thunderbird now: qzsam.walker@gmail.com (6.3 MB)"`.
  - Positive twin, green: the account alone says its address.
- **Weight:** this needs write access to the Thunderbird profiles folder, which already gives access to the mail. Thunderbird names folders after servers, so such a folder is planted or hand-made. The old list showed folder names, so case (a) looked the same before this change. The harm is a wrong pick.
- **Fix:** keep the account's name and give the other row the lead word "Folder ". Only MailArchive's own rows can start with it, because `needsLead` reserves "folder ".

### MAP-01 (LOW, Linux): in a locale that is not UTF-8, "All of them" cannot be chosen and a row spelled with "?" can take another row's pick

- **Claim:** `pickChoice` (`picker.go:482-501`) matches the answer by exact text first, then after Unicode normalisation (NFKC) and case folding.
  - On Linux the answer is the dialog program's output (`zenity run_unix.go:53`, `util.go:93-107`).
  - GNOME zenity prints the chosen row with `g_print` (`src/tree.c`: master 523-529, 3.44.0 769-771).
  - GLib 2.84 and older convert that text to the locale's character set, writing "?" for every character they cannot convert. See GLib 2.80.0 `gmessages.c:3391-3417` and `901-941` (the "?" fallback is at 925), and 2.84.0 at 3468.
  - GLib 2.86.0 converts only when the output is a terminal (`gprint.c:232`). MailArchive reads it through a pipe, so 2.86 is not affected.
- **Reproduction:** the answer is simulated through the `listDialog` test hook; this was not run on Linux.
  - `TestAtkMAP01a_TheAllRowAsAnASCIILocalePrintsIt` is RED. The answer "? All of them (and any added later)" matches no row. After 10 lists the step ends with "The list of mailboxes did not say which one was chosen…" and nothing is exported.
  - `TestAtkMAP01b_ARowSpelledWithQuestionMarksTakesTheOthersPick` is RED. Accounts "José@qz.example" and "Jos?@qz.example" are in two profiles, 1.4 MB each. Picking José's row is answered "Jos?@qz.example (1.4 MB)", which gives the other profile's store.
  - Positive twins, green.
- **Weight:** this needs a locale whose character set is not UTF-8 (LANG unset, C or POSIX). Desktop sessions usually set a UTF-8 locale (general knowledge, not checked). Getting the wrong store also needs a second row spelled with "?". Any other answer that matches nothing is asked again, which is safe. The old list took such an answer as a path. qarma and matedialog were not checked.
- **Fix:** give the dialog program a UTF-8 locale when the wizard's own locale is not UTF-8, for example `LC_ALL=C.UTF-8` in the environment the dialogs inherit. Prove it with MA-1286's fake dialog programs recording their environment. Or, alternatively or as well, ask again when the exact match could equally be another row with "?" in place of its letters outside ASCII.

### FS-01 (LOW): a folder swapped for a link during the count is followed

- **Claim:** MA-1466 says the size count "follows no link". `storeSize` checks the store folder with Lstat (`picker.go:204`) and walks it with `filepath.WalkDir` (`picker.go:209`). WalkDir lists each folder with `os.ReadDir(path)` (Go 1.26.0 `path/filepath/path.go:319`), which opens the path and so follows a link that has just been put there.
- **Reproduction:** `TestAtkFS01_AFolderSwappedForALinkDuringTheCountIsFollowed` is RED. A goroutine swaps `z.sbd` for a link to another folder and back every 0.2 ms. On the 4th count (the 7th in an earlier run) the store counted 777,778,777 bytes, which includes a 777,777,777-byte file outside the store. Positive twins, green: 1,005 bytes with nothing swapped, and 1,000 with the link left in place.
- **Weight:** only directory listings and file sizes outside the store are read. No file is opened, and the result is only the size shown. It needs write access to the store plus a race, which gives an attacker nothing new. It does make the catalog's sentence untrue.
- **Fix:** walk through `os.Root` (`os.OpenRoot(dir)`, then `fs.WalkDir(root.FS(), ".", …)`), which refuses a path that leaves the folder. That is how the store's own reader keeps to its folder (`internal/source/localstore.go` scope, R25).

### PRV-01 (INFO, for the owner)

- **Linux and the BSDs:** the rows go to qarma, zenity or matedialog as command-line arguments (`zenity list_unix.go:19-20`, `run_unix.go:53`). The change's own Linux test shows this (`TestNoPickerRowReachesTheDialogProgramAsAnOption`).
- **What other local users can see:** they can read a process's arguments through `/proc/<pid>/cmdline` unless /proc is mounted with hidepid (Linux behaviour, not checked here). So while the list is open they can read every found account's name, usually its address, and its size.
- **Before this change:** the rows were folder names and Evolution display names.
- **Already shipped:** the end summary passes the run's account names the same way, as `--text` (`msg_unix.go:8`; the names come from `wizard.go:1944`).
- **Other platforms:** on macOS the script goes to osascript on standard input, not its arguments (`run_darwin.go:43-45`). On Windows the list is drawn inside MailArchive's own process.
- This is not a blocker by itself.

### SPF-05 (INFO)

`TestAtkSPF05_ASameScriptLookAlikeOfTheAllRow` is RED. "AII of them (and any added later)" (two capital i's), "A11 of them…" and "AlI of them…" get no lead word. Each row still maps to its own mailbox, comes after the real All row and has no arrow. The documented limit (MA-1464, MA-1287's R2-02) should also name same-script look-alikes.

### FS-02 (INFO)

- **Claim:** the 3-second limit (`picker.go:46, 67`) is checked between entries only (`picker.go:217`). Two things fall outside it:
  - Each Thunderbird row re-reads prefs.js and user.js in full (`picker.go:170`), and other rows open their reader (`picker.go:186`).
  - WalkDir reads each folder's whole listing before the 200,000-entry bound or the deadline can act.
- **Measured** (`TestAtkFS02_TheBudgetDoesNotCoverTheSettingsReads`, green): one read of a 15 MiB prefs.js takes 21-29 ms, and 40 rows take 0.79-0.87 s. About 150 such folders would pass 3 seconds; that is my extrapolation. A single huge folder was not measured.

### FS-03 (INFO)

The " (2)", " (3)" loop (`picker.go:464-472`) tries each number in turn. `TestAtkFSInfo_ManyRowsAlike`: 1,000 rows that read alike take 76-95 ms, and 3,000 take 0.7 s. It needs thousands of planted folders.

### FS-04 (INFO, older than this change)

An Evolution row's name comes from `source.Open`, which reads `~/.config/evolution/sources/<uid>.source` with `os.ReadFile` (`internal/source/evolution.go:357`). A named pipe there is waited on, so the list never appears. `TestAtkINFO_AnEvolutionSourceFileThatIsAPipeHoldsTheListBack` is RED (no answer in 3 seconds). The removed `autoInputLabel` made the same call. Fix: read the file with `util.ReadRegularFile`, as prefs.js is read.

## Held

**SPF (spoofing):**
- `TestAtkSPFHeld_RandomHostileNames`: 400 random lists built from options, arrows, bidi and zero-width characters, NUL, bytes that are not UTF-8, MailArchive's own words and 10 KiB names. Every row starts with A to Z, a to z or 0 to 9. None holds a control or format character or a line separator, so no NUL reaches `syscall.StringToUTF16Ptr` (`util_windows.go:28`). Every row is valid UTF-8, none starts with the All row's words, no two read alike, and each maps back to itself.
- `TestAtkSPFHeld_HostileNamesWrittenIntoPrefsJS`: the same kinds of names written into a real prefs.js, end to end.
- Rows are plain text in GNOME zenity 3.44 (`tree.c:598-602`) and 4 (`zenity-tree-column-view.c:665`).
- The prompt and the title are fixed words.

**MAP (mapping):**
- `TestAtkMAPHeld_EveryAnswerMapsToItsOwnRowOrNone`:
  - each row's exact text, with a line break added, with spaces around it, in decomposed Unicode forms (NFD, NFKD) or in another letter case, gives that row;
  - "", a row cut by one letter or with one added, a path, "➤" and the lead words give no row;
  - All is reached by its text, its words without the arrow, and those words in capitals;
  - 300 random answers through `pickAutoInputs` give a found store, all of them, or nothing.
- Windows returns the item by its position in the list given, and the list is unsorted (`list_windows.go:89, 224-225`). macOS returns the item `chooseFromList` gives back (`list.gojs:6, 13`).

**FS (file system):**
- `TestAtkFSHeld_LinksLoopsAndPipesAreNotFollowedOrOpened`: links pointing out of the store, links that loop back, a named pipe, a linked .pst file and a pipe named like a .pst file.
- `TestAtkFSHeld_PipedSettingsFilesDoNotHoldTheListBack`: user.js, prefs.js or compatibility.ini as a named pipe.
- Windows junctions: by Go 1.26.0 `os/types_windows.go:184-229`, a junction is never treated as a folder, so it is not entered or counted. I read this; I did not run it.

**PRV (privacy):**
- `TestAtkPRVHeld_TheWordsShownAreKeptNowhere`: for every row and for All, no file is written under the settings folders, nothing changes in the profile, the job holds neither name, and the store's own name is its folder. `picker.go` has no log call. I did not re-check the run's own log (MA-1288).

## Catalog rows MA-1462 to MA-1466: claims not true of the code

- **MA-1462:** "A to Z within each" is really character-code order of the case-folded words (`picker.go:459`). Letters outside A to Z (é, other scripts) sort after z. A row with a lead word sorts by the words after its lead word. The profile words can also be the same for two profiles (SPF-02).
- **MA-1463:** true of the code.
- **MA-1464:**
  - The right-to-left sentence is untrue for a name that starts with a digit (SPF-01).
  - "Never two rows alike" holds for whole texts after normalisation and case folding. Rows still read alike apart from " (2)" or their size (SPF-02, SPF-03), and on Windows the end of a long row is not drawn.
  - The documented limit should also name same-script look-alikes (SPF-05).
- **MA-1465:** true of the code, except for MAP-01's locale case on Linux.
- **MA-1466:**
  - "Follows no link" is untrue during a swap (FS-01).
  - "All of them after 3 seconds" is checked only between entries (FS-02).
  - `storeSize`'s comment (`picker.go:200-202`) says the count "stops … when something below cannot be read". The code keeps counting and marks the size as "at least" (`picker.go:213-215`).
  - .dat files are left out by their extension (`picker.go:227-229`), so an IMAP folder named "Notes.dat" is not counted.
  - The sentences about Finder, GNOME Files and File Explorer were not checked.
- **Release notes, "No two lines read the same":** each line does pick its own mailbox, but lines can read the same apart from " (2)" or their size.

## Checks to hand back

- Re-run `go -C <review copy> test -count=1 -run TestAtk -v ./internal/wizard/`. The expected reds are the eleven tests listed above.
- On a Linux machine with GLib 2.84 or older, run `LANG=C zenity --list --hide-header --column= "➤ x" "José"` and pick each row. Each should print with "?", which is MAP-01's premise.
- On the Windows test PC, set up two `*.default-release` profiles holding one address, as in MA-1467's lab item, to see whether the two rows look identical.

Counters: Register: finding (review report). Borrowed lines: short attributed quotes of the catalog and the cited source. Drift: none I can point to. Touched: four untracked test files and a gitignored folder of downloaded sources (`.claude/worktrees/zz-src/`) in the review copy, no product file. Persona: none. Source reached: the code, tests, catalog, zenity v0.10.12, GNOME zenity, GLib and Go at the cited versions. Not reached: the platforms' actual drawing, distribution GLib versions, Evolution's default display name, Ubuntu's snap profile copy.

Files are in /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/picker-adv:
- internal/wizard/zz_attack_picker_spf_test.go
- internal/wizard/zz_attack_picker_map_test.go
- internal/wizard/zz_attack_picker_fs_test.go
- internal/wizard/zz_attack_picker_prv_test.go
- .claude/worktrees/zz-src/

---

## Fold (the lead, 2026-11-18)

Each finding below was read against the code before it was folded; each fix has its own test,
written to fail on the code before the fold, and its own mutation in the round-2 sweep
(`export/mutate/sweep2.py`, not committed; results in the fold commit's message). The reviewer's
attack files stay in the review copy, not committed.

| ID | Verdict | Change | Tombstone test | Mutation |
|---|---|---|---|---|
| SPF-01 | Fixed | `needsLead` also gives the lead word when the first letter with a direction runs right to left (`golang.org/x/text/unicode/bidi`, of a module the repo already uses). | `TestEveryRowIsOneSourceNeverTheAllRowNeverAnOption`: "1שלום@qz.example", "2024 مرحبا", folder "7שלום", file "1שלום.pst"; twins that keep no lead word: "2nd account@qz.example", "Alison@qz.example" | N6 |
| SPF-02 | Fixed, with the friction review's F2 | Rows of two profiles that read alike say when each copy's mail last changed ("last changed today", "Mar 2, 2024"); two changed the same day add the profile folder's short name, and its whole name when the short ones match; " (2)" after that. | `TestOneAddressInTwoProfilesSaysWhichChangedLast` (real file times; the snap-style copy with one folder name in two places); `TestNamesFollowTheSharedNameRuleWithinAProfile` ("two profiles that end alike") | N2, N3, N16 |
| SPF-03 | Fixed, with F1 | When the mailboxes found belong to more than one of Outlook, Thunderbird and Evolution, Outlook's rows start "Outlook: " and Evolution's "Evolution: " (new `source.IsEvolutionStore`, the reader's own test). Both words are reserved row starts, so an account named "Outlook: x" starts "Account ". | `TestTwoProgramsSayWhichIsWhich` (a real Evolution cache store and its settings file) | N1 |
| SPF-04 | Fixed as suggested | The account keeps its name; a row said by its folder's or file's name that reads like another row's account name starts with "Folder " or "File ". The first build's rule, which dropped the name instead, is removed. | `TestNamesFollowTheSharedNameRuleWithinAProfile`: "a name like another row's folder", "a folder named like an address" | N8, N15 |
| MAP-01 | Fixed by the second route | On Linux and the BSDs, when the locale (LC_ALL, else LC_CTYPE, else LANG) does not name UTF-8, an answer holding "?" is the row it fits, with each character outside ASCII as "?", only when it fits exactly one (the All row among them); two fits give no row and the list is asked again. Not chosen: giving the dialog programs a UTF-8 locale, since the wizard's environment is MailArchive Desktop's own when the list opens from Make a new archive…, and a Thunderbird it starts later inherits it. Known limit, in MA-1465: a character set other than ASCII (ISO-8859-1) prints other bytes, which match no row and are asked again. | `TestTheAnswerMapsBackToItsRow` (the transcoded answers and the locale table) | N9, N10, N11 |
| FS-01 | Fixed as suggested | `storeSize` walks through an `os.Root` opened on the store (`fs.WalkDir(root.FS(), ".", …)`), after checking that the folder opened is the one looked at (`os.SameFile`). | `TestAFolderSwappedForALinkIsNotFollowed` (macOS, Linux, BSDs): the reviewer's race, 2 seconds of counts, never followed; plus a structural check of the walk | N12, N13 |
| PRV-01 | The owner's call; recorded | Not changed. Stated in MA-1462's row: on Linux and the BSDs the rows, with each address and size, are the dialog program's arguments while the list is open; the end summary has passed names the same way since 1.7.0. | — | — |
| SPF-05 | Fixed beyond the INFO | ASCII look-alikes (1, i and \| as l; 0 as o) are folded on both sides before a row's start is compared with MailArchive's own words, so "AII of them", "A11 of them" and "AlI of them" start "Account ". Other scripts' look-alikes stay MA-1287's documented limit. | `TestEveryRowIsOneSourceNeverTheAllRowNeverAnOption` | N7 |
| FS-02 | Noted | MA-1466 now says the 3 seconds bound the counts only, checked at each entry. Reading prefs.js once per profile is later work. | — | — |
| FS-03 | Noted | Needs thousands of planted folders. | — | — |
| FS-04 | Noted, older than this change | The Evolution settings read (`os.ReadFile` of `<uid>.source`) is its own row later: through `util.ReadRegularFile`. | — | — |
| Catalog and notes | Fixed | MA-1462's order words; MA-1464's right-to-left sentence (now true), "never two rows alike" (now "no two rows are the same text"), the look-alike limit; MA-1465's locale case; MA-1466's link, budget and .dat sentences, and `storeSize`'s comment; the release notes' "No two lines read the same". | — | — |

Found while folding, outside this change: the mbox reader leaves out every file ending .dat, .json,
.sqlite or .html (`source.isMailboxCandidate`), so a Thunderbird mail folder named, say, "Notes.dat"
is never archived and nothing says so. That is an R1 question for its own row.
