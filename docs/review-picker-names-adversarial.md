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

---

## Re-verification, round 2 (the reviewer, 2026-11-18)

*The same reviewer re-ran its attack files on the fold, commit 2217a76, and tried to break the
fold's new code. Its report, word for word:*

**Verdict: NOT CLEAN (0 HIGH, 0 MEDIUM, 2 LOW, 2 INFO).** The fold closes SPF-01, SPF-03, SPF-04, SPF-05 and FS-01 as tested, but two LOW gaps remain:
- **R2-01:** the new root-bound size walk still reads each file's size by its path, so a swapped folder is still followed.
- **R2-02:** the "?" rule decides from the locale's name, not the locale Linux actually loaded.

No product code was changed by this pass.

## 1. Round-1 attack tests on 2217a76

I changed only the call shapes (`storeSize` now returns four values, `pickChoice` takes `false` for the third argument). One run of `go -C <copy> test -count=1 -run TestAtk -v ./internal/wizard/` exited 1.

**Turned green:**
- SPF-01, SPF-03 and SPF-05.
- SPF-04, both sub-tests.
- FS-01: 2,527 counts under the swap race, none followed the link.
- The round-1 held tests stayed green, and the FS-02 and FS-03 timings are unchanged (40 rows 0.83 s; 3,000 rows alike 0.71 s).

**Still red, with why:**
- **SPF-02, one of its two cases.** "Two `*.default-release` profiles in one Profiles folder" is green: the whole folder names now differ. "One profile copied whole into a second Thunderbird folder, both changed today" is red. The rows are `"qzsam@gmail.com (1.4 MB, last changed today, profile abcd1234.default-release)"` and the same with `" (2)"`. That is the fold's stated last resort (see R2-04).
- **MAP-01a and MAP-01b, on this Mac only.** They go through `pickAutoInputs`, which turns the "?" rule on only for Linux and the BSDs (`answerMayBeTranscoded(runtime.GOOS, …)`, `picker.go:76`). These tests feed a Linux-style answer into a macOS run, where a real dialog never answers that way. The fold's rule itself holds when called with `transcoded` set to true: R2-02's positive twin with LANG=C gives All for "? All of them…" and gives no row for the José/Jos? pair.
- **FS-04 (older Evolution named-pipe hang):** red, as expected; not folded.

The fold's own 14 picker tests all passed here (exit 0). `GOOS=linux go vet ./internal/wizard/` with every attack file also passed (exit 0).

## 2. Findings

### R2-01 (LOW): the root-bound walk still reads each file's size by its path, so a folder swapped after it is listed is followed

- **Claim:** `storeSize` lists folders through the `os.Root` (`picker.go:252`), but reads sizes and times with `d.Info()` (`picker.go:274`).
  - In Go 1.26.0, `unixDirent.Info` is `lstat(d.parent + "/" + d.name)` (`src/os/file_unix.go:457-461`).
  - The folder's name there is the joined path string the Root gave it (`src/os/root_unix.go:106`; `rootFS.ReadDir` at `src/os/root.go:375-397`).
  - So the lookup goes through the path again. If `z.sbd` becomes a link after it is listed, each file of the same name in the link's target is read.
- **Reproduction:** `TestAtkR2FS01_EachFilesSizeIsLookedUpByItsPath`, RED.
  - Setup: `z.sbd` holds 1,000 files of 1 byte; a folder outside the store holds the same 1,000 names at 1,000,000 bytes each (sparse, no disk used), dated 2031.
  - The first count under the swap race read 313,000,430 bytes (the store holds 1,005). Its newest file time was 2031, so the "last changed" words take the outside time too.
  - Positive twins, green: 1,005 bytes with nothing swapped; 5 bytes with the link left in place.
- **Why the fold's tombstone missed it:** `TestAFolderSwappedForALinkIsNotFollowed` puts a file named `BIG` outside, a name the store does not have.
- **Weight:** as for FS-01. Only metadata is read: sizes and file times, no file contents. It needs write access to the store plus a race. It makes MA-1466's "never followed out of the store" untrue again.
- **Fix:** read each file through the Root by its path inside it: `root.Lstat(p)` instead of `d.Info()`. `root.Lstat` refuses a path that leaves the root. Give the tombstone same-named outside files, as this test does.

### R2-02 (LOW, Linux): the "?" rule decides from the locale's name; GLib decides from the locale the C library actually loaded

- **Claim:** `answerMayBeTranscoded` (`picker.go:690-705`) treats a locale whose name contains "UTF-8" as untranscoded. GLib instead goes by the C library's actual character set, `nl_langinfo(CODESET)` (GLib 2.80.0 `glib/gcharset.c:189-192` and `203-214`; on Unix the console character set is the same thing, `:435`). That is "the C locale if no locale has been loaded by setlocale()". The `CHARSET` environment variable overrides both (`:120-130`).
  - Cases where the name says UTF-8 but the loaded character set is ASCII:
    - a UTF-8 locale that is named but never installed (glibc's setlocale fails, and GTK warns "Locale not supported by C library");
    - `LC_CTYPE=UTF-8` as macOS sends it over ssh, which is not a Linux locale name.
  - In both, GLib 2.84 and older print "?" (MAP-01's premise), while `pickChoice` matches exactly.
- **Reproduction:** `TestAtkR2MAP02_ALocaleNamedUTF8ThatIsNotLoaded`, RED for `LANG=en_GB.UTF-8` and `LC_CTYPE=UTF-8`.
  - The All row's answer "? All of them (and any added later)" gives no row, so All cannot be chosen.
  - Picking `"José@qz.example (1.4 MB)"`, answered `"Jos?@qz.example (1.4 MB)"`, gives the other profile's store.
  - Positive twins, green: with LANG=C, All maps and the pair gives no row.
  - The premise comes from GLib's source. glibc's failing setlocale was not run here.
- **Fix:** on Linux and the BSDs, apply the "?" rule to every answer that holds "?", whatever the locale's name. The only cost: a row holding its own "?" that another row also fits is asked again, which is already the fold's behaviour in an ASCII locale. Asking the C library itself would need cgo, and the repo is pure Go.

### R2-03 (INFO): "last changed" says a time after now as a past day

- **Claim:** `pickChangedWords` (`picker.go:294-310`) says a file time later this year as, for example, "Dec 25", which reads as last Christmas when today is Nov 18. A later year comes out as a plain date ("Jan 2, 2031").
- **Weight:** the copy-in-use hint ("the one that changes") can be steered by anyone who can set a copy's file times, or by a clock that was wrong. It only changes the words shown.
- **Reproduction:** `TestAtkR2SPF03_ATimeAfterNowIsSaidAsThePast`, RED. Positive twins green: today, yesterday, a day earlier this year, and a later hour today.
- **Fix:** say any time after now as "today", or leave it as is. Builder's call.

### R2-04 (INFO, documented residual): two profiles' rows can still read alike

- **Linux:** one profile copied whole, with the same folder name in two Thunderbird folders, both changed the same day, reads alike apart from " (2)". This is the last resort MA-1462 itself describes (round-1 SPF-02, second case, red). Naming the place each profile sits in would close it.
- **Windows:** two `*.default-release` profiles holding one address with one size, both changed the same day, differ only after "last changed …". The roughly 38-character cut hides that part (MA-1467), so on screen the rows look identical.

## Held

- **`TestAtkR2Held_PrefixesHintsDirectionAndQuestionMarks`** — 600 random hostile lists, with prefixed, hinted and right-to-left rows present (positive twin):
  - "Outlook: " and "Evolution: " appear only on Outlook and Evolution rows.
  - No row runs right to left, and every row starts with an ordinary letter or digit.
  - Look-alike reserved words get a lead word: "0utlook:", "Evo1ution:", "Fi1e ", "AII of them".
  - No two rows are one text, and each row's own text maps to itself.
  - A row's answer as GLib prints it in an ASCII locale gives that row or none: never another row, never All. All's ASCII answer gives All.
- **`TestAtkR2Held_StoreUnusedOnlyForCertainLeftovers`:**
  - A live folder whose setting is in capitals, or whose name is in another Unicode form (NFD, setting in NFC), is never called unused, on macOS.
  - A hand-edited prefs.js, or a folder outside any profile, is never called unused.
  - A real leftover is unused (positive twin).
- **`storeUnused` in the prep loop:** by reading `wizard.go:858-864`. An unused IMAP store skips only the offline-download question and falls into no other case. "All of them" inputs go through the same loop.
- **Round-1 held tests, unchanged and green:**
  - `TestAtkSPFHeld_RandomHostileNames`
  - `TestAtkSPFHeld_HostileNamesWrittenIntoPrefsJS`
  - `TestAtkMAPHeld_EveryAnswerMapsToItsOwnRowOrNone`
  - `TestAtkFSHeld_LinksLoopsAndPipesAreNotFollowedOrOpened`
  - `TestAtkFSHeld_PipedSettingsFilesDoNotHoldTheListBack`
  - `TestAtkPRVHeld_TheWordsShownAreKeptNowhere`
- **PRV-01's facts still hold, one item added:** the "last changed" dates now also go into the Linux dialog program's arguments.

## Header

- **Reviewed:** `2217a76`, detached in `.claude/worktrees/picker-adv`; the fold is `git diff 9e3feae 2217a76 -- internal/`. Date: 2026-11-18. Go 1.26.0 darwin/arm64.
- **Commands and exit status:**
  - `git checkout --detach 2217a76`: 0. The three `git diff` commands: 0.
  - Round-1 `-run TestAtk`: 1 (as listed above).
  - `-run TestAtkR2`: 1 (R2-01, R2-02, R2-03 red; both held tests green).
  - The fold's 14 picker tests: 0. `GOOS=linux … vet`: 0.
  - `curl` downloads: 0.
- **Sources outside the repo, read with `curl` into the gitignored `.claude/worktrees/zz-src/`:**
  - Go go1.26.0: `src/os/file_unix.go`, `root.go`, `root_unix.go`, `dir_unix.go`, `dir_darwin.go`.
  - GLib 2.80.0: `glib/gcharset.c`.
  - Not read: the module cache, which is in the owner's home folder.
- **Not checked:** Linux, glibc's setlocale and Windows were not run; no dialog was opened.
- **Counters:** Register: finding (re-verification report). Borrowed lines: short attributed source quotes. Drift: none I can point to. Touched: in the review copy, the call shapes in my four round-1 attack files and a new untracked `internal/wizard/zz_attack_picker_r2_test.go` (no covers marker); no product file. Persona: none.

Files are in `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/picker-adv/internal/wizard/`:
- `zz_attack_picker_r2_test.go`
- `zz_attack_picker_spf_test.go`
- `zz_attack_picker_map_test.go`
- `zz_attack_picker_fs_test.go`
- `zz_attack_picker_prv_test.go`

## Fold, round 2 (the lead, 2026-11-18)

| ID | Verdict | Change | Tombstone test | Mutation |
|---|---|---|---|---|
| R2-01 | Fixed as suggested | Each file's size and time is read through the root by its path inside it (`root.Lstat(p)`), never by its joined path (`d.Info()`). A first version also left out a result that was not a regular file; no test can reach that branch without a race, and its mutation (R4) survived, so the branch was removed rather than shipped untested: `root.Lstat` does not follow a link, so such an entry adds at most its own few bytes. | `TestAFolderSwappedForALinkIsNotFollowed`, given the reviewer's sharper setup: the same 1,000 names outside the store, 1,000,000 bytes each and dated 2031; no count may exceed the store's 1,005 bytes or take a 2031 time; plus a structural check | R1 |
| R2-02 | Fixed as suggested | `answerMayBeTranscoded` is true on Linux and the BSDs whatever the locale's name, since GLib goes by the character set the C library loaded and pure Go cannot ask it. The cost, stated in MA-1465: a row spelled with its own "?" that another row also fits is asked again there. | `TestTheAnswerMapsBackToItsRow` | N9, N10, R2 |
| R2-03 | Fixed | A day after today is said with its year ("Dec 25, 2026"), never as a past day of this year; a later hour today is still "today". | `TestOneAddressInTwoProfilesSaysWhichChangedLast` (the date table) | R3 |
| R2-04 | Documented residual | MA-1462 names it: a profile copied whole into a second Thunderbird folder, both copies changed the same day, reads alike but for " (2)"; on Windows a long row's end, where the date sits, is not drawn (MA-1467). | — | — |
| PRV-01 addendum | Recorded | MA-1462's exposure sentence now names the "last changed" dates too. | — | — |
| MAP-01a/b on macOS | No change | As the reviewer says, those two attack tests feed a Linux answer into a macOS run, where the rule is off by design; the rule itself is proven with `transcoded` set. | — | — |

---

## Final look, round 3 (the reviewer, 2026-11-18)

*The same reviewer re-ran its attack files on commit 615ecb0 (the round-2 fold and the friction
re-walk's fold) and tried to break the new "Older copy:" and ASCII-uniqueness code. Its report,
word for word:*

**Verdict: NOT CLEAN (0 HIGH, 1 MEDIUM, 1 LOW, 2 INFO).** R2-01, R2-02 and R2-03 are closed. Two new problems come from the "Older copy:" logic: it calls mailboxes copies when they are not (R3-02), and a file dated in the future makes the copy in use read as the older one (R3-01). No product code was changed.

## Re-run on 615ecb0

One run of `go -C <copy> test -count=1 -run TestAtk -v ./internal/wizard/`, exit 1. I started it only after the other session's `go test -p 4` had ended. Free memory was 56%. That session was compiling a Windows test binary and starting the VM at the time, and its note expected the reviewer's targeted tests alongside.

- **Turned green:**
  - R2-01: 19,083 counts under the race, none read outside the store; FS-01: 3,958 counts.
  - R2-02, after the new call shape `answerMayBeTranscoded("linux")`: José's "?" answer gives José's store, and All's gives All.
  - R2-03: a time ahead of the clock now carries its year ("Dec 25, 2026").
  - SPF-01, 03, 04 and 05, and every held test from rounds 1 and 2.
- **Two adjustments beyond call shapes, said openly:**
  - R2-02's per-locale loop is gone, because the function no longer reads the locale.
  - R2-03's check was too narrow (it wanted "today" or "later"); it now checks the substance, that the words carry the year.
- **Still red:**
  - **SPF-02**, the "one profile copied whole, changed the same day" case: the two rows differ only by " (2)". This is documented as the last resort (R3-04).
  - **MAP-01a and MAP-01b** are test artifacts on macOS, not product faults. Both feed a Linux-style answer to a macOS run. MAP-01b also builds Linux labels, which now carry the extra " (2)" for rows alike in ASCII, then answers the macOS list with them. The Linux path itself is checked by R2-02, which is green.
  - **FS-04** (the older Evolution named-pipe hang), as expected.

## Findings

### R3-02 (MEDIUM): "Older copy:" is said of mailboxes that are not copies

- **Claim:** any two Thunderbird rows from two profiles whose words read alike get the label, by date alone (`picker.go:508-510`). But rows that read alike are often different mailboxes:
  - Every profile has its own Local Folders, named "Local Folders", holding its own mail. Feed accounts are the same.
  - Two different Gmail accounts said by their folder both read "imap.gmail.com".
- **Reproduction:** `TestAtkR3_02_TwoProfilesOwnMailboxesAreCalledCopies`, RED:
  - `"Older copy: Local Folders (5.0 MB, last changed Mar 9)"`: a work profile's own Local Folders with 5 MB of its own mail.
  - `"Older copy: imap.gmail.com (9.0 MB, last changed Mar 9)"`: a different account.
  - Positive twin, green: one profile alone gets no such label.
- **Why MEDIUM:** MailArchive's own words falsely call a distinct mailbox a duplicate, in a case anyone with two profiles hits (Local Folders). That invites leaving it out of a single-row pick. The row still maps to its own mailbox, and "All of them" still includes it.
- **Fix:** say "Older copy:" only when both rows are the same account by its settings (the same type, other than none or rss, the same hostname and the same name, read with certainty). Never say it for Local Folders, feeds, or rows said by their folder. Otherwise keep round 2's neutral "last changed" and profile words.

### R3-01 (LOW): a stale copy holding one file dated in the future makes the copy in use the "Older copy:"

- **Claim:** "newest" is the latest file time, summaries and .dat files included (`picker.go:286`). A copy is older than any copy with a later time (`:491-510`), and the newest sorts first (`:652`). A time after now counts as later.
- **Reproduction:** `TestAtkR3_01_ACopyDatedAheadOfTheClockCallsTheCopyInUseOlder`, RED. With the stale copy's INBOX.msf dated 2031, the rows are `"qzsam.walker@gmail.com (6.3 MB, last changed Jan 1, 2031)"` (the stale copy, first) and `"Older copy: qzsam.walker@gmail.com (14 MB, last changed today)"` (the copy in use). Positive twin, green: with the stale copy dated 2024, it is the older copy.
- **Weight:** needs a planted time or a profile carried over from a computer whose clock ran ahead. A person following the label backs up the stale copy, and later runs keep doing so.
- **Fix:** treat times after now as unknown when choosing the newest copy and the older ones.

### R3-03 (INFO, by design): a summary file rewritten today dates a stale copy

`TestAtkR3Info_ASummaryWrittenTodayDatesAStaleCopy` (log only): the stale copy's INBOX.msf written today makes it lead, and the copy in use, last changed yesterday, becomes `"Older copy: … (14 MB, last changed yesterday)"`. This is the R2-F4 design. It needs write access to the stale profile, or a program that opened it.

### R3-04 (INFO, documented residual)

One profile copied whole and changed the same day: no profile words, only " (2)", as MA-1462 states.

## Held

- **`TestAtkR3Held_OlderCopyWordsAndASCIIUniqueness`:**
  - "Older copy", in any case or with look-alike letters ("0lder copy", "Oider c0py", "older  copy"), as an account name, folder or Evolution store, never starts a row with "Older copy:".
  - Across 800 random Linux lists, every row's ASCII form is its own and differs from the All row's. Each row's exact and ASCII answers give that row.
  - "Older copy:" appears only on a Thunderbird row with a later row of the same words, and rows that read alike come newest first.
- **Earlier held tests:** the R2 held tests (prefixes, right-to-left, look-alikes, storeUnused) and all round-1 held tests are green. The FS-02 and FS-03 timings are unchanged.
- **Windows path handling (read, not run):** `root.Lstat` on the slash paths that `fs.WalkDir` gives works on Windows, because `os.Root` splits on both "/" and "\" (Go 1.26.0 `os/path_windows.go:19-22`, `os/root.go:301-330`).
- **"Thunderbird is a program only through its mail accounts" (`picker.go:455-465`):** checked by reading, plus the fold's own Local Folders and Outlook test.

New attack file: `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/picker-adv/internal/wizard/zz_attack_picker_r3_test.go`. I also edited `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/picker-adv/internal/wizard/zz_attack_picker_r2_test.go` as described. Both are untracked with no covers marker.

Counters: date 2026-11-18. Register: finding. Touched: those two files only. Persona: none. Source reached: `picker.go` at 615ecb0 and the Go 1.26.0 Windows path source. Not run: Linux, Windows.

## Fold, round 3 (the lead, 2026-11-18)

| ID | Verdict | Change | Tombstone test | Mutation |
|---|---|---|---|---|
| R3-02 | Fixed as suggested | "Older copy:" only for copies of one account: rows said by the account's name (read with certainty), of a mail account (never type none or rss: Local Folders, feeds), whose settings name the same server (`pickRow.host`, from `ReadStoreFacts`' account), in two profiles. Rows that merely read alike keep the neutral "last changed" and profile words. | `TestOnlyCopiesOfOneAccountAreCalledOlder`: each profile's own Local Folders, two accounts said by one folder name, one name on two servers; positive twin, one account in two profiles | P1, P4 |
| R3-01 | Fixed as suggested | A time after now is not known: it makes no copy the newest and none older, and sorts as no time. Its words still carry the year (R2-03). | `TestOnlyCopiesOfOneAccountAreCalledOlder` (a stale copy whose summary is dated 2031) | P2, P3 |
| R3-03 | By design; recorded | MA-1462 says it: a stale copy whose summary another program rewrote today reads as newest (R2-F4's trade; it needs another program to open the stale profile, or write access to it). | — | — |
| R3-04 | Documented residual | As MA-1462 states. | — | — |
