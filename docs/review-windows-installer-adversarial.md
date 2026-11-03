# Adversarial pass: the unsigned Windows installer in every release

Date: 2026-11-02. Branch `windows-installer` (off v1.5.3 = main 3cff877). Reviewed: the
uncommitted tree that became commit fef5b3d. Settled in the commit after it.

**Verdict of the pass:** no high-severity hole; 0 high, 3 medium, 8 low. The workflow
fails closed everywhere the pass attacked it. **Not shippable yet:** the WiX pin is a
placeholder (M1), and this change still owes its **friction review** (the process gate
for a shippable feature).

Catalog ids for this work start at MA-1100, so they do not clash with the 1.5.4 delete
branch's MA-1036 onward.

## Findings and what was done

| # | Finding | Disposition |
|---|---|---|
| M1 | `wix314-binaries.sha256` is a placeholder, so no tag can release (no zip, no Mac app, nothing), while the suite stayed green | **Open, owner action.** The block is now visible: new test MA-1103 skips with "OPEN (MA-1103) … the release is BLOCKED" until a person fills the pin in, then checks it is 64 hex digits and that the file says **Checked by / Checked on / Checked against**. The pin file and README-RMM.md say plainly that the release is blocked. Not made a failing test: that would turn CI red on every branch until the owner acts (the repo's open-gap encoder is a skip). Not filled in here: downloading WiX is not approved. Whether to pin WiX 3.14.1 instead was not checked (offline). |
| M2 | Ten weakenings of release.yml and Get-WiX.ps1 all passed the tests | **Fixed.** MA-1100 now checks: publish's `if` exactly; top-level permissions exactly `contents: read`; the one write permission is publish's; build and msi set none; no download-artifact step names `run-id`, `github-token` or `repository:`; no `if: false`; the one-file check, the dist guard and the exact-names diff, in order; the exact-tag step's `if` and place. MA-1101 checks the two refusal lines exactly, each followed by its throw, and that the hashed file is the expanded file. Get-WiX.ps1 gained `-ZipPath` (a local zip, held to the same pin, never deleted), and a Windows-only test runs the real script: positive twin, then placeholder, sha256sum-style line and wrong hash each refused with nothing expanded. |
| M3 | Upgrading or uninstalling while MailArchive Desktop runs is the normal case and untested | **Words fixed, lab pending.** Release notes and README: click **Shut down** first and let a running backup finish; on a shared computer, others sign out. README-RMM.md explains why CloseApplication stays limited to the former name (it matches by program name only, so it cannot tell a backup from a dashboard) and adds §D item 4. New lab row **MA-1104 (pending)**: upgrade idle, upgrade during Run now, uninstall while running. `mailarchivew.exe` was NOT added to CloseApplication. |
| L1 | Version: tag v1.5.3 already exists (O7: no files added to a published release); the delete branch also calls itself 1.5.4 | **Owner call, unchanged.** The notes keep "New in 1.5.4" provisionally. Choose: installer is 1.5.4 and delete work 1.5.5, or both ship together in one release. |
| L2 | "each time you sign in" undercounts: it starts for every account | **Fixed:** "each time anyone signs in to this computer (every account on it, not only yours)", in the notes and README; tested. |
| L3 | Settings → Apps alone leaves the scheduled backups behind | **Fixed:** use **Danger zone → Uninstall** in MailArchive Desktop first, then Settings → Apps; notes and README; tested. |
| L4 | "Windows warns you twice" may undercount (Edge warns at download) | **Fixed in words, unverified:** "Your browser may warn … choose Keep (and Keep anyway if it asks again)". Edge's exact wording not checked on the VM. |
| L5 | Actions pinned by tag, not commit, in publish (which can write) | **Deferred.** The commit SHAs cannot be looked up offline, and dropping publish's checkout for `GH_REPO` changes release mechanics that can only be proven by a real release run. Same as the rest of the repo. |
| L6 | A pushed v0.0.0 passes the exact-tag check and skips the version-stamp check | **Fixed:** the exact-tag step also refuses v0.0.0; tested. |
| L7 | A local build uses any WiX on PATH or in `.tools\wix314` without the pin | **Documented** in README-RMM.md §B (delete `.tools\wix314`, build where WiX is not installed). CI is not affected. Hash-checking a cached copy was not added (a folder has no single hash to pin). |
| L8 | CloseApplication and Install-RMM.ps1 end any `mailarchive-desktop.exe`, even a backup under the former name | **Recorded, unchanged.** It predates this change; it goes away when the former-name copies are removed. |

## Prove-fail record

- On this Mac, a throwaway copy (`mutate2.py`): the pass's 10 mutations plus 13 more
  (v0.0.0 refusal dropped, the tag step's `if` weakened, `permissions: write-all` on
  msi, a download from another repository, hashing a different file, the pin filled
  without who/when/what, continue-on-error on the install, five wording reversals):
  all 22 red, and the positive twin (pin filled with who/when/what) green. Restored: green.
- On the Parallels Windows 11 VM (test program cross-built, run as the signed-in user
  from a copy in the lab share): the Windows test passed; with the hash check
  neutralised, the wrong-hash case went red; with the pin check neutralised, the
  placeholder and sha256sum-style cases went red; restored, green.

## Not tried

A real MSI build, install, upgrade or uninstall (no WiX: downloading it is not
approved); SmartScreen, UAC and Edge wording; Restart Manager behaviour (MA-1104); a
release.yml run on GitHub; poisoning setup-go's cache; whether re-running
`schedule … -install` collides with the zip's entry.
