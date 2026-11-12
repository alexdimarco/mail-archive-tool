# Short adversarial confirm: the C3 account-name fixes (N-01 to N-04) at 8639d5f

**Verdict: NOT CLEAN (2 LOW; no HIGH, no MEDIUM).** N-01 holds for GNOME zenity and MATE's
matedialog. N-02 to N-04 hold for what they were written against. Two LOW findings remain:

- **R2-01 (LOW, Linux and the BSDs where qarma is installed; new in 8639d5f).** The ncruces/zenity
  module runs **qarma** before zenity when both are installed. qarma shows a list's text as plain
  text, so the escaping that `listText` now adds shows up literally. For example, `AT&T` shows as
  `AT&amp;T`, `Pat's` as `Pat&#39;s`, `"` as `&quot;` and `\` as `\\`. Every launch health view is
  hit, because its Fixity line always quotes a command (`verify -out "…"`). Anyone who copies that
  command gets one that does not work. Before 8639d5f, qarma showed the same text correctly.
- **R2-02 (LOW, extends the documented limit).** The record says "letters of another script
  (Cyrillic)". The real gap is wider: Unicode look-alikes in *any* script still pass. These include
  Latin `ɑ` and `ɡ`, the hyphens U+2010 and U+2011, and dotless `ı` plus a combining dot. They pass
  both the shared-name rule and the fallback-words rule. So do Greek letters and a name that drops
  the leading "the".

No product code was changed by this pass.

## Header

- **Reviewed:** branch `tb-completeness` at `8639d5f` (worktree `git pull` said "Already up to
  date"). Scope: `git show 8639d5f`, which covers `internal/wizard/listtext.go`, the `zenity.List`
  call sites, `internal/state/tbcompleteness.go` (`tbNameKey`, `TBNameIsFallback`,
  `dropSharedNames`) and `cmd/mailarchive/main.go` (`summaryOnScreen`). It also covers the surfaces
  those feed.
- **Process:** a `../assurance-kit/process/` adversarial pass, short and narrow, as briefed. One
  reviewer, who wrote neither the fixes nor round 1.
- **Clone:** `.claude/attack-c3-name-r2`, a git clone of the worktree at `8639d5f`. Nothing there is
  committed. Round 1's attack files were copied in from `.claude/attack-c3-name` unchanged; that
  clone was only read. The new files are:
  - `internal/app/zz_attack_r2_test.go`: the health-text feed and the real-command log probe.
  - `internal/wizard/zz_attack_r2_test.go`: zenity and qarma models.
  - `internal/state/zz_attack_r2_test.go`: look-alike names.

  Each test asserts the safe property, so a red test is a confirmed finding. The probes use real
  temporary Thunderbird profiles (`prefs.js`, `folderCache.json`, mbox and `.msf` files) and real
  archive folders. The binary was built from the clone (`.atk/mailarchive`). No real profile was
  touched.
- **Commands** (Go 1.26.0, darwin/arm64, 2026-11-09):
  - `go test -count=1 -run TestAtk ./cmd/mailarchive/ ./internal/app/`
  - `env ATK_TEXT=… ATK_BIN=… go test -count=1 -v -run TestR2Atk ./internal/app/ ./internal/wizard/ ./internal/state/`
  - the builder's MA-1286/1287/1288 tests, all green.
- **Sources outside the repo:**
  - ncruces/zenity v0.10.12 from the module cache: `list_unix.go`, `msg_unix.go`, `entry_unix.go`,
    `progress_unix.go`, `notify_unix.go` and `internal/zenutil/run_unix.go`.
  - GNOME zenity `src/tree.c` (master and the `zenity-3-44` branch), `src/zenity-tree-column-view.c`
    (master) and `src/msg.c` (`zenity-3-44`), read on gitlab.gnome.org.
  - mate-dialogs `src/tree.c` (master) and qarma `Qarma.cpp` (master), read on GitHub.
  - Qt's `Qt::mightBeRichText` (qtbase `src/gui/text/qtextdocument.cpp`, dev).

  All were read on 2026-11-09. No Linux desktop was run, so the qarma and GTK results are from
  models built on that source, not from screens.

## 1. Round 1's attack tests, rerun against 8639d5f

| Test | Round 1 | Now | Reading |
|---|---|---|---|
| ATK-1 (the health text holds the name raw) | red | red, **as designed** | The fix escapes downstream, in `wizard.listText`. The upstream text is meant to stay raw. Adapted as R2-ATK-1 (§2): green for zenity. |
| ATK-2 (a name imitating another account's folder words) | red | **green** | On screen and from the record, the two accounts read apart. |
| ATK-3 (look-alike names) | red | NFC/NFD **green**; Cyrillic red | Cyrillic is the documented limit (MA-1287's skip encoder). See R2-02. |
| ATK-4, ATK-5, ATK-6 | green | green | Unchanged. |
| ATK-7 (the summary writes the name to a `-log`) | red | **green** | The log says "the Thunderbird account kept in the folder imap.qz.example". |

## 2. N-01: `wizard.listText`

**HELD: GNOME zenity and matedialog.** R2-ATK-1 starts from a real run whose account name in
`prefs.js` decodes to `AT&T <sam@att.example>\nBackup complete.\101 \\ "q" Pat's`, with an archive
folder `Sam & Pat's mail`. The test builds the health list's exact text (`"Backup health — " +
Posture + "\n\n" + Summary`). Positive twin: the raw text is broken markup in the builder's model
(`shownByZenityList`). The text from `listTextFor("linux", …)` shows exactly as written. That
includes the shell command `'…Pat'\''s mail'`, where the backslash survives `g_strcompress`.

The backslash edge cases also come back as written: a trailing lone `\`, `\\`, `\0`, `\x41`,
`A`, a backslash before a line break, `\&amp;`, `&#10;` and `&lt;b&gt;`. In mate-dialogs,
`tree.c` has the same `gtk_label_set_markup(GTK_LABEL(text), g_strcompress(...))`.

**HELD: the list's choices (the column values) are not markup, so not escaping them is right.**

- GNOME zenity 3.44 `tree.c` binds every text column with `"text"`. No column binds `"markup"`.
- GNOME zenity master renders cells with `gtk_label_set_text` (`zenity-tree-column-view.c`
  `factory_bind_cb`). The printed choice is the item's text as given.
- mate-dialogs binds `"text"`.
- qarma builds `QTreeWidgetItem`s from the strings, with no HTML step.

So the paths and labels in `pickAutoInputs`, and the fixed choices, show as written. The returned
value equals the label, and `byLabel` lookup keeps working. No fix is needed.

**HELD: the other Linux dialogs that show a name or path.**

- `zenity.Info`, `Warning`, `Error` and `Question` pass `--text TEXT --no-markup`
  (`msg_unix.go`). zenity's `msg.c` then calls `gtk_label_set_text(text)` with no `g_strcompress`.
- qarma reads `--no-markup` after `--text`, so it applies `labelText` first. Because qarma is run
  as `qarma`, `m_zenity` is false and the text is unchanged. It then sets `Qt::PlainText`, because
  `html` ends false.
- The wizard's `zenity.Entry`, `zenity.Progress` and `dlg.Text` texts are fixed words or counts,
  with no name or path. The same is true of the Desktop's `zenity.Entry`, which holds a link.
- `zenity.SelectFile` titles are fixed.
- No `zenity.Notify` call exists in product code.
- The end summary that names accounts (`exportSummary`) goes through `zenity.Question`, so it is
  held as above.

**HELD: the unescaped `"` survivor (mutation M21) is truly equivalent.** In Pango markup, as
GMarkup parses it, `"` and `'` are only special inside attribute values. In text content they are
plain characters, so leaving `"` unescaped changes nothing on screen. Escaping them is harmless
in GTK, but see R2-01 for qarma.

**CONFIRMED R2-01 (LOW, Linux and the BSDs with qarma installed; introduced by 8639d5f).**

```go
// R2-ATK-8, internal/wizard/zz_attack_r2_test.go (text = the real-run health text above)
shown, _ := shownByQarma(text)                       // positive twin: == text (raw shows as written)
shown, _  = shownByQarma(listTextFor("linux", text)) // red:
//   "…/Sam &amp; Pat&#39;s mail … verify -out &quot;…&quot; … AT&amp;T &lt;sam@att.example&gt;\\nBackup
//    complete.\\101 \\\\ &quot;q&quot; Pat&#39;s so far … MailArchive Desktop&#39;s Settings …"
```

How this happens:

- `zenutil/run_unix.go` looks for `qarma`, then `zenity`, then `matedialog`, and runs the first
  one it finds, with argv[0] set to `qarma`.
- In qarma's `showList`, the text goes through `lbl->setText(labelText(text))`. Because
  `m_zenity = binary.endsWith("zenity")` is false, `labelText` returns the text unchanged. The
  label has no `setTextFormat`, so it keeps QLabel's default `Qt::AutoText`.
- `Qt::mightBeRichText` looks only at the text up to the first line break. The first line is the
  fixed `Backup health — POSTURE`, so the text is shown as plain text.

So every entity and doubled backslash that `listText` adds is shown literally. The Fixity line's
`verify -out "…"` command appears on every launch health view, and it always becomes `&quot;…&quot;`.
Paths with `&` or `'` and account names get the same treatment. Before 8639d5f, qarma showed the text
as written, because a name's markup could never reach the first line.

No WARN is hidden, and nothing restyles. The text is still readable, but it is visibly broken, and
a copied command fails.

The fixed list questions (`followLinksQuestion`, `scheduleQuestionFor("linux")`, the open-app
question, `presyncQuestionText`) hold no `& < > " '`, so they are not changed. Not seen on a real
qarma: the lab check is to install qarma on a Linux desktop, schedule a backup, and open
`mailarchive wizard`.

Proposed fixes, for the builder. Neither changes a returned value.

- **(a), the more robust one.** Show the health text in a message dialog: `zenity.Info`, or
  `Question` with extra buttons. Those dialogs pass `--no-markup`, which holds on zenity (no
  `g_strcompress`) and on qarma (`PlainText`). Then put a fixed prompt on the list of choices, so
  no variable text ever reaches a list dialog. With that, `listText` can stay as a guard for fixed
  words.
- **(b)** Make `listText` follow the tool: use the module's own order (`exec.LookPath` on `qarma`,
  then `zenity`, then `matedialog`). Escape only when the tool found is zenity or matedialog, and
  pass the text unchanged for qarma. That copies a module detail, which can change when the module
  version changes.

Either way, MA-1286's row ("shown as written on Linux and the BSDs") should say which tools it
covers.

**INFO (not findings of this slice; not tested).**

- A list choice that starts with `--` is an argv item after `--column=`. Both zenity's GOption
  parser and qarma's argument loop may read it as an option. `pickAutoInputs` labels are file and
  store names, so a `.pst` named `--text=x.pst` is the case.
- GMarkup refuses text that is not valid UTF-8. A Linux archive path made of non-UTF-8 bytes in
  the `Archive:` line would blank the label. `listText` passes invalid bytes through. The builder's
  `encoding/xml` model does not see this, because Go decodes a bad byte as U+FFFD.

Both predate C3.

## 3. N-02: names that imitate the fallback words

**HELD** (dropped, so account A is said by its own folder), each through
`TBDistinctNames` and `TBLines`:

- leading and trailing spaces;
- all upper case;
- U+200B and U+2060 inside words;
- a soft hyphen;
- a no-break space and an ideographic space;
- mathematical-bold `𝐭𝐡𝐞`;
- a full-width `ｔ`.

The builder's tests also cover `saneTB` and `saneTBCompact`, and round 1's ATK-2 is green through a
real run and the record.

**Read apart, so not a finding.** `the` joined to `Thunderbird` by U+2800. Cleaning strips the
character without adding a space, so the line says `theThunderbird account…`. That is visibly
different.

**CONFIRMED (part of R2-02, LOW).** Each of these names is kept, and its line reads almost the same
as account B's `… messages in the Thunderbird account kept in the folder imap.b.example so far.`:

| Name given to account A | Its line |
|---|---|
| `Тhe Thunderbird account kept in the folder imap.b.example` (Cyrillic Т; the documented class) | `… messages in Тhe Thunderbird account kept in the folder imap.b.example so far.` |
| `the Thunderbird account kept in the fοlder imap.b.example` (Greek ο) | same shape |
| `the Thunderbird ɑccount kept in the folder imap.b.example` (Latin ɑ, U+0251: same script) | same shape |
| `Thunderbird account kept in the folder imap.b.example` (no leading "the") | `… messages in Thunderbird account kept in the folder imap.b.example so far.` |

`this account's mail` is also kept. Its line, `… messages in this account's mail so far.`, names no
folder. It does not imitate a particular account, because an account with no name always says its
folder words first. INFO only.

## 4. N-03: the same-name key (`tbNameKey`)

**HELD** (each pair is now one name): NFC beside NFD; the Kelvin sign `K` beside `K`; micro `µ`
beside Greek `μ`; U+034F (the combining grapheme joiner, stripped as invisible); one-dot leader `․`
beside `.`. The builder's tests add letter case, `ﬁ` beside `fi`, and `STRASSE` beside `straße`.

**CONFIRMED (part of R2-02, LOW): pairs that render the same and are still two names.**

| Pair | Class |
|---|---|
| `sam@x.example` / `sаm@x.example` (Cyrillic а) | documented (another script) |
| `sam@x.example` / `sɑm@x.example` (Latin ɑ U+0251) | **same script, not documented** |
| `greg@x.example` / `ɡreg@x.example` (Latin ɡ U+0261) | **same script** |
| `sam-lee@x.example` / `sam‐lee@x.example` (U+2010), and with U+2011 | **punctuation; NFKC leaves U+2010 as it is and maps U+2011 to U+2010** |
| `kim@x.example` / `kı̇m@x.example` (dotless ı + U+0307) | **same script** |

Each needs a name someone chose on purpose, and both accounts in one run. The documented limit
should say "characters Unicode lists as confusable, in any script". Folding them needs Unicode's
TR39 "skeleton", which `golang.org/x/text` does not provide. A small table for the hyphen family
and the common Latin cases would close the cheapest gaps.

**HELD: two different real addresses that now fold together fail safe.** `info@straße.example`
and `info@strasse.example` are different IDN domains. Both names are cleared, and each account is
said by its own folder (`imap.one.example`, `imap.two.example`), so the two read apart. The only
cost is a friendlier name that is lost.

**INFO.** Accounts named `Sam` and `Sam's mail` share one sentence: "Thunderbird's own settings keep
only part of Sam's mail on this computer". Their headlines differ (`… messages in Sam.` and
`… messages in Sam's mail.`), so each block still says which account it is. The second block then
says `open Sam's mail's "Synchronization & Storage" page`, which is awkward but correct.

## 5. N-04: where the name can reach a file

**HELD, end to end on the real command** (R2-ATK-7e, the binary built from 8639d5f, a real
profile whose account has a name):

- **`-log` outside the archive:** the log holds the folder words and no address.
- **stderr sent to a pipe (not a terminal):** the folder words.
- **Positive twin:** stderr on a real pseudo-terminal (`internal/tty/ptytest`) shows
  `… messages in qzsam@qz.example so far`. The plaintext record holds the name, as MA-1284 says.

`-log` cannot be pointed at a terminal: `runlog.Open` refuses anything that is not a regular file,
so `-log /dev/tty` is refused.

**HELD, by reading the code:**

- **Scheduled run-job (`RunHeadless`):** `logSummary` writes counts only. A failure goes to
  `jobNote` (`.jobfail.log`) and stderr as `err.Error()`. No error string in the app,
  `tbcomplete` or `storefacts` formats `Name`. `Name` is used only at
  `app/tbcompleteness.go:207` (into the verdict input) and `:317` (into the record).
- **Desktop Run now:** the note (`runNowSummaryFor`, `tbNoteLines`) is returned to the page, not
  logged. Its partial-run note (`partialNote`) holds counts only.
- **Wizard:** its `mailarchive.log` gets only `app.Run`'s own lines. The end summary with names
  goes to `zenity.Question`.
- **Crash or panic output:** no code passes `Name` to `panic`. Go's tracebacks print string
  arguments as pointers, not text.
- **Encrypted runs:**
  - The command line's keyed run (`main.go:445`) uses the same `printSummaryLed`, so the rule
    above applies.
  - Desktop's encrypted Run now keeps the note in memory for the page (O1).
  - Nothing in an encrypted archive's folder holds the name (the builder's
    `TestAnEncryptedArchiveWritesNoName` and `TestAnEncryptedRunWritesNoName`).
- **Email alerts, archive index, schedule files:** `internal/backupalert`, `internal/archindex` and
  `internal/schedule` use no completeness type.
- **Run history:** only the plaintext record and the history in the archive folder hold the name
  (MA-1284, accepted).

**INFO.** `mailarchive status > file` (or `status -json > file`) writes the name into that file.
It reads the plaintext record, which is already in the archive folder, and the person chose the
redirect. This matches the lead's rule, which covers the run's summary and not `status`.

## 6. Gate state

- This pass changed no product code.
- MA-1286's Linux lab check (an account named "AT&T Mail" on a real zenity) is still owed. If
  R2-01 is fixed by option (b), a qarma lab check is owed as well.
