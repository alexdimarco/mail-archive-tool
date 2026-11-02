# Friction review: the Windows installer (1.5.4)

Walked 2 November 2026, hands-on on the Parallels VM "Windows 11" (ARM64; the x64 installer
runs emulated), by the walk agent of workflow wf_30c4c600-d1d. Recorded here by the lead.

**Verdict: SHIPPABLE_WITH_NOTES.** Install, every shortcut, an upgrade and two uninstalls
worked, and the test archive (12 files) was the same before the install and after both
uninstalls.

## What was walked

- Two installers built on the VM with WiX 3.14: 1.5.3 from the released code (3cff877),
  and this branch (317bf3b) numbered 1.5.90 so it counts as an upgrade.
- Both installed silently as the system account, as IT tools install.
- Every entry the installer adds: the Start menu entry and the desktop shortcut open
  MailArchive Desktop in Edge; the sign-in entry starts it with no browser tab; the wizard
  and Copy link entries each open a window.
- An upgrade 1.5.3 → 1.5.90 with MailArchive Desktop running.
- Two uninstalls: one with Desktop running, one after Shut down.

## The prompts, word for word

1. Double-click on a downloaded file: "Open File - Security Warning": "The publisher could
   not be verified. Are you sure you want to run this software?" Publisher: Unknown
   Publisher. Run / Cancel.
2. The administrator-permission window: not captured (Windows does not let a program read
   it, and the walker did not answer it).
3. Start menu "Uninstall MailArchive Desktop": "Are you sure you want to uninstall this
   product?" Yes / No.
4. "Copy MailArchive link": "Your browser did not open. Copy this link into your browser:"
   Copy / Close.
5. "MailArchive wizard": "What would you like to do?" OK / Cancel.

## Findings, and what was done

- **Medium: an upgrade leaves the old version running, with no sign.** No prompt and no
  restart request; the Start menu entry reopened the old dashboard until sign-out.
  *Done:* the release notes say to Shut down first, and if you forgot, to sign out and back
  in. *Open:* the program offering "MailArchive was updated. Restart it now?" (a product
  change for a later release); lab row MA-1104 (upgrade during a Run now backup) is still
  pending.
- **Low–medium: after an uninstall with Desktop running, it keeps answering until
  sign-out**, and an empty Program Files folder stays until the next install. *Done:* the
  docs already say to use Uninstall MailArchive… (which shuts it down) first. *Open:* the
  same restart offer.
- **Low: "Danger zone → Uninstall" is not on the page.** *Done:* the docs name the control
  as drawn, **Uninstall MailArchive…**, and MA-1102 checks it.
- **Low: the notes did not name the "publisher could not be verified" prompt.** *Done:*
  "If Windows says "The publisher could not be verified", click Run." (MA-1102).
- **Low: Settings → Apps showed the contact "IT Support".** *Done:* removed from Product.wxs.
- **Low: the installed "READ ME.txt" is the build note for IT** (it names the person who
  first built the MSI). *Open:* install a home-user note instead; for a later release.
- **Low: the Copy link entry says "Your browser did not open"** when started on purpose.
  *Open:* older than the installer; for a later release.

## Not walked

Answering the permission window; a real sign-out and sign-in; a real Edge download and the
"Windows protected your PC" screen; upgrading during a Run now backup; clicking through
Settings → Apps (the same uninstall command was run); more than one user signed in; a real
x64 PC.
