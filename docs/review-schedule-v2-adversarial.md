# Adversarial review — schedule v2, secret file, lock, wrapper, and the offline archive

> As of 2026-09-02, over the tree at `2c99b4a` (schedule v2 slices a–e, the
> incremental-completeness build, the R19 offline hardening). Method: the
> assurance-kit adversarial pass run as a workflow — four independent finders,
> one per lens (outside aggressor via mail content; insider with write access to
> the archive directory; integrity/concurrency; unattended 02:00 operation), each
> finding then handed to a refutation-default skeptic with the code in hand.
> 22 findings survived (0 refuted). Every one is dispositioned below; the fixes
> landed in commit `11f4d35` ("Adversarial hardening") with PROVE-FAIL records,
> except where a row says otherwise.

## Findings and disposition

| # | Lens | Sev. | Finding (as confirmed) | Disposition | Proof |
|---|---|---|---|---|---|
| AGG-1 | outside | high | Regex-based meta neutralization diverged from the HTML parser: `&#114;efresh` and a `>` inside a quoted attribute let a mail-supplied `<meta http-equiv=refresh>` survive; the archive CSP cannot restrain a meta-refresh navigation, so an archived page opened from disk navigated to an attacker URL. | Fixed: `sanitizeHTML` parses the mail body with `golang.org/x/net/html` and neutralizes on the parsed tree (attribute renamed to `data-mailarchive-neutralized`), then re-serializes. | MA-80 (`TestRenderNeutralizesParserTricks`) |
| AGG-2 | outside | high | The archive CSP meta was injected after the mail's own first `<head>`/`<html>`, so resource-loading markup placed before `<head>` (or a comment-hidden head) was fetched before the policy applied. | Fixed: the page is always the tool's own document; the CSP meta is the first child of `<head>` and mail head content (minus `<title>`) follows it. | MA-80, MA-81 |
| AGG-3 | outside | low | `charset` appearing anywhere in the body suppressed the injected UTF-8 charset meta, leaving encoding to browser sniffing. | Fixed: `<meta charset="utf-8">` is always emitted first; a mail-supplied charset meta is removed. | MA-80 |
| AGG-4 | outside | low | `tsv()` stripped `\t`/`\n` but not `\r` or other controls: a subject with a bare CR forged an extra row in `attachments-report.tsv`. | Fixed: every C0/C1 control and DEL maps to a space. | MA-68 (`TestReportRowsSurviveControlCharacters`) |
| INS-1 | insider | medium | A newline in a scheduled argument (`-out`, `-input`, …) became a physical crontab line: arbitrary cron entry injection that `-remove` could not undo. | Fixed: `parseJob` and `Spec.Validate` refuse any control character in any argument, path, name, exe or log, naming the offending value. | MA-72, MA-97 |
| INS-2 | insider | medium | The lock file was opened without O_NOFOLLOW and then truncated: a planted symlink at `<out>/.mailarchive.lock` zeroed its target. | Fixed: `Acquire` Lstat-refuses a non-regular file and opens with O_NOFOLLOW on Unix. | MA-85 (`TestLockHardening`) |
| INS-3 | insider | low | `runlog.Open` followed a symlink at the log path or the rotation target and appended the whole run log to it. | Fixed: both paths are Lstat-checked and refused unless regular. | MA-93 (`TestRunLogRefusesSymlink`) |
| INS-4 | insider | low | The lock holder line was echoed raw into the refusal: terminal-escape injection from an attacker-written lock file. | Fixed: `readHolder` reads at most 256 bytes and strips control characters at the single choke point. | MA-85 |
| INS-5 | insider | low | `status` printed descriptor fields (exe, host, cadence) raw from an attacker-writable JSON. | Fixed: `ReadDescriptor` strips control characters, validates the name, degrades an unparseable cadence to a visible placeholder. | MA-97 (`TestDescriptorIsUntrusted`) |
| INS-6 | insider | low | `readSecret` was check-then-use (Lstat, then `ReadFile` on the path): a swap between the calls bypassed the regular-file/mode/size checks. | Fixed: open once with O_NOFOLLOW\|O_NONBLOCK (Unix), fstat the descriptor, read through a 4 KiB limit. | MA-74 |
| INS-7 | insider | low | The GUI "Remove the scheduled backup" deleted whatever path the descriptor's `wrapper` field named. | Fixed: the GUI rebuilds the Spec from `SanitizeName(d.Name)` + `DefaultWrapperPath` + `Validate()`, never trusting the descriptor's wrapper path. | GUI is lab-tier (MA-95); the shared construction is the CLI's, proven by MA-97 |
| INS-8 | insider | low | `DefaultWrapperPath` fell back to `.` (the cwd) when no config dir resolved: an executable batch file in a shared directory and a relative task path. | Fixed: the default is always absolute (LOCALAPPDATA → UserConfigDir → `~/.config`); `Validate` refuses a relative wrapper path. | MA-97 |
| IC-1 | integrity | high | The Message-ID-reuse split was gated on the stored record being complete, so a different message reusing an id while the first copy was still incomplete overwrote it. | Fixed: the fingerprint covers only the stable envelope (subject, sender, recipients, date, attachment names) and the split applies regardless of completeness; a body fill keeps its key. | MA-86 (`TestFillVersusReuseWhileIncomplete`) |
| IC-2 | integrity | high | `reindex` took no archive lock: its orphan sweep could delete an in-flight zip and it clobbered a concurrent run's manifest. | Fixed: `Reindex` acquires the archive lock first and refuses, naming the holder, while a run is active. | MA-85 (`TestReindexRefusesLockedArchive`) |
| IC-3 | integrity | medium | The manifest was saved before the index batch was committed; a crash in that window left messages recorded as exported but absent from the index, and incremental never re-added them. | Fixed: one `commit` helper flushes the index before saving the manifest at every checkpoint and at the end (run and graph). Ordering is by inspection; no fault-injection fixture exists to prove the crash window (acknowledged below). | — |
| IC-4 | integrity | medium | flock is bound to the inode: deleting the lock file mid-run let a second run acquire a fresh lock and interleave. | Fixed: `Lock.StillHeld` (fstat vs stat, `os.SameFile`) is checked at every checkpoint; a run whose lock was removed or replaced stops with that error instead of continuing. | MA-85 (`TestLockHardening`) |
| UJ-1 | unattended | high | The Graph client had no timeout or deadline: a stalled network at 02:00 wedged the job forever, holding the archive lock. | Fixed: per-request (60 s) and MIME (10 min) deadlines, `ResponseHeaderTimeout` and TLS handshake timeout on the transport. | MA-98 (`TestGraphRequestsAreBounded`) |
| UJ-2 | unattended | medium | `status` judged a `running` record by pid liveness alone: after a crash and reboot a reused pid read as "in progress" (fails open). | Fixed: liveness comes from the archive lock (`Gather` probes `Acquire`); a running record with a free lock is RED "never finished". | MA-75/76 (`TestGatherUsesTheLockForLiveness`) |
| UJ-3 | unattended | medium | A scheduled job whose `-out` sat under an unmounted drive created a shadow archive on the local disk and recorded `ok`. | Fixed: every scheduled job carries `-unattended`, which refuses to create a new archive when `-out` does not exist (CLI and GUI headless). | MA-99 |
| UJ-4 | unattended | low | Two spellings of one archive path (case-insensitive volumes) produced two schedules colliding nightly on the lock. | Fixed: `Install` refuses when the archive already records a schedule under a different name. | MA-97 |
| UJ-5 | unattended | low | `-copy-first` snapshotted into the system temp dir (often a small tmpfs), not the archive volume. | Fixed: the snapshot is created inside `-out`. | MA-100 (`TestSnapshotOnArchiveVolume`) |
| UJ-6 | unattended | low | Two archives installed under the same explicit `-name` shared one cron marker; the second install silently removed the first. | Fixed: on Linux/BSD `Install` refuses when a block under the same marker backs up a different `-out`. | MA-97 |

## Acknowledged limits

- **IC-3** is an ordering fix without a crash fixture: the repository has no
  fault-injection tier, so the window is closed by construction (index commit
  before manifest rename) and reviewed, not proven by a test.
- **INS-7** lives in the GUI, which has no display in CI; the defended
  construction it now shares with the CLI is what MA-97 proves.
- The offline-archive review that preceded this pass is filed separately as
  `docs/review-archive-offline.md`; its findings are not repeated here.
