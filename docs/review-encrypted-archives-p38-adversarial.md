# Review — the adversarial pass over P3-8's fixes

**Reviewed:** a531899..452be31 (P3-8's Tier A–C fixes, MA-849..853), on a frozen copy,
2026-10-27.
**How:** five lenses — WINPATH (Windows identity and paths), WINACL (Windows access lists, drives,
signals), ORDER (refusal ordering and the key verbs), LEAK (what text reaches where), DESK (the
Desktop) — and a Windows lab rerun, then one skeptic told to refute each finding, then a
completeness critic (workflow run wf_b488f2af-bb9; 8 agents).
**Tallies:** 15 found by the lenses, 14 confirmed by the skeptic (several regraded), 1 refuted;
the critic added 1 (COMP-1). The Windows lab rerun found no product defect: its failures are
environmental (no source tree or `go` in the guest, SYSTEM's files owned by Administrators, Unix
mode-bit tests) or a test's own hygiene.
**Fixed in:** MA-854.

| Finding | Graded | What | Disposition |
|---|---|---|---|
| WINPATH-1 | LOW | a pin that did not take was ignored, leaving the identity to be read at the comparison | **Fixed.** `PinIdentity` reports whether it took; every caller refuses when it did not |
| WINPATH-2 | LOW | the junction fix recorded a folder-mounted volume by its drive letter, which can change | **Fixed.** A walk follows junctions and links only; a volume mounted in a folder is kept as spelled (proven red on the guest with a real mounted virtual disk) |
| WINPATH-3 | LOW | the junction fix opened paths without the long-path form | **Fixed by the same walk** (through the os package). Not provable red on this guest: Go's process opens long paths even with the setting off |
| WINACL-1 | LOW | the built-in Administrator's own access entry (alias LA) was taken for another account, and the remedy removed it | **Fixed** in winsec and keyring; proven red and green on the guest |
| WINACL-2 | LOW | an internal eMMC disk (SD or MMC bus) counted as leaving the computer | **Fixed.** Only USB and FireWire on a fixed drive |
| WINACL-3 | LOW | the icacls remedies break in PowerShell for a name or path with `$` or a backtick | **Fixed.** They are for the Command Prompt, and say so. A `%NAME%` that names a variable is still expanded there — rare, recorded |
| ORDER-1 | MED | restore-key-file -remembered's early check (B6) was narrower than the restore's own lookup | **Fixed.** One lookup for both |
| ORDER-2, DESK-1 | LOW | forget showed one spelling and checked another | **Fixed.** The text exactly as shown is accepted, for that archive only |
| ORDER-3 | LOW | a run that takes the lock after the early busy check is refused with the lock's raw details | **Accepted.** A narrow race; changing the lock's own message touches two existing gates (MA-85, MA-153) |
| ORDER-4 | — | repair-record on a record with one damaged copy | **Refuted:** one good copy is a usable record by design |
| LEAK-B19 | LOW | a damaged message's label could carry bidirectional overrides from mail text | **Fixed.** `StripBidi` |
| F1 | MED | forget-key said "nothing to forget" for a remembered key the store will not read | **Fixed**, and its twin in the forget plan |
| F2, COMP-1 | LOW | printed commands quoted with `%q` fail for a network folder, and a drive root's trailing backslash breaks the command line | **Fixed** for P3-8's commands (`encmark.CommandPath`, trailing backslash doubled). Older commands outside P3-8 still use `%q` — recorded for later |
| DESK-2 | LOW | the advice for an archive made at the command line gave only the local-mail command | **Fixed.** Both forms |
| DESK-3 | LOW | that advice appeared beside an Archive now line saying Run now, for an archive that cannot be opened | **Fixed** |

**Also found and fixed while checking these:** commit 452be31's wording of the program-copies
question broke the terminal test `TestTheCLIMakesAndBacksUpAnEncryptedArchiveAtARealTerminal`,
which waited for the release wording; it now waits for the question's start.
