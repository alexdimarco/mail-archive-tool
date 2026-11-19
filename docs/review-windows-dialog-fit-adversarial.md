# Adversarial pass: Windows dialogs that fit their words

Date: 2026-11-19. Branch `windows-dialog-fit` (off main 17d730b). Procedure:
`assurance-kit/process/adversarial-review.md`.

- **Reviewed:** commit d09d178 (slices 1 to 4) by the finders; judged at 63a70d5 by the
  skeptics (63a70d5 changed only `msg_windows.go`, the lab test and the pins, none of
  which a finding cites).
- **Settled in:** commit 8bfe68b (code, pins, workflows) and the commit that files this
  report (design notes B6 to B10, catalog amendments to MA-1468, MA-1469 and MA-1470).
- **Who:** three finders, each with one lens: outside aggressor (OUT, hostile text and
  window messages), insider (INS, copy integrity and supply chain), integrity and
  concurrency (CON, threads, timing and resources). Then two skeptics, each told to
  refute: one for INS, reproducing in a throwaway clone; one for OUT and CON, from the
  source, Microsoft's documentation and a model of `fit.go`. All were fresh Claude
  instances working under no name, read-only, and saw no other's findings until their
  phase closed.
- **Tools:** go1.26.0; upstream's generator `mkwinsyscall` at x/sys v0.17.0; the office
  test PC, Windows 11 Professional 26H2 build 26300.9974, 1920x1080 at 150 %.
- **Digests:** the published module `h1:o4SErDa0kQijlqG6W4OYYzO6kA0fGu34uegvJGcMLBI=`; the
  copy's `lab-copy` at 8bfe68b `sha256:89f04cfe067b1499f90be9544c5fc32aef0dfc0b5feeb432db8c5a52518ad6f3`,
  and after the friction re-check's list change (G1, the commit after 8bfe68b, outside this
  pass) `sha256:1f77a47868199966d5196a25fdb2a59861bc6abb0642212145eaded84e190473`.

**19 found / 19 confirmed / 0 refuted.** Three pairs are one defect each (OUT-01 and
CON-02; OUT-04 and CON-01(c); OUT-05 and CON-03's width part), so 16 distinct defects;
INS-03 is confirmed in part. The skeptics rated none above MEDIUM: INS-01, INS-02 and
INS-05 MEDIUM; INS-03, INS-04, INS-06, INS-07 and OUT-01/CON-02 LOW; the rest INFO.

**Verdict of the pass:** no finding lets hostile text or another program's window message
crash, hang, read, write or change an answer beyond what v0.10.12 already allows. The
copy check (MA-1468) was the weak point: it could be passed by a hand-written line in the
generated file, a vendored or work-space copy, records that vouched for themselves, or a
stale lab text. Each confirmed finding is fixed or recorded below; two need nothing, and
three are upstream's or older than this change.

## Findings and what was done

| ID | Lens | Claim | Skeptic | Disposition | Tombstone |
|---|---|---|---|---|---|
| INS-01 | insider | The generated file's exemption let any `x.NewProc(f())` run code when the program starts, with every gate green (`zenity_copy_test.go`, `generatorLine`) | Confirmed by reproduction: re-recorded, all green, and the shipped `mailarchive.exe`'s start-up code calls the new function (disassembly). MEDIUM | **Fixed.** The generated file must be byte for byte what upstream's generator makes from the `//sys` lines; the exemption matches the generator's two shapes only | `TestTheZenityGeneratedFileIsTheGenerators` (its twin refuses one hand-written line); refusals "a generator line that calls", "a DLL loaded from the program's folder" |
| INS-02 | insider | MA-1468 read go.mod's text, not what the build resolves: a vendored copy, an `exclude`, or a `go.work` builds another zenity | Confirmed: a vendored, edited zenity compiled into `mailarchive.exe` with all green. MEDIUM. `go list -m` alone misses vendor mode | **Fixed.** `go list -f {{.Dir}}` must find the zenity packages a Windows build compiles in the copy, `go list -m` must name the replace, and `vendor/` must not exist | `TestTheBuildUsesTheZenityCopy` (its twin refuses a `go.work` that replaces zenity); refusal "a vendored zenity" |
| INS-03 | insider | Nothing forced MA-1470 to run again: the recorder stamped any lab text, and `fit.go` had no record | Confirmed in part: "MA-1470 not run, would pass" and "MA-1469 fit pass" were accepted; a receipt is no stronger against an insider than text. LOW | **Fixed, beyond the smallest fix.** A lab record must name MA-1470; the pins keep `lab-copy`, the hash of the copy's built files that the last passing lab run was built from, and the recorder takes it only from the receipt the lab test writes when all five tests pass. A receipt can be forged, as text can; it stops the honest mistake of re-recording after a change with yesterday's run | refusals "a Windows file changed after the lab run", "another row's lab run" |
| INS-04 | insider | Patch records vouched for themselves (their upstream imports and variables), a second record won, and any file could be patched | Confirmed: (a) a forged record passed, (b) a ninth patch file passed. LOW | **Fixed.** The eight patchable files, with each original's SHA-256, imports and start-up variables, are in the test's own table; a record must agree with it; a second record does not parse; patch files and `fit.go` may carry no compiler directive but build constraints | refusals "a file the table does not name", "a record that vouches for itself", "a patch file's imports", "a patch file's directive", "fit.go's directives"; the two-records parse check |
| INS-05 | insider | MA-1469 could disappear with every gate green: `fit_test.go` deleted or behind a build tag | Confirmed, including `//go:build windows`, an easy honest mistake. MEDIUM | **Fixed.** `fit_test.go` must exist and build on Windows, macOS and Linux; the runner reads `go test -json` and requires each MA-1469 test, by name and as declared in the file, to report a pass | refusals "no fit_test.go", "fit_test.go behind a build tag"; the runner's own check |
| INS-06 | insider | No test required the published-module scans, and the Windows scans targeted a program Windows does not ship | Confirmed: the scan lines deleted, all green. LOW | **Fixed.** Both workflows must run the scan on the whole module and, for Windows, on `./cmd/mailarchive` (the program that ships as `mailarchive.exe` and `mailarchivew.exe`); the script checks that the go.mod it scans resolves zenity to the published v0.10.12. Failing on any zenity advisory whatever its reach is left as an owner choice | `TestThePublishedZenityIsScanned` |
| INS-07 | insider | The copy is checked during the tests, then built later in the same job, so a test could change it in between (hidden with `skip-worktree`) | Confirmed, but older than this change and not specific to it. LOW | **Fixed in part.** Just before it builds, the release checks the copy again, runs `go mod verify` and refuses files hidden from git. The complete fix, a build job on a fresh runner after the test job, is recorded as a follow-up | the release step "The zenity copy, just before the build" |
| OUT-01, CON-02 | outside, integrity | After a DPI change the window was resized where it stood, then clamped into the work area of the monitor it was on by then, which during a drag between screens of different scaling can be the one it is leaving | Confirmed by mechanism, and the geometry reproduced in a model of `fit.go`. LOW (design §4.3's promise was false) | **Fixed.** The monitor is the one the suggested rectangle lies on (`MonitorFromRect`, generated); the controls are placed first, then the window in one move to the suggested corner, inside that monitor's work area. A dialog dragged across the edge now moves wholly onto the new screen when its DPI changes | MA-1470's corner check; its glue mutant (the corner ignored) fails seven cases. Which monitor is read cannot be told apart on a one-screen PC |
| OUT-02 | outside | One line of text that measures 0 wide gives upstream's layout, against §8's sentence | Confirmed as behaviour, which §4.3's Fallbacks specify; unreachable from MailArchive's callers. INFO | **Words fixed.** §8 names the exception; no code change | MA-1469 pins it ("a prompt line measuring 0 wide", "a row measuring 0 wide", and the twin with visible text) |
| OUT-03 | outside | The DPI handlers now read the suggested rectangle, a pointer, from the message | Confirmed by mechanism; a crash needs Windows not to copy the rectangle between processes (unverified). INFO | **No fix.** Only a program on the same desktop at equal or higher integrity can send the message (UIPI), and it can already end the process or answer the dialog | none |
| OUT-04, CON-01 | outside, integrity | A layout whose measurement failed (a GDI failure, or a DPI another program forged) kept the old boxes with the new fonts, or at opening applied nothing; a progress text that failed was not tried again | Confirmed by mechanism. INFO | **Fixed.** At the same DPI the last layout stands (at opening, the first); at another DPI the dialog falls back to upstream's layout and label style for good; a progress text is measured again with the next `Text` | by mechanism only: no failure can be caused in the lab |
| OUT-05, CON-03 | outside, integrity | The progress window could narrow again after a wide word, and its floor lost a pixel per DPI round trip | Confirmed in the arithmetic. INFO | **Fixed.** The width is kept with the height and both re-base rounding up. The lab then showed Windows breaks a word wider than the label inside it (`DT_EDITCONTROL`), so the width never changes there: the widening was never reachable on Windows | MA-1469 ("the progress width shrinks" and "the re-base rounds down" mutants red); MA-1470 checks the dialog never shrinks; the glue mutant that forgets the width survives, as it must |
| OUT-06 | outside | 32-bit arithmetic can overflow | Confirmed for 32-bit builds only. INFO | **No fix.** Windows ships 64-bit only (`build-release.sh`) | none |
| OUT-07 | outside | The lab test passed stack addresses to Windows through its own helpers, which Go's rule for `unsafe.Pointer` does not protect | Confirmed (escape analysis: the values stayed on the stack). INFO, test only | **Fixed.** The helpers carry `//go:uintptrescapes`, as `LazyProc.Call` does | escape analysis: every rectangle and buffer they pass now moves to the heap |
| CON-04 | integrity | Upstream's fallback layout moves the window without `SWP_NOACTIVATE` | Confirmed; upstream's lines, kept on purpose by §4.3's Fallbacks. INFO | **No fix** | none |
| CON-05 | integrity | Upstream binds EnumWindows to EnumChildWindows, so its "bring our own window forward" step finds nothing | Confirmed in the copy and upstream; byte-identical to v0.10.12. INFO | **No fix in this change.** MailArchive brings its dialogs forward itself (`internal/dialogfront`); binding the real call would change upstream's behaviour and needs its own review against that | none |

## Prove-fail record

- MA-1468, on the Mac: 13 mutants, each undoing one new check, each red (listed in
  8bfe68b's commit message); restored, green. The runner's pass check, in a
  scratch copy with `fit_test.go` behind `//go:build race`: red with the check, green
  without it. The workflow check: red on the previous `ci.yml`, `release.yml` and scan
  script, green on these.
- MA-1469: 24 of 24 mutants of `fit.go` red, 6 of them new.
- MA-1470, on the office test PC (lab-copy `sha256:89f04cfe…`): run 1 failed one new
  check only, "a word wider than 400 widens the progress dialog", which was wrong: Windows
  breaks the word inside the label and the dialog keeps 400. Rewritten to "never shrinks";
  run 2: all five lab tests pass and the receipt is written. Glue mutants: the corner
  ignored, red in seven cases; the progress width forgotten, survives (not observable).
- OUT-07: `go test -c -gcflags=-m` for Windows shows every value the helpers pass moved
  to the heap; before, the skeptic found them on the stack.

## Not fixed here, and why

- The complete INS-07 fix (a build job on a fresh runner) and a zenity-advisory policy
  whatever its reach (INS-06): release-pipeline changes older than this one, for the
  owner to schedule.
- CON-05: upstream's code; see the table.
- What no lab here can show: the monitor a DPI change reads (one screen), a drag across
  screens of different scaling, and the failure fallbacks (no GDI failure can be caused).
  The smallest check: on a PC with a 150 % laptop screen and a 100 % monitor, drag the
  wizard's first list slowly across the edge both ways.

## Re-verify of the fixes

A fresh-eyes finder (procedure §3), a fresh Claude instance working under no name, read
63a70d5 (which no finder of the pass saw) and 8bfe68b, ran the copy checks on a copy of
8bfe68b, and found no crash, hang or unbounded recursion in the Windows glue (the
fallback's second layout runs upstream's branch; the thread's DPI switch in 63a70d5 is
restored on every path and cannot leak to another goroutine). Its ten findings, all fixed
in the commit after daf68cb unless said otherwise:

| ID | Severity | Finding | Fix | Tombstone |
|---|---|---|---|---|
| RV-01 | MEDIUM | Windows writes go vet's paths with backslashes, so the one allowed finding was not recognised: a false red on every windows-latest run | the finding is read with either separator | `TestTheVetExceptionReadsEveryHostsPaths` |
| RV-02 | MEDIUM | A lab run filtered to one subtest (`-test.run "TestMA1470/hostile"`) passed all five tests by name and wrote a full receipt | a run with a subtest pattern or `-test.skip` writes none | on the test PC: the filtered run wrote none, the whole run one |
| RV-03 | LOW-MEDIUM | A receipt left by an earlier run survived a failing one, and the hash did not cover the lab test | each lab run first deletes the receipt; `lab-copy` covers the lab test | refusal "the lab test changed after the lab run"; on the test PC the filtered run deleted the earlier receipt |
| RV-04 | LOW | The hash and the build were two commands, so the program could be built from other files than those hashed | one command hashes, builds and re-hashes (`-zenity-lab-build`) | used for the last lab run |
| RV-05 | LOW | An `-overlay` or `-toolexec` in GOFLAGS builds other files than the copy's while `go list` names the copy | the resolution check refuses both; build-release.sh refuses any GOFLAGS | the twin with an `-overlay` |
| RV-06 | LOW | Renaming a test made the release's re-check run nothing and pass | the re-check's test names must exist | `TestTheGuardStepsCannotPassQuietly` |
| RV-07 | LOW | `continue-on-error` or `|| true` on a scan passed the wiring check | refused on the scan and re-check steps | `TestTheGuardStepsCannotPassQuietly` |
| RV-08 | LOW, latent | The message-box re-flow placed the box, then its buttons, the reverse of the other dialogs | buttons first, then the box | by mechanism (MA-1470's message-box cases pass) |
| RV-09 | INFO | A list falling back to upstream's layout kept its sideways extent | the fallback sets it to 0 | by mechanism |
| RV-10 | INFO | The receipt proves consistency, not that a lab run took place | §8 and UPSTREAM.md say so | none |

Prove-fail for these: six more mutants of MA-1468 (the vet exception reading only forward
slashes; the hash leaving out the lab test; GOFLAGS not checked; the re-check's names not
checked; a scan step allowed to fail; a scan followed by `||`), each red, with each
mutant's own `lab-copy` recomputed as its recorder would write it; with the original 13,
19 of 19. The last lab run (lab-copy `sha256:ab0644f4…`): a run filtered to one subtest
deleted the earlier receipt and wrote none; the whole run passed all five tests and wrote
the receipt the pins record.

Not covered by any review agent: the friction re-check's list change (G1, daf68cb), a
pure change to `fit.go` proven by MA-1469 (27 of 27 mutants) and MA-1470.
