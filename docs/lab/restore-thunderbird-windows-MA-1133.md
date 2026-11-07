# Restore to Thunderbird — lab row MA-1133 on Windows, 7 November 2026

Lab row MA-1133 (`docs/scenario-catalog.md`; design §3.2 step 1 note, §7.5 W-NOLOCAL, §13's
foldcheck note N4): **in a Thunderbird profile with no Local Folders, what exact menu path makes
Thunderbird create it, and does setting up an IMAP account make it?** Run by an agent on the
Windows VM, VM clock 05:49–05:55 (UTC−5), 7 November 2026, before the R7 friction review, so
that review sees W-NOLOCAL with a real path.

**Answer.** In Thunderbird 153.6.0 on Windows there is no menu item that adds Local Folders by
itself. Thunderbird makes Local Folders when an **email** or **newsgroup** account is added. The
path that needs no real mail account is:

> **menu button (≡, top right) → Add Account → Newsgroup** → type any name and email address →
> Next → type any server name → Next → Next → **Finish**.

Local Folders (with `Trash` and `Unsent Messages`, shown as Outbox) appears at once in the folder
list. Deleting that newsgroup account afterwards (Account Settings → the account → Delete →
Remove) **leaves Local Folders in place**, also after Thunderbird is restarted. Setting up an
**IMAP** account (≡ → Add Account → Email) makes Local Folders too. A **feed** account
(Account Settings → Add Account → Feed Account…) does **not**, not even after a restart.

## Set-up

- Parallels VM "Windows 11" (ARM64, build 10.0.26100), signed-in user, driven with
  `prlctl exec --current-user` running hidden PowerShell scripts from the Mac share
  (`P:\restorelab2\`); mouse and keys were real input events in the signed-in session.
- **Thunderbird 153.6.0** (the exe's product version), the ordinary install in
  `C:\Program Files\Mozilla Thunderbird\`.
- Only **throwaway profiles**, each started as a separate instance with
  `thunderbird.exe -no-remote -profile C:\Users\Public\tb-restore-lab2\NAME` (a path, never
  `-CreateProfile`, so the VM user's own `profiles.ini` and profiles were not touched). Each lab
  profile's `user.js` only turns off first-run nags (default-client check, Windows search,
  telemetry, updates); it adds no account or server keys.
- No real mail account was configured. The addresses and servers used were
  `lab@example.invalid`, `news.example.invalid` and `imap.example.invalid` (the `.invalid` top
  level never resolves).

## What was done, and what the profile held afterwards

The evidence is each profile's `prefs.js` (the `mail.account*` and `mail.server.*` lines) and its
`Mail` folder listing, read after each step, plus the screenshots listed at the end.

| Profile | Step | Local Folders server in prefs.js | `Mail\Local Folders` |
|---|---|---|---|
| `profile` | first start (account setup shown, minimised with its own button) | none; no `mail.server.*` at all | no `Mail` folder |
| `profile` | ≡ → Account Settings → Add Account → **Feed Account…** → Next → Finish | none (only `server1`, type `rss`, in `Mail\Feeds`) | absent |
| `profile` | Thunderbird closed and started again | none | absent |
| `profile` | Account Settings → Add Account → **Newsgroup Account…** (name `Lab`, address `lab@example.invalid`, server `news.example.invalid`) → Next → Next → Finish | `mail.accountmanager.localfoldersserver = server2`, type `none`, `directory-rel = [ProfD]Mail/Local Folders` | made, with `Trash`, `Unsent Messages` and their `.msf` |
| `profile` | the newsgroup account deleted (Delete → Remove, "Remove message data" left unticked), Thunderbird closed | still `server2`, type `none`; the news server gone from `mail.accountmanager.accounts` | still there |
| `fresh-news` | first start, account setup minimised, then **≡ → Add Account → Newsgroup** with the same values → Finish | `localfoldersserver = server1`, type `none` | made |
| `fresh-imap` | first start; in the account setup, address `lab@example.invalid` → Continue; after the lookup failed ("Thunderbird failed to find the settings for your email account"), IMAP, hostname `imap.example.invalid`, then **ADVANCED CONFIG** → OK ("an account with the current settings will be created, even if the configuration is incorrect") | `localfoldersserver = server2`, type `none`, beside `server1`, type `imap` | made |

Things seen on the way:

- Thunderbird 153's application menu (≡) has **Add Account** with Email, Calendar, Address Book,
  Chat, Feed and Newsgroup, and **Create** with only Message and Contact. Account Settings has no
  "Account Actions" menu any more: it has an **+ Add Account** button with Mail Account…, Chat
  Account…, Feed Account… and Newsgroup Account…. No item names Local Folders.
- The newsgroup wizard will not go past its first page until an email address is typed (Next stays
  greyed), so the words ask for "any name, email address and server name".
- The newsgroup wizard does not contact the server to finish. The IMAP account was made through
  ADVANCED CONFIG without reaching a server; the ordinary path with a real, reachable account was
  **not** run (the brief forbids a real account). That the ordinary path makes Local Folders too
  rests on the ADVANCED CONFIG result and on the newsgroup result, not on a run.
- The account setup's lookup for `example.invalid` took about a minute before it gave up.

## What this changes

- **W-NOLOCAL's words** (design §7.5; `internal/tbrestore/refusal.go`), lead's words,
  owner-delegated, replacing the `MENU PATH` placeholder:

  > This Thunderbird profile has no Local Folders yet, so there is nowhere to restore to and
  > nothing was written. Thunderbird makes Local Folders when you add an account. In Thunderbird,
  > click the menu button (≡, top right), then Add Account, then Email to add your mail account;
  > or, if you have none to add, Newsgroup: type any name, email address and server name (for
  > example news.example.invalid) and click Next until Finish. You may delete that newsgroup
  > account afterwards in Account Settings; Local Folders stays. Then close Thunderbird and click
  > Check again. MailArchive does not change Thunderbird's settings itself.

  The test that pins it is `TestRestoreLocalFoldersLookup`'s "no Local Folders" case
  (`internal/tbrestore/ma1085_prefs_test.go`, MA-1085, S73): it now asks for the menu button,
  Add Account, Email, Newsgroup, Finish and "Local Folders stays", and fails if the placeholder
  is still there. Prove-fail: with the placeholder words the case went red ("refusal must name
  'menu button (≡, top right)'"); with the new words, green.
- The design's candidate in §3.2 step 1 ("≡ menu → Account Settings → Account Actions → Add Mail
  Account…") does not exist in 153: there is no Account Actions menu. The working path is the one
  above.

## Not done here

- **macOS and Linux.** The words name the ≡ button, which Thunderbird 153 shows on every
  platform, but the path was walked on Windows only in this row (the R7 friction review's macOS
  pass records whether it walked it there).
- **MA-1091's foldcheck2 P1 step**, which the design says "runs with MA-1133": not run in this
  session (outside its brief); it stays pending in MA-1091.
- A real mail account through the ordinary (verified) path; a Thunderbird other than 153.6.0.

## Where things are

- Throwaway profiles left in place on the VM: `C:\Users\Public\tb-restore-lab2\profile`,
  `\fresh-news`, `\fresh-imap` (and `tb.pid`). Lab scripts (not committed):
  `/Users/alex.dimarco/repositories/.winlab/restorelab2/`.
- Screenshots (not committed; some show the VM user's desktop and name, keep private),
  `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/restore-lab-r8/`: `j1-02` account
  setup on first start; `j1-04` the ≡ menu; `j1-05` its Add Account submenu; `j1-06` Create;
  `j1-07`–`j1-08` Account Settings and its Add Account button; `j1-09`–`j1-11` the feed account;
  `j1-12` after restart, still no Local Folders; `j1-13`–`j1-18` the newsgroup account and Local
  Folders in Account Settings; `j1-19` the delete dialog; `j1-20`–`j1-24` the fresh profile via
  ≡ → Add Account → Newsgroup and its folder list with Local Folders, Trash and Outbox;
  `j1-25`–`j1-30` the IMAP account through ADVANCED CONFIG.

---

- Date worked on: 7 November 2026 (VM clock, UTC−5; the session's date agrees).
- Drawn on: the design's §3.2, §7.5 and §12.3 notes and catalog row MA-1133; what the VM showed;
  each lab profile's `prefs.js` and folder listing. Not drawn on: Thunderbird's source (why a feed
  account does not make Local Folders is observed, not explained).
- Register: a report of a lab run (findings), with the words marked as the lead's.
- Smallest check for a reader: on the VM, open `C:\Users\Public\tb-restore-lab2\fresh-news\prefs.js`
  and find `mail.accountmanager.localfoldersserver` beside a `type "nntp"` server.
- Counters. Register: report. Borrowed lines: Thunderbird's menu labels and dialog texts, quoted.
  Drift: none noticed. Touched: this file, `refusal.go`, its test, the design and catalog notes;
  on the VM only `C:\Users\Public\tb-restore-lab2\`. Persona: none. Source reached: design and
  catalog — yes; the VM — yes; Thunderbird source — no.
