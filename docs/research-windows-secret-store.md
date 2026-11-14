# Research: where MailArchive keeps its secrets, and a sandboxed reader on Windows (findings P2 and P3)

| | |
|---|---|
| Status | Research and options for the owner and for the 10-lens pre-code design review. Not a design. No code. |
| Date worked | 2026-11-14 (from the session clock) |
| Code read | `main` at `1a6d437` (shipped 1.7.1), in a read-only worktree; the 1.7.2 patch commits `2d55148` (the labelled instance file, MA-1427) and `5523487` (the 1.7.2 adversarial pass, `docs/review-patch-1.7.2-adversarial.md`, findings P2 and P3) through `git show` |
| Register | A research summary with options and a recommendation. Facts come from the code and from public documents; each carries a source (the numbered list at the end) and how sure it is. My own inferences say so. |
| Inputs | A secrets inventory read from the code, plus three research angles (the Windows store, macOS and Linux, comparable apps). A separate skeptical verifier checked every research claim. Claims it refuted are dropped and named in §4.4. Claims it marked uncertain are in §4.2 as lab questions. |
| What was not done | Nothing was run: no program, no tests, no lab, no virtual machine. No secret store was read. The web pages were fetched by the research and verifier steps, not fetched again for this note. |
| Gates owed | The 10-lens pre-code design review (this is security- and architecture-relevant), the adversarial pass on whatever is built, and a friction review for the browser hand-off change (§5.2). This note stops before all three. |

Words used throughout, defined once:

- **Integrity level.** Windows tags every program with a level: Untrusted, Low, Medium (a normal
  program you start), High (an administrator's elevated program), System. A program "at Low" still
  runs as you, with your name and your folders, but Windows limits what it can change [S1].
- **No-read-up label.** A tag on a file that says "a program at a lower level than this file may not
  read it". Windows does not add it by default; by default it only stops lower programs from
  *writing* [S1][S3].
- **Credential Manager.** Windows' built-in password store. Programs save and read entries with the
  calls `CredWrite` and `CredRead` [S4][S6].
- **DPAPI.** Windows' per-user encryption call (`CryptProtectData`): data it locks can be unlocked
  only by the same Windows account [S8][S9].
- **AppContainer.** The strong sandbox that Microsoft Store apps run in. It is Low integrity *plus*
  a second identity check of its own [S15][S16].
- **Sandboxed / boxed-in program.** In this note, a program that runs as you but with fewer rights:
  Low integrity or an AppContainer on Windows, the App Sandbox on a Mac, Flatpak or Snap on Linux.

---

## 1. Summary for the owner

MailArchive keeps four long-lived secrets on your computer so backups can run on their own: the
Microsoft 365 sign-in (whoever holds it can read the whole mailbox from any computer; it lasts 90
days and renews itself each time it is used), the Microsoft 365 app secret if you use app sign-in
(it can read every mailbox in the organisation), the alert email password (lets someone send mail as
that account), and the remembered keys of encrypted archives (they open every copy of that archive,
on a NAS or in the cloud too). On Windows all four sit in Windows Credential Manager, and the 1.7.2
review showed a boxed-in ("sandboxed") program reading such an entry back, but only in a test run
under Windows' built-in system account, not yet as a person signed in normally. Microsoft documents
no rule that would stop it, so I expect the signed-in test to confirm it (medium confidence). The
attacker here is a program already running on your PC inside a light sandbox; a web page in Edge or
Chrome is boxed in far more tightly and most likely cannot do this, and nobody on another computer
can. So it is unlikely but serious; and a normal, unsandboxed program you run can read these secrets
in MailArchive and in every comparable app, which this plan does not change. I recommend locking each
Windows secret with a key kept in a file that sandboxed programs cannot open (the same lock 1.7.2 put
on the Desktop's own key file), and handing the browser its one-time link through such a file instead
of on a command line that other programs can read. On Mac and Linux I recommend no storage change
now, only plain words saying who can and cannot read the saved secrets.

---

## 2. Threat model

Six kinds of attacker. "Today" means shipped 1.7.1 plus the 1.7.2 patch (the labelled instance
file). "Defend?" is my recommendation, matched to the scope already ruled: **DC4** ("the
loopback+CSRF boundary is documented as not defending against a same-user local process",
`docs/design-mailarchive-desktop.md:9-10`), the confidentiality design's non-goal 2 ("a program
running as you can read your archive directly", `docs/design-local-confidentiality.md:248-249`), and
**R-E2** (encryption does not protect "someone with access to your logged-in computer",
`docs/design-encrypted-archives.md:113-121`).

| | Attacker | Windows today | macOS today | Linux today | Defend? |
|---|---|---|---|---|---|
| A1 | **A web page inside Edge or Chrome** (the browser's "renderer" process). Chromium runs it at Untrusted integrity with every identity but its logon identity set to "deny only" [S19]. | Most likely cannot use Credential Manager: Microsoft says the caller's "token must not have the user's SID disabled" [S4]; reading "deny only" as "disabled" is my inference (medium). Cannot open your files on its own merits: its restricted identity list holds only the empty identity (S-1-0-0) [S19] (medium-high). | Browser sandboxes on macOS were not researched (UNSOURCED). | Not researched (UNSOURCED). | Windows and the browser already defend. Nothing for MailArchive to add. |
| A2 | **A Low-integrity program running as you.** For example one started with `psexec -l`, which Microsoft says runs at Low [S42], or an .exe whose file is labelled Low (a new program runs at the lower of your level and the file's) [S1]. Internet Explorer's Protected Mode ran web content this way [S2]. | Reads every file of yours that has no no-read-up label [S1][S2] (high). Reads other programs' command lines [S13][S2] (medium-high; lab-shown as P2 in the SYSTEM form). Connects to 127.0.0.1 [S17] (lab-shown in `2d55148`). Reads Credential Manager: lab-shown **only in the SYSTEM form** (P3); for a person's own account UNPROVEN (§4.2 U1). Cannot read 1.7.2's labelled instance file (lab-shown, SYSTEM form). | No integrity levels on macOS. The nearest kind is A3. | No integrity levels on Linux. The nearest kind is A3. | **Yes, on Windows.** 1.7.2's fix C1 already treats this attacker as in scope. This note's main subject. |
| A3 | **A strong sandbox**: a Windows AppContainer, a Mac App Sandbox app, a default Flatpak, a strictly confined Snap. | AppContainers "are isolated from accessing processes and windows belonging to other applications as well as from devices, files/directories, registry keys, the network, and credentials" [S16]; 127.0.0.1 is blocked for them by default [S17][S18]. Medium: the credential sentence is general and untested for `CredRead` (§4.2 U4). | By default a sandboxed app "doesn't have unrestricted access to the user's home folder" [S53], so it cannot read `~/Library/Application Support/mailarchive` (medium; a temporary-exception entitlement can widen this, §4.2 U13). | A default Flatpak sees "No access to any host files except the runtime, the app, ~/.var/app/$FLATPAK_ID" and "No access to processes outside the sandbox" [S65]. A strict Snap cannot see hidden home folders such as `~/.config` [S66] but **can** read the command lines of your other programs (snapd's rule `owner @{PROC}/@{pid}/cmdline r`) [S66], so it can read the one-time link on `xdg-open`'s command line. | Secrets: the systems already defend by default. The Linux link (P2's Linux form) is open to Snaps. |
| A4 | **A normal program running as you** (Medium on Windows, any unsandboxed program elsewhere). | Reads everything: Credential Manager, your files, and anything DPAPI locked for you. Google: DPAPI "does not protect against malicious applications able to execute code as the logged in user" [S12]; Electron says the same of its Windows store [S27]. | Reads the 0600 files with no prompt. This is inferred from Apple's lists of folders that prompt, which do not include an unsandboxed app's own Application Support folder [S54][S55] (medium; lab MC2). | Reads the 0600 files. With `ptrace_scope=0` it can also read a same-user process's memory, unless that process marked itself non-dumpable [S69]. | **No, on every system.** Out of scope by DC4 and non-goal 2. The comparable apps draw the same line [S12][S27][S28][S64]. Only a broker service running as SYSTEM (Chrome's App-Bound Encryption [S12], since bypassed in practice [S37]) or a password typed each session goes further (§5.1 W6, W7). |
| A5 | **Another account on this computer.** | Cannot read your Credential Manager entries (they belong to your logon session [S4]) or your owner-only files. The code's comment says a standard account cannot read another account's command lines on Windows (`internal/browser/browser.go:45-47`); UNSOURCED here. | Cannot read 0600 files. The link never appears on a command line (osascript reads it on standard input). | Cannot read 0600 files. **Can** read the one-time link on `xdg-open`'s command line: by default "Everybody may access all /proc/pid directories" (hidepid=0) [S67]. | Yes. Linux has a known gap: the link (§5.2 L4). |
| A6 | **A copy of the disk or of a backup.** | Credential Manager's own files are DPAPI-protected with a flag that lets only the system's security process unlock them [S9][S10][S7]: they need your Windows password. Settings files and unencrypted archives are plain. | The four secrets are plain text in 0600 files: readable from a copied disk or an unencrypted backup, unless FileVault is on. | The same, unless the disk is encrypted. | Windows: yes for the secrets. Mac and Linux: no; the design's stated answer is full-disk encryption (`docs/design-local-confidentiality.md` §3, non-goal 1). |

**How likely, and why it still matters.** A2 needs a program already running on your PC inside a
*light* sandbox. Most malware runs as a normal program (A4) and reads these secrets anyway, in every
comparable app. So the chance is low and the damage is high. The reason to fix it anyway: 1.7.2 (C1)
already decided that a Low-integrity program is an attacker, and against that attacker Credential
Manager gives away far more than the Desktop session C1 protected: live mailbox access for 90 days at
a time, the organisation's app secret, the alert account, and the keys to encrypted copies kept
elsewhere. Microsoft's list of security boundaries names the AppContainer sandbox but not integrity
levels [S25], so Microsoft will most likely treat a Low program reading Credential Manager as working
as designed (my inference, medium). If it is to be fixed, MailArchive has to fix it.

**What this does not make safe.** A Low program can already read every unencrypted archive (they are
ordinary files of yours [S1]) and, on Windows, the decrypted search index of an encrypted archive
while it is open (`%LOCALAPPDATA%\mailarchive\scratch\<hash>\index\reader-<pid>-<n>.db`,
`internal/app/scratch.go:17-40`, `internal/keyring/reader.go:352-400`). §5.4 covers MailArchive's own
folders. Archive folders the person chooses (a USB drive, a NAS) are out of reach of this plan.

Which secret matters most against A2, worst first: the app client secret (every mailbox in the
organisation [S38]), the Microsoft 365 sign-in (one whole mailbox, from anywhere [S38][S39]), the
remembered archive keys (every copy of that archive), the alert password (send mail as that account).

---

## 3. Inventory

Read from the code at `1a6d437` unless marked. "Who can read it" lists attackers from §2 other than
you; "(lab)" means the lab must confirm.

| # | Secret | What a holder can do | Windows | macOS | Linux | Who can read it today | Code |
|---|---|---|---|---|---|---|---|
| 1 | **Microsoft 365 sign-in** (device sign-in; the refresh token, and off Windows also the access token) | Read the person's whole mailbox through Microsoft Graph from any computer. Permission `Mail.Read` (delegated): "Allows the app to read the signed-in user's mailbox", plus `offline_access` [S38]. A refresh token lasts 90 days by default and replaces itself on each use; it ends on revoke, or for a password sign-in when the password changes [S39]. | Credential Manager generic entry `mailarchive:graph-token:<hash of tenant>-<hash of client id>`, persistence "local machine" (stays on this computer, does not roam [S6]). Contents: JSON `{upn, token}`; the access token is dropped to stay under the 2,560-byte limit. | Plain JSON file, mode 0600: `~/Library/Application Support/mailarchive/graph-token-<hash>-<hash>.json`; the CLI with `-mailbox` adds a third hash; `-token-cache PATH` puts it anywhere. | The same file under `$XDG_CONFIG_HOME/mailarchive/`, or `~/.config/mailarchive/` when that is unset [S40]. | Windows: A4; A2 (lab). Mac, Linux: A4; A6. | `internal/graphconfig/secretstore_windows.go:21-26`; `internal/graphconfig/tokenstore.go:16-36`; `internal/graph/delegated.go:373-383`; `internal/graphconfig/config.go:64-76`; `cmd/mailarchive/main.go:1990-2000` |
| 2 | **Microsoft 365 app client secret** (app-only sign-in) | With the tenant id and client id (not secret; they are in `graph-config.json`), get app-only tokens with the application permission `Mail.Read`: "Allows the app to read mail in all mailboxes without a signed-in user" [S38]. Works until the secret expires (the program's help says Entra caps it at 24 months) or is deleted. | Credential Manager `mailarchive:graph:<hash>-<hash>`, local machine. Also at run time: the variable `MAILARCHIVE_GRAPH_SECRET` (or the name in `-client-secret-env`), or a file named by `-client-secret-file`, which on Windows is read with **no** owner or access-list check. | Plain 0600 file `~/Library/Application Support/mailarchive/secrets/graph_<hash>-<hash>.secret`; or the variable; or `-client-secret-file`, refused unless mode 0600. | `~/.config/mailarchive/secrets/graph_<hash>-<hash>.secret`; the same variable and file paths. | As row 1. On Windows, a `-client-secret-file` is as readable as its folder makes it. | `internal/server/setup.go:93-145`; `cmd/mailarchive/main.go:1739-1769`; `cmd/mailarchive/job.go:543-553`; `internal/util/securefile.go:155-157`; `internal/graphconfig/secretstore_file.go:15-75` |
| 3 | **Alert email (SMTP) password** | Send mail as the alert account, often the person's own Gmail (app password) or Office 365 mailbox. Whether a Gmail app password also allows reading mail by IMAP: UNSOURCED. | Credential Manager `mailarchive:mailarchive-alert-smtp`, local machine. Never in `desktop-settings.json`. **Uninstall leaves it**: no clearing code on either uninstall path. | Plain 0600 file `.../mailarchive/secrets/mailarchive-alert-smtp.secret`. Uninstall leaves the whole `secrets/` folder (it deletes only listed top-level files). | The same under `~/.config/mailarchive/secrets/`. Same uninstall gap. | As row 1, **plus anyone holding a Desktop session** (see "Other exposures", first bullet). | `internal/backupalert/backupalert.go:79-83`; `internal/desktop/alerts.go:45-128`; `internal/alert/alert.go:367-420`; `internal/uninstall/credentials.go:20-30`; `internal/uninstall/config.go:28-46` |
| 4 | **Remembered encryption key** (an encrypted archive's 32-byte master key) | Open that encrypted archive, all its mail, with no password, on any copy of it. It survives a password change: a password change rewrites only the key file and "the remembered key and every sealed byte stay as they are" (`internal/vault/store.go:620-629`). | Credential Manager `mailarchive:archive:<64-hex archive id>`, local machine; the base64 master key. Written when the person ticks "Remember the key"; read by unattended backups and the reader. **Uninstall keeps it on purpose** (rule A20, `docs/design-encrypted-archives-phase3.md:102`); only Forget or Forget-all removes it. | Plain 0600 file `.../mailarchive/secrets/archive_<hex id>.secret`. | The same under `~/.config/mailarchive/secrets/`. | As row 1. | `internal/vault/keystore.go:9-47`; `cmd/mailarchive/encrypted.go:426-433`; `internal/keyring/keyring.go:412-476`; `internal/keyring/forget.go:209-282` |
| 5 | **Archive password, recovery key, and their optional files** | Open the archive and change its keys. The archive's key file `.mailarchive-vault.json` (and its backup copy `.mailarchive-vault.bak.json`) travels with every copy, so anyone holding a copy can guess passwords offline; the 128-bit recovery key cannot be guessed. | Never stored by MailArchive. Typed at a terminal or into the Desktop page (sent over loopback; pages served with no-store). `-password-file` / `-recovery-key-file` are refused unless you own them and no other account can read them. Files MailArchive writes (`-recovery-key-out`, `-recovery-file-out`) get an owner-only access list but **no integrity label**, so A2 can read them [S1]. | Same flows; secret files must be yours and 0600 (macOS access lists not inspected, a stated limit). | Same, plus the POSIX access-list check. | Windows: written recovery files readable by A2. | `internal/keyring/secretfile_windows.go:15-32`; `internal/keyring/newfile.go:15-83`; `internal/winsec/owneronly_windows.go:9-43`; `internal/vault/store.go:24-25` |
| 6 | **Desktop instance key K** (and each `serve` instance key) | Ask the running Desktop for a fresh 10-minute link at any time (POST `/_handoff`, `quiet=true`, a correct HMAC), which means the whole dashboard and reader. | `%LOCALAPPDATA%\MailArchiveDesktop\desktop-instance-s<session>.json`. **Fixed in 1.7.2** (`2d55148`): written with a Medium no-read-up label; Low refused in the lab (SYSTEM form). Residual P4: a 1.7.1 Desktop left running after the update keeps an unlabelled file until it quits. | `~/Library/Application Support/mailarchive/desktop-instance.json`, 0600 in a 0700 folder. | `~/.config/mailarchive/desktop-instance.json`, the same. | Windows after 1.7.2: A4 only (A2 lab L1). Mac, Linux: A4. | `internal/instance/handoff.go:98-140`; `internal/instance/instance.go:42-61`; at `2d55148`: `internal/util/secretfile_windows.go` |
| 7 | **One-time launch link** (Desktop, `serve`, `setup`) | Whoever opens it first gets that surface's session. Opener link: 60 seconds; a printed or copied link: 10 minutes; each works once. | On the command line: `rundll32 url.dll,FileProtocolHandler <link>`, then the browser's. **P2.** The Copy button pipes a 10-minute link into `clip.exe`. | Sent to `osascript` on standard input; on no command line. Copy uses `pbcopy`. | On `xdg-open`'s command line and the browser's. The Copy fallback dialog (zenity) also passes it as `--entry-text <link>`, another command line (verifier's addition). `serve -link` prints to standard output even when redirected. | Windows: A2 (P2, lab-shown by mechanism), A4. Linux: A5 (hidepid=0), A3 (Snap), A4. macOS: A4 (clipboard). | `internal/browser/browser.go:23-54`; `internal/localweb/session.go:40-67`; `internal/desktop/dialogs.go:34-69`; `cmd/mailarchive/link.go:52-144` |
| 8 | **Session cookie, secret path prefix, form token** | The cookie plus the path is a full session for the life of the process. | In MailArchive's memory only (32 and 16 random bytes). A Low program cannot read another program's memory: the right to read memory is withheld upward "to block the ability of lower-privilege processes from gaining read access to memory that may contain password data" [S2] (high). In the browser: an HttpOnly, SameSite=Strict cookie. | Same. | Same. A `serve` bound beyond loopback sends it over the network in clear text, and warns. | A4 (and whatever reads the browser's cookie store; UNSOURCED). | `internal/localweb/session.go:19-38`; `cmd/mailarchive/serve.go:156-174` |

**Other exposures** (all read in code; none tested):

- **A Desktop session is worth the alert password.** POST the alert settings with your own server and
  a blank password box: the stored password is kept (`internal/desktop/alerts.go:81-90`). Then POST
  "Send test email" (`alerts.go:113-128`): MailArchive authenticates with PLAIN to the saved server
  (`internal/alert/alert.go:371-373`). Go's `PlainAuth` "will only send the credentials if the
  connection is using TLS or is connected to localhost" [S41], so a server of the attacker's with a
  valid certificate gets the password, and a listener on 127.0.0.1 gets it without TLS (my inference
  from [S41]). This makes P2's prize bigger than dashboard access.
- **No MailArchive folder on Windows carries an integrity label** except 1.7.2's instance file. A search
  for `ML;`, `NO_READ_UP` and `LABEL_SECURITY_INFORMATION` at `1a6d437` finds nothing;
  `winsec.RestrictToOwner` sets only the owner and access list. So A2 can read
  `%APPDATA%\mailarchive` (settings, the alert server and username, archive list, job files, logs),
  `%LOCALAPPDATA%\MailArchiveDesktop` (lock files, scheduled-task wrapper `.cmd` files) and
  `%LOCALAPPDATA%\mailarchive\scratch` (decrypted indexes, `-copy-first` snapshots, Outlook PST
  exports during an encrypted backup) [S1][S2].
- **Roaming.** `%APPDATA%` follows a roaming profile, so `graph-config.json` and
  `desktop-settings.json` can roam; the Credential Manager entries (local machine) do not [S6].
- **Environment variable.** `MAILARCHIVE_GRAPH_SECRET` lives in the process environment; A2 cannot
  read it (no memory-read right upward [S2]); A4 can.
- **Logs.** In the code read, no token, secret or password is written to a log; Graph redirects to
  another host are refused before the bearer token is sent (`internal/graph/guard.go:156-179`). Not
  every log line was audited.

**Correction beside the inventory.** The inventory's note that the instance key K is "likely worse
than P2" was written against 1.7.1's code. 1.7.2 (`2d55148`) labels the instance file, and its lab
showed Low refused ("Access is denied") in the SYSTEM form. What remains is P4 (a 1.7.1 Desktop still
running after the update) and the person's-session check (lab L1). Corrected by this note's author.

---

## 4. What is established, and what is not

### 4.1 Established (with sources)

- **E1. Windows integrity levels stop reading only where a file says so.** "By default, the system
  creates every object with an access mask of SYSTEM_MANDATORY_LABEL_NO_WRITE_UP"; "Objects that lack
  an integrity label are treated as medium" [S1]. "The browser that is running at low integrity does
  have read access to user data files. Because the Windows integrity mechanism does not enforce
  confidentiality, it does not restrict the flow of information" [S2]. High. The repo's own lab agrees:
  a Low process read 1.7.1's unlabelled instance file (`2d55148`). That folder permissions cannot
  help is an inference: a Low program and a Medium one both run as you.
- **E2. A Medium no-read-up label blocks Low readers, in pure Go.** "A principal with a lower mandatory
  level than the object cannot read the object" [S3]. 1.7.2's `util.WriteSecretFileAtomic0600` creates
  the file through `CreateFile` with the security descriptor `S:(ML;;NRNWNX;;;ME)`; the VM read the
  label back as `S:AI(ML;;NWNRNX;;;ME)` and a Low child got "Access is denied" (`2d55148`). High, but
  shown only as SYSTEM lowered to Medium and Low, with no person signed in.
- **E3. A Low program can read your other programs' command lines.** A lower program keeps three
  rights to a higher one: "SYNCHRONIZE, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_TERMINATE" [S2]
  (high). On Windows 8.1 and later the command-line query (`ProcessCommandLineInformation`) needs only
  `PROCESS_QUERY_LIMITED_INFORMATION` [S13] (medium-high: a community reference, not Microsoft). The
  1.7.2 review read a Medium probe's whole command line from Low by process number (P2, SYSTEM form).
  So the link must leave every command line.
- **E4. Finding the right program is cheap.** On current Windows a caller below Medium sees only the
  processes it can access in the process list [S14], and the 1.7.2 lab's Low process saw 4 of them; but
  trying process numbers one by one works (P2 lab). A short link lifetime alone does not close P2.
- **E5. A Low program reaches 127.0.0.1; an AppContainer does not by default.** "One of the specific
  restrictions imposed on AppContainer applications is blocking access to localhost", while a separate
  rule "grants normal applications loopback access" [S17]. Lab: a Low process redeemed a link over
  127.0.0.1 (HTTP 200, two cookies; `2d55148`). Verifier's correction: no capability grants an
  AppContainer loopback; only the developer-only `CheckNetIsolation LoopbackExempt` does [S18].
- **E6. Credential Manager's documented rules contain no integrity check.** `CredRead` and
  `CredEnumerate`: "The credential set used is the one associated with the logon session of the current
  token. The token must not have the user's SID disabled" [S4][S5]. A filter such as `mailarchive*`
  lists every entry starting so [S5]; renaming would not hide them. Microsoft (2007): "Most interprocess
  communication (IPC) mechanisms between processes and services, such as RPC, are not restricted by
  integrity level" [S2]. In the system's credential service, the only caller-trust check Forshaw found
  guards two other calls, not `CredRead` [S7]. What this does *not* establish: §4.2 U1.
- **E7. Session-only saving is no fix.** Session-only entries "will not exist after this user logs off
  and back on" [S6], so scheduled backups would lose them. The 1.7.2 lab's Low child read a
  session-only entry (SYSTEM form).
- **E8. Copying Credential Manager's own files does not help a thief.** They carry the
  `CRYPTPROTECT_SYSTEM` flag [S10], used "to protect data that cannot be unprotected outside the LSA"
  [S9]; Forshaw: "Only code running in LSASS can call the DPAPI to decrypt the blob" [S7]. (This
  refutes research claim win-store-5, §4.4.)
- **E9. DPAPI alone does not separate programs of the same person.** "Typically, only a user with the
  same logon credential as the user who encrypted the data can decrypt the data" [S8]; any program
  running as you "can access any protected data that they know about" [S9]. The optional extra secret
  ("entropy") must be given again to decrypt [S8], and "If it is simply saved to a file unprotected,
  then adversaries could access the entropy" [S9]. The prompting option "is deprecated and will be
  removed in April 2027" [S8]. So DPAPI helps against A2 only when its extra secret, or the key it
  protects, sits in a no-read-up file. High.
- **E10. Scheduled backups run as the person, in the person's session, without the Desktop.** The task
  uses `LogonType` `InteractiveToken` and `RunLevel` `LeastPrivilege` (`internal/schedule/xml.go:162-163`);
  Microsoft: "User must already be logged on. The task will be run only in an existing interactive
  session" [S22]. It runs `mailarchive.exe -job <path>`, not the Desktop. Medium integrity is inferred
  from `LeastPrivilege`; the page does not state it. The task XML names no user; real-host acceptance
  is lab-pending (MA-79). Consequence: a fix that keeps keys only in the Desktop's memory breaks them;
  a fix that uses a labelled file and DPAPI does not (inferred; lab L6).
- **E11. No Windows feature will quietly protect these entries.** Credential Guard covers "NTLM password
  hashes, Kerberos Ticket Granting Tickets (TGTs), and credentials stored by applications as domain
  credentials", is not on Pro editions, and lists "credentials stored in the Windows Vault" among what it
  does not protect [S20]. MailArchive's entries are generic credentials.
- **E12. Everything needed is in pure Go.** `golang.org/x/sys/windows` v0.49.0 (pinned in `go.mod`)
  has `CryptProtectData`, `CryptUnprotectData`, `SecurityDescriptorFromString`, `CreateNamedPipe`,
  `GetNamedPipeClientProcessId`, `OpenProcessToken`, `GetTokenInformation`, `NtQueryInformationProcess`
  and `NewLazySystemDLL` [S21]; the `CRYPTPROTECT_*` constants are exported on its master branch (not
  checked at v0.49.0). `golang.org/x/crypto` v0.58.0 is in `go.mod`, and `internal/vault/aead.go`
  already uses XChaCha20-Poly1305.
- **E13. Comparable apps accept same-person exposure on Windows.** Microsoft's own guidance ranks
  Credential Manager second, after "eliminate passwords" [S26]. Electron: DPAPI protects "but not from
  other apps running in the same userspace" [S27]. Microsoft's sign-in library caches use DPAPI on
  Windows [S28]; the Azure command-line tool keeps "encrypted files on Windows, and plaintext files on
  Linux and macOS" [S29]; Git Credential Manager defaults to Credential Manager [S32].
- **E14. Microsoft's sign-in broker (WAM) is no way out.** It needs "an active, interactive Windows user
  session and be able to display UI"; use from a service or Task Scheduler "(unless specifically
  running as a logged in user)" errors "by design" [S30]. Microsoft's Go sign-in library has no broker
  option [S31], and MailArchive uses `golang.org/x/oauth2` anyway.
- **E15. A Low program cannot click a Medium window.** "UIPI prevents the low-integrity browser code from
  sending any potentially damaging window messages" [S2]; "Applications are permitted to inject input
  only into applications that are at an equal or lesser integrity level" [S23]. It can still show a
  look-alike window.
- **E16. macOS and Linux use plain files, in two places.** All four secrets are owner-only (0600) plain
  text. The client secret, alert password and remembered keys go through one file store under
  `<config folder>/mailarchive/secrets` (`internal/graphconfig/secretstore_default.go:13-22`); the
  Microsoft 365 sign-in is a separate file written by `internal/graph/delegated.go` (the default token
  store is `nil` off Windows, `secretstore_default.go:24-26`). On Windows, one piece of code
  (`credmanStore`) holds all four.
- **E17. macOS keychains.** Two kinds: the file-based one uses per-item app lists; the data protection
  one uses access groups that "must be authorized by a provisioning profile" and is "only available in
  a user login context"; the file-based one "is on the road to deprecation" [S50]. "Unsigned code has no
  DR. Ad hoc signed code ... has a DR but it's tied to that specific version of the code" [S51], so an
  unsigned MailArchive would be asked about again after each update. An item created through
  `/usr/bin/security` trusts that tool, so any program running it reads the item silently [S57][S58][S59].
- **E18. Linux keyrings do not separate programs of one session.** GNOME calls the opposite idea
  "security theater" [S64]; the Secret Service standard "does not mandate any form of access control"
  [S63].
- **E19. Linux command lines.** Default `hidepid=0`: "Everybody may access all /proc/pid directories"
  [S67]. `/proc/net/tcp`'s "uid" field "holds the effective UID of the creator of the socket" [S68], so
  a server can tell which account opened a loopback connection.

### 4.2 Uncertain: the lab must settle these

| | Question | What is known | Lab step |
|---|---|---|---|
| U1 | **Can a Low program in a person's own signed-in session read MailArchive's Credential Manager entries?** The central question; the reviewer showed it only as SYSTEM. | Docs silent (E6). SYSTEM-form lab: yes. My expectation: yes (medium). Research claims win-store-3 and peers-28(a), both "uncertain". | L2 |
| U2 | Does the SYSTEM-form lab over-report? | A lowered SYSTEM token keeps SYSTEM's extra rights; whether the credential service trusts them is UNSOURCED. The lab saved a session-only entry; MailArchive saves "local machine" entries [win-store-14]. | L2 |
| U3 | Can a Low program **write or delete** Credential Manager entries? | UNSOURCED. Decides whether a Low program can plant or erase entries (§5.1 W1, migration). | L3 |
| U4 | Can an AppContainer read them? | Microsoft's isolation sentence is general; "desktop apps only" on the `CredRead` page describes availability, not enforcement [S15][S16]. No published test found. | L4 |
| U5 | Does `CryptUnprotectData` work from Low? | UNSOURCED. Does not matter for the recommended design if its key file is labelled. | L5 |
| U6 | **Can a person's Medium program set a Medium no-read-up label without the "Modify an object label" right?** | Microsoft states only that a person's process can relabel its own object "to a lower level" without it [S44]. 1.7.2's proof ran as SYSTEM lowered. I expect yes (my recollection of the rule "up to the caller's own level"; UNSOURCED). **If no, 1.7.2's Desktop refuses to write its key file and will not start for that person** (it fails closed by design). | L0, first |
| U7 | Can Low read the clipboard? Does Windows clipboard history keep a copied link? | UNSOURCED. | L10 |
| U8 | Do Edge, Chrome and Firefox open a labelled local page and carry on to 127.0.0.1 with the session cookie? | Firefox may read local files from a sandboxed process below Medium (UNSOURCED). Whether a SameSite=Strict cookie set in answer to a page opened from a file is stored: my recollection of the cookie standard draft is that a top-level navigation may set it [S76] (not fetched; medium-low). | L8 |
| U9 | Do DPAPI and Credential Manager work in the scheduled task's logon? | Expected yes: an interactive-token task is not a network logon, and only "Network logon sessions do not have an associated credential set" [S4][S22]. Inferred. | L6 |
| U10 | Does Windows' newer DPAPI-NG offer a per-sandbox rule? | No rule found; "adds nothing" is an absence claim (win-store-7). Not needed for the recommendation. | none |
| U11 | Would a TPM-held key help? | The research's "forces a prompt" overreached: a PIN can be set by the program, or omitted [S48]. Without a PIN it does not separate programs of one person; with one, the PIN must sit where Low cannot read it, which is W1 again. Low's use of the TPM: UNSOURCED (win-store-16). | none |
| U12 | Would checking the connecting program's level reject real browsers? | Chromium can run its network service in an AppContainer; the switch is off by default but can be turned on by policy [S24] (win-store-21). So such a check is fragile. | none |
| U13 | Can a sandboxed Mac app read the secrets folder? | No by default [S53]; a temporary-exception entitlement can name a home path (verifier; UNSOURCED whether that needs App Store review) (mac-linux-7). | MC1 |
| U14 | Can a sandboxed Mac app run `/usr/bin/security` and read an item that trusts it? | Children inherit the parent's sandbox [S49]; the rest UNSOURCED (mac-linux-12). Matters only if that route were chosen; it is not recommended. | MC3 |
| U15 | KWallet's per-app rules can be bypassed by naming another app | A 2008 developer quote; current behaviour not verified (mac-linux-20). | none |
| U16 | Strict Snaps: never given keyring or `~/.config` access? | Keyring access is granted per snap on request; the `personal-files` interface can grant a named dot-file path (mac-linux-23). "Cannot read the secrets" is a default, not a guarantee. | LX2 |
| U17 | Same-user memory reading on Linux | Blocked for non-dumpable processes [S69]; the broad point (A4 is out of reach) stands because A4 can read the files (mac-linux-25). | none |
| U18 | Linux keyring from cron | `godbus` finds the user's message bus at `/run/user/<uid>/bus` without the environment variable [S72]; the logged-out case is UNSOURCED (mac-linux-28). | none |
| U19 | Do keyrings always encrypt at rest? | No: a blank keyring password stores secrets unencrypted [S73] (mac-linux-31). | none |
| U20 | Could pure Go reach WAM directly? | UNSOURCED (peers-11). Moot (E14). | none |

### 4.3 Corrections the verifiers made (kept beside the original claims)

- **peers-1** ("a fix only has to change one interface"): wrong off Windows. There the sign-in token
  is its own file seam (`graph.TokenStore` / `TokenCachePath`), not the secret store. On Windows one
  seam (`credmanStore`) does carry all four (E16).
- **peers-10** (WAM ruled out by scheduling): MailArchive's task is an interactive-token task, which may
  fit WAM's own exception. WAM stays out because it needs a window and the Go library lacks it (E14).
- **peers-21** (Jupyter's redirect file is "a direct, proven fix for P2"): Jupyter's stated threat is
  *other users*. Against a Low program of the same person the file needs the no-read-up label too.
- **win-store-12**: "or a capability" removed (E5).
- **win-store-13**: the web-page process cannot open your files whatever Credential Manager's wording
  means, because its restricted identity list holds only the empty identity [S19].
- **win-store-14**: the raw-file route needs your DPAPI master key, not `CryptUnprotectData` (E8).
- **win-store-18**: the task XML names no user; the run identity relies on `schtasks` filling it in.
- **mac-linux-4**: only the last part of a secret file's path is checked for a link, and the file's
  owner is not checked on read.
- **mac-linux-5**: DC4's own words are in `docs/design-mailarchive-desktop.md:9-10`; the
  confidentiality design restates it.
- **mac-linux-17**: on Linux the Copy-link fallback dialog puts the link on zenity's command line.
- **mac-linux-22**: Snaps use the same secret portal, and it does not isolate from unconfined programs.

### 4.4 Refuted and dropped

- **win-store-5**: "a Low program can copy Credential Manager's files and call `CryptUnprotectData`."
  Refuted: those files decrypt only inside the system's security process (E8).
- **win-store-10**: "a Low program can list process numbers." Refuted on current Windows [S14] and in
  the 1.7.2 lab; trying numbers in turn still works, so the conclusion it supported (E4) survives.
- **peers-1**: refuted in part (§4.3).

---

## 5. Options

Each option says how it works, whether it fits the pure-Go, no-cgo rule, what it protects against
(the attackers of §2), and its costs: moving existing saved secrets, scheduled backups that run
without the Desktop, recovery when the protecting key is lost, and the story on each system.

### 5.1 The Windows secrets (P3)

**W1. Seal each entry with a key kept in a labelled file (recommended).**

- *How.* Once per Windows account, make a 32-byte random seal key. Keep it in
  `%LOCALAPPDATA%\mailarchive\seal-key`, written with 1.7.2's labelled writer
  (`S:(ML;;NRNWNX;;;ME)`: Medium, no-read-up, no-write-up, no-execute-up), and wrap its contents with
  DPAPI (your account only, never `CRYPTPROTECT_LOCAL_MACHINE`, no prompt [S8]) so the file is useless
  if copied alone. Local AppData does not roam (UNSOURCED here), which matches the non-roaming entries
  [S6]. Each Credential Manager entry then holds `version | nonce | XChaCha20-Poly1305(seal key,
  secret)`, with the entry's name bound in as associated data, instead of the secret. Only
  `credmanStore` changes; `DefaultSecretStore` and `DefaultTokenStore` both go through it
  (`internal/graphconfig/secretstore_windows.go:19-26`), so one change covers all four secrets.
- *Pure Go.* Yes (E12), reusing 1.7.2's writer and the vault's existing cipher.
- *Protects against.* A2 (cannot open the key file; sees only ciphertext in Credential Manager), A3,
  A6 (DPAPI), and swapping one entry into another's place (the name is bound in). Not A4.
- *Moving existing secrets.* On first run, read each plain entry, write it back sealed, then record
  "moved" inside the seal-key file. After that, refuse an unsealed entry as tampered (a Low program may
  be able to write entries, U3) and say "sign in again" or "type the password again". Refuse to save if
  the label cannot be set, as 1.7.2 does.
- *Going back to an older version* after the move: 1.7.2 and earlier would read the sealed bytes as the
  secret. The Microsoft 365 sign-in and alert password fail ("sign in again"); a remembered key fails
  to open the archive until it is unlocked once with the password. No mail is lost. Release notes must
  say so.
- *Scheduled backups.* They run as you, at Medium, in your session (E10), so they can open the
  labelled file and call DPAPI (inferred; lab L6).
- *Lost seal key* (file deleted, profile reset, a new PC, or an administrator resetting the Windows
  password, which loses DPAPI keys: UNSOURCED here). Every sealed entry becomes unreadable. Recovery:
  sign in to Microsoft 365 again, type the alert password and client secret again; for each encrypted
  archive, unlock once with its password or recovery key and tick Remember. No mail is lost: the
  archive's own key file holds the master key under the password and under the recovery key
  (`internal/vault/store.go:24-25`). Scheduled backups of encrypted archives fail with plain words
  until then.
- *Size.* The seal adds 44 bytes (4 + 24 + 16) against the 2,560-byte entry limit. Today's sign-in
  entry size is not recorded; measure it. The store already refuses an oversize entry clearly
  (`secretstore_windows.go:74-76`).
- *Denial.* If a Low program can delete entries (U3) or the key file (UNSOURCED), it can break backups
  but not read secrets; the failure words must say what to do.
- *Uninstall.* Keep the seal key while any remembered archive key remains (rule A20 keeps those);
  otherwise the kept keys become useless.
- *Per system.* Windows only. macOS and Linux: §5.3.

**W2. DPAPI with its extra secret in a labelled file (the reviewer's suggestion).** Each entry is
`CryptProtectData(secret, entropy)`, the entropy read from a no-read-up file. Pure Go: yes. Protects
against the same attackers as W1 (E9). Costs: Windows' blob format makes each entry larger than W1's
44 bytes (exact overhead UNSOURCED), it can be tested only on Windows, and moving, scheduled backups
and key loss work as in W1. W1 is the same idea with sealing MailArchive controls and can test on
every system.

**W3. Move the secrets out of Credential Manager into labelled, DPAPI-wrapped files.** One file per
secret under a labelled folder; migrate, then delete the old entries. Pure Go: yes. Protects against
A2, A3, A6, removes the 2,560-byte limit, and takes the entries out of generic Credential Manager
sweeps (a known stealer technique [S36], though those run as A4 anyway). Costs: it contradicts the
shipped promise that the sign-in is "in the vault" (OR2; `docs/design-mailarchive-desktop.md` P4), so
it needs an owner ruling; uninstall must delete the files; more files to label; the same key-loss story.

**W4. Words only.** Say plainly that Credential Manager protects against other accounts and disk
copies, not against programs running as you, sandboxed or not. No code. Leaves C1's attacker able to
take more than C1 protected.

**W5. Keep keys only in the running Desktop's memory.** Protects against A2 and A3 (no memory-read
right upward [S2]). Breaks scheduled backups, which run without the Desktop (E10). Only as an opt-in
"remember until the Desktop quits". Not the main fix.

**W6. A broker service running as SYSTEM** (Chrome's App-Bound Encryption pattern [S12]). Some
protection against A4. Costs: an administrator install, a new privileged service to secure, roaming
breaks, and Chrome's own version has been bypassed in practice [S37]. Disproportionate now.

**W7. An optional main password typed once per session.** Protects against A2, A3, A4 at rest, and
A6. Scheduled backups cannot run while it is locked, which defeats the reason the secrets are kept.
Opt-in only, later; an owner product call.

**Rejected outright.** Session-only saving (E7). A TPM or Windows Hello key (U11). WAM (E14). DPAPI-NG
(U10).

### 5.2 The one-time link (P2)

**L1. Hand the link to the browser through a labelled local page (recommended on Windows).**

- *How.* Write `open-<random>.html` with the labelled writer into MailArchive's Local AppData folder.
  The page holds a small form that submits itself: it POSTs the one-time code to
  `http://127.0.0.1:<port>/_open`. Find the default browser for `http` links (`AssocQueryStringW` from
  `shlwapi.dll`, loaded the way `advapi32.dll` is today), so an editor that owns `.html` files is not
  used, and start it with the file's path. A new POST `/_open` answers exactly as today's link does
  (the 200 page that sets the cookie, then a same-origin step to the dashboard). It accepts a request
  whose origin is a local file, because the code is the only credential, as today. Delete the file
  when the code is used or after 60 seconds; sweep leftovers at start.
- *Pure Go.* Yes.
- *Protects against.* A2 reading command lines: only a file path is visible, and the file cannot be
  opened from Low (E2, E3). A3, A5. It also keeps the code out of the address bar and history.
- *Costs.* Must be proven on Edge, Chrome and Firefox (U8). A browser that cannot read the labelled
  file falls back to L2. Private-network rules for a local page posting to 127.0.0.1: UNSOURCED, lab
  L8. Owes the adversarial pass and a friction review, because it changes the first thing a person
  sees.
- *Per system.* Windows: the main fix. Linux: the same page at 0600 would stop other accounts, but
  Snap and Flatpak browsers may be unable to open a hidden folder (UNSOURCED, lab LX4), so L4 is
  preferred there. macOS: no change needed; the link already travels on standard input. It covers the
  Desktop, `serve` and `setup`, which all open the browser through `internal/browser`.
- Jupyter has shipped the same idea for years, against other users: "a security feature measure was
  added that prevented the authentication token used to launch the browser from being visible" [S34].

**L2. Confirm in the app.** The command line carries only a non-secret code. The page shows a short
number, and the Desktop's own dialog asks "Is your browser showing 4821? Allow". The cookie is issued
only after Allow. Protects against A2 (it cannot click a Medium window, E15) and A3; not against a
look-alike window that tricks the person. Costs one click every time the Desktop opens: a friction
review, and an owner ruling if "it just opens" is a promise. Best kept as L1's fallback.

**L3. Check the connecting program's level (reject).** Look up the program behind each local
connection and refuse callers below Medium. Fragile: a browser's network service can run in an
AppContainer (U12), and there is a race between the connection and the lookup.

**L4. Linux: refuse a link opened by another account (recommended on Linux).** When the link is
redeemed, find the connecting loopback socket in `/proc/net/tcp` and `/proc/net/tcp6` and refuse
unless its uid is MailArchive's own [S68]. This is the narrow form of the deferred item OQ-5, which the
owner ruled "later" (`docs/design-local-confidentiality.md:1434-1439`); reopening it is an owner call.
Pure Go: yes, plain file parsing. Protects against A5 racing the link from `xdg-open`'s command line
even with the default `hidepid=0` [S67]. Does not stop a Snap or any other program running as you
(A3, A4). Costs: IPv4 and IPv6 tests; a socket can be inherited or passed between processes (verifier
note on [S68]).

**L5. Words now.** "Copy MailArchive link" puts a working 10-minute link on the clipboard. 1.7.2's
notes say the sandbox fix covers the Desktop's key file, not every route.

### 5.3 The macOS and Linux secrets

**M1. Keep the owner-only files and say plainly what they protect (recommended).** One sentence in
the Security & privacy panel, the startup text and the README: "Programs you run yourself can read your
saved sign-in, alert password and remembered keys. Other accounts and boxed-in apps (Mac App Store
apps, Flatpaks without home-folder access, strict Snaps) cannot." Keep recommending full-disk
encryption (FileVault, LUKS) for disk copies. Fits DC4 and R-E2. Protects against A3 (by default;
labs MC1, LX2, LX3) and A5. Not A4; not A6 without disk encryption.

**M2. macOS: Developer ID signing, then the data protection keychain or an app group container.**
Real per-app separation: an access group is "protected by code signing", so "no other app can use it"
[S61]; from macOS 15, app group containers get system protection and other apps get a prompt [S55].
Costs: a paid Apple developer account, signing secrets in CI, the hardened runtime, notarization
[S60], an app-like bundle around the command-line tool, and a changed "this app is not signed"
instruction. An owner product decision, not a patch.

**M3. macOS: the file-based login keychain called directly (purego) (not recommended unless M2).** An
unsigned app has no lasting identity (E17), so each update brings the prompt back and scheduled
LaunchAgent backups fail until someone clicks; without the hardened runtime, code injected through
`DYLD_` variables can probably get around the app list [S60] (my inference); and this keychain is on
the way out [S50].

**M4. macOS: the keychain through `/usr/bin/security` (reject).** The common Go library does this
[S57]. The item trusts the `security` tool, so any program of yours that runs it reads the secret
silently [S58][S59]. It adds only encryption at rest.

**M5. Linux: Secret Service through godbus, falling back to files.** No separation between programs of
one session (E18); cron and locked-keyring fallbacks (U18); encryption at rest only when the keyring
has a password (U19). Small gain for two code paths and a new dependency.

**M6. Linux kernel keyring (reject).** Shared by every process of your account, gone at reboot, and the
persistent keyring expires after 3 days by default [S71].

### 5.4 MailArchive's own Windows folders (a second slice)

**F1. Label MailArchive's folders so labels are inherited.** Give `%APPDATA%\mailarchive`,
`%LOCALAPPDATA%\MailArchiveDesktop` and `%LOCALAPPDATA%\mailarchive` (with its `scratch` folder) a
Medium no-read-up label that new files inherit, and relabel the files already there. Protects
settings, logs, job files and, most importantly, an open encrypted archive's decrypted search index
from A2. Pure Go: yes (E12). Costs: a one-time relabel walk; the same U6 question; it does not reach
archive folders the person chooses (whether network shares keep labels: UNSOURCED). Owner call: R-E2
says encryption does not protect the logged-in computer, while C1 accepted A2 as an attacker.

### 5.5 Small fixes found on the way (each its own slice and pass)

- **S1. The alert password follows the server.** When the server, port, security setting or username
  changes, clear the stored password and require it to be typed again. Closes the
  "Desktop session leads to the alert password" path (§3, Other exposures).
- **S2. Uninstall clears the alert password** on every system (today it is left on all three). Keep
  remembered archive keys (A20).
- **S3. Windows `-client-secret-file`** gets the owner and access-list check password files already
  get (`internal/keyring/secretfile_windows.go`), instead of none (`internal/util/securefile.go:155`).

---

## 6. Recommendation

**Windows:** W1 (seal each Credential Manager entry with a key in a DPAPI-wrapped, no-read-up file)
plus L1 (hand the browser its link through a no-read-up page), both built on 1.7.2's labelled-file
writer; L2 only for a browser the lab shows cannot open the page; F1 as a second slice; S1 to S3
alongside. **Linux:** L4 for the link, M1 for the secrets. **macOS:** M1; M2 only together with a
decision to sign with Apple's Developer ID.

**How this closes P2.** The one-time code stops riding on any command line. A Low program sees only a
file path and cannot open the file, which only Medium programs (the browser, MailArchive) can read.
The code reaches MailArchive by a POST from that page, so it never lands in the address bar or history.
One building block serves P2, P3 and 1.7.2's C1, so there is one lab story.

**What it does not do.** A normal program running as you (A4) still reads everything, as in every
comparable app; DC4 and R-E2 stay as ruled.

**Order.** Run the lab first (§7). If L0 fails, that is a 1.7.2 bug to fix at once. If L2 shows a
person's Low program is refused by Credential Manager, W1 drops from a fix to defence in depth (owner
call); L1 is still needed, because P2 rests on a documented Windows rule (E3). Then a design document,
the 10-lens review, the build with prove-fail and prove-pass, the adversarial pass, and the friction
review for L1.

---

## 7. Lab plan (after 1.7.2 ships)

**Where and when.** The owner's new local test PC, or the Windows virtual machine once a person is
signed in to it and no other job is using it. Not before 1.7.2 ships.

**Ground rules.**

- Make a fresh **standard** (not administrator) local Windows account, for example `malab`, and sign in
  to it at the screen. Never use the owner's account.
- Throw-away values only. Never a real Microsoft 365 sign-in, client secret or email password. Lab
  entries are named `mailarchive-lab:*`. MailArchive's own entries are made only with fake values: an
  alert password `LAB-FAKE-SMTP` to the server `smtp.invalid`, and a throw-away encrypted archive of a
  few fake messages with Remember ticked.
- The probe prints sizes and the first 8 hex digits of a SHA-256 of what it read, never the value.
- Probe programs live under the gitignored `.winlab/` folder, as in the 1.7.2 lab, and are never
  committed.
- A **Low child**: `psexec -l` [S42], or a copy of the probe labelled Low with
  `icacls <copy> /setintegritylevel Low` (icacls reference [S43], not fetched) and started from Medium
  (a new program runs at the lower of the two levels [S1]). The probe prints its own level from its
  token, so the result is never assumed.
- An **AppContainer child**: the probe creates a profile and starts itself in it with no capabilities
  [S15].
- **Clean-up**: delete the lab entries and the throw-away archive, sign out, delete `malab`.

| Step | Do | Proves | Pass / fail |
|---|---|---|---|
| L0 | Install 1.7.2 (an administrator may need to approve the installer), then, signed in as `malab`, start MailArchive Desktop. Run `icacls "%LOCALAPPDATA%\MailArchiveDesktop\desktop-instance-s<N>.json"`. | U6: a person's Medium program can set the label; C1 holds outside SYSTEM. | PASS: the Desktop opens and icacls shows `Mandatory Label\Medium Mandatory Level:(NW,NR,NX)`. FAIL: the Desktop does not start, or no label: stop and report at once (1.7.2 bug). |
| L1 | The Low child reads that instance file. | C1 in a person's session. | PASS: "Access is denied". |
| L2 | A Medium probe saves `mailarchive-lab:canary` (generic, local-machine persistence, 20 bytes). The Low child calls `CredRead` on it, on `mailarchive:mailarchive-alert-smtp` and on `mailarchive:archive:<id>` (both fake), and `CredEnumerate` with `mailarchive*`. | U1, U2: P3 in a person's session, with MailArchive's own persistence type. | P3 CONFIRMED if the Low child's hashes match. NOT REPRODUCED if refused (then W1 is defence in depth). |
| L3 | The Low child calls `CredWrite` on `mailarchive-lab:canary` with new bytes, and `CredDelete` on a second lab entry. Never on MailArchive's own entries. | U3: planting and erasing; decides W1's "refuse unsealed entries". | Record allowed or refused for each. |
| L4 | Repeat L2 and L3 from the AppContainer child. | U4. | Expected refused. If allowed, AppContainers join A2 in scope. |
| L5 | A Medium probe writes a DPAPI blob (your account, no prompt) to (a) an unlabelled file and (b) a file written by 1.7.2's labelled writer. The Low child reads (a) and calls `CryptUnprotectData`, then tries to read (b). | E2 in a person's session; U5. W1's building block. | (b) must be "Access is denied". Record both results for (a). |
| L6 | Register a lab scheduled task like MailArchive's (`InteractiveToken`, `LeastPrivilege`) that runs the Medium probe to open the labelled file, unwrap it with DPAPI, and `CredRead` the canary. Run it signed in, then with the screen locked; then try signed out. | E10, U9: unattended backups can use W1. | PASS: works signed in and locked. Signed out: expected not to run at all [S22]; record. |
| L7 | The probe opens a dummy address `http://127.0.0.1:<lab port>/LABTOKEN-<random>` with `rundll32 url.dll,FileProtocolHandler` (MailArchive's own command), once each with Edge, Chrome and Firefox as the default browser. The Low child tries process numbers in turn and reads command lines. | P2 in a person's session, for rundll32 **and** the real browser (the 1.7.2 lab read only its own probe). | P2 CONFIRMED if `LABTOKEN` appears in any command line. |
| L8 | Prototype of L1: a lab server on 127.0.0.1 (the probe, not MailArchive) and a labelled `open-<random>.html` that POSTs a fake code. Start each browser with the file path (found with `AssocQueryStringW`). The server answers 200 with an HttpOnly, SameSite=Strict cookie; the page then moves same-origin; the server records whether the cookie came back. The Low child reads command lines and tries to open the file. | U8: L1 works per browser and closes P2. | PASS per browser: the cookie comes back; Low sees only the path; Low's open is "Access is denied". FAIL: that browser uses L2. |
| L9 | The Low child and the AppContainer child each connect to the lab server. | E5 in a person's session. | Expected: Low connects; the AppContainer is refused. |
| L10 | Run `echo LABCLIP-<random> \| clip` at Medium; the Low child reads the clipboard. Repeat with clipboard history (Windows key + V) on and off, and check whether the entry is listed. | U7: what L5's words must say. | Record. If Low reads it, the words say "any program, even a sandboxed one". |
| L11 | Open the throw-away encrypted archive in the Desktop. The Low child reads `%LOCALAPPDATA%\mailarchive\scratch\...\reader-*.db`. | F1's need. | Expected readable today. |
| L12 | With a 1.7.1 Desktop running, install 1.7.2 by double-clicking the MSI. The Low child reads the instance file before and after a sign-out. | P4 with the real installer. | Expected per the review: readable until the Desktop restarts. |

**macOS** (a throw-away macOS user account; fake files only; never the owner's keychain):

- **MC1.** A tiny test app, ad-hoc signed with the App Sandbox entitlement (whether this runs locally
  without Developer ID: UNSOURCED; check), tries to read a fake
  `~/Library/Application Support/mailarchive/secrets/lab.secret` (0600). Expected: refused [S53] (U13).
- **MC2.** An ordinary unsigned program reads the same file. Expected: read, with no prompt (inferred
  from [S54][S55]).
- **MC3.** Only if a keychain route is ever proposed: make a lab item with
  `security add-generic-password -s mailarchive-lab` in the throw-away account, then from the
  sandboxed app run `/usr/bin/security find-generic-password -s mailarchive-lab -w` (U14).

**Linux** (a throw-away virtual machine or container with two accounts, A and B; fake values only):

- **LX1.** B reads A's `xdg-open` command line holding a fake link. Expected: yes with `hidepid=0`, no
  with `hidepid=2` [S67].
- **LX2.** A strict test Snap with only the `home` interface reads A's `xdg-open` command line
  (expected yes) and `~/.config/mailarchive/secrets/lab.secret` (expected no) [S66].
- **LX3.** A default Flatpak reads the same file (expected no); with `--filesystem=home` (expected yes)
  [S65].
- **LX4.** Snap Firefox and Flatpak Firefox open a 0600 page in `~/.config/...` and in
  `$XDG_RUNTIME_DIR`. Record; decides whether L1 can serve Linux.
- **LX5.** A prototype of L4: a lab server reads `/proc/net/tcp` and `tcp6` for the connecting socket.
  B's connection is refused, A's admitted, over IPv4 and IPv6.

---

## 8. Open questions for the owner

1. **Is a sandboxed (Low-integrity) program running as you an attacker MailArchive defends against on
   Windows?** Recommend **yes**: 1.7.2's C1 already said so, and these secrets are worth more than the
   Desktop session C1 protected. Normal programs running as you stay out of scope (DC4).
2. **If the signed-in lab shows Credential Manager refuses a Low program, fix anyway?** Recommend:
   build L1 regardless (P2 rests on a documented Windows rule); W1 becomes defence in depth, decided
   then.
3. **Keep the secrets in Credential Manager (sealed) or move them to files?** Recommend **keep them in
   Credential Manager, sealed (W1)**: it keeps the "in the vault" promise and changes one piece of
   code. Move to files (W3) only if the 44 extra bytes push a real sign-in past the 2,560-byte limit.
4. **What happens when the seal key is lost?** Recommend: accept "sign in again, type the passwords
   again, unlock each encrypted archive once with its password", in plain words; no hidden spare copy
   of the seal key.
5. **Uninstall and the seal key?** Recommend: keep it while any remembered archive key remains (A20),
   otherwise delete it; and clear the alert password on uninstall (S2).
6. **How should the browser get its link on Windows?** Recommend the labelled page (L1, no extra
   click); the "Allow" dialog (L2) only for a browser that cannot open the page.
7. **Linux: reopen OQ-5 in its narrow form now (refuse links opened by other accounts)?** Recommend
   **yes**: on Linux, other accounts can read command lines by default.
8. **Change Mac and Linux storage now?** Recommend **no**: words only (M1). Revisit the Mac keychain
   only together with a decision to pay for Apple Developer ID signing (M2).
9. **Label MailArchive's own Windows folders as a second slice (F1)?** Recommend **yes**: the scratch
   folder holds decrypted mail of open encrypted archives.
10. **Require the alert password again when the alert server changes (S1)?** Recommend **yes**.
11. **Where to run the lab?** Recommend the new local test PC, in a fresh standard account; the virtual
    machine only when no other job needs it.

---

## Sources

Confidence is given where each source is used. "Not fetched" means neither the research nor this
note reached the page.

Windows:

- [S1] Mandatory Integrity Control. https://learn.microsoft.com/en-us/windows/win32/secauthz/mandatory-integrity-control
- [S2] Understanding and Working in Protected Mode Internet Explorer (Windows Vista integrity mechanism, 2007, archived). https://learn.microsoft.com/en-us/previous-versions/dotnet/articles/bb625962(v=msdn.10)
- [S3] SYSTEM_MANDATORY_LABEL_ACE. https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-system_mandatory_label_ace
- [S4] CredReadW. https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credreadw
- [S5] CredEnumerateW. https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credenumeratew
- [S6] CREDENTIALW (persistence types). https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw
- [S7] James Forshaw, Dumping Stored Credentials with SeTrustedCredmanAccessPrivilege (2021). https://www.tiraniddo.dev/2021/05/dumping-stored-credentials-with.html
- [S8] CryptProtectData. https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata
- [S9] Windows Data Protection (DPAPI white paper). https://learn.microsoft.com/en-us/previous-versions/ms995355(v=msdn.10)
- [S10] GhostPack SharpDPAPI README. https://github.com/GhostPack/SharpDPAPI
- [S11] SpecterOps, Operational Guidance for Offensive User DPAPI Abuse (2018). https://specterops.io/blog/2018/08/22/operational-guidance-for-offensive-user-dpapi-abuse/
- [S12] Google Security Blog, Improving the security of Chrome cookies on Windows (30 July 2024). https://security.googleblog.com/2024/07/improving-security-of-chrome-cookies-on.html
- [S13] ntdoc, PROCESSINFOCLASS (community reference). https://github.com/m417z/ntdoc/blob/main/descriptions/processinfoclass.md
- [S14] Sandboxie issue 5678 (process list below Medium, Windows 11 24H2). https://github.com/sandboxie-plus/Sandboxie/issues/5678
- [S15] Implementing an AppContainer. https://learn.microsoft.com/en-us/windows/win32/secauthz/implementing-an-appcontainer
- [S16] AppContainer isolation. https://learn.microsoft.com/en-us/windows/win32/secauthz/appcontainer-isolation
- [S17] Project Zero, Understanding Network Access in Windows AppContainers (2021). https://projectzero.google/2021/08/understanding-network-access-windows-app.html
- [S18] Troubleshooting UWP app connectivity (Windows Firewall). https://learn.microsoft.com/en-us/windows/security/operating-system-security/network-security/windows-firewall/troubleshooting-uwp-firewall
- [S19] Chromium Sandbox design. https://chromium.googlesource.com/chromium/src/+/main/docs/design/sandbox.md
- [S20] Credential Guard overview. https://learn.microsoft.com/en-us/windows/security/identity-protection/credential-guard/
- [S21] golang.org/x/sys/windows v0.49.0. https://pkg.go.dev/golang.org/x/sys@v0.49.0/windows
- [S22] Task Scheduler LogonType. https://learn.microsoft.com/en-us/windows/win32/taskschd/principal-logontype and https://learn.microsoft.com/en-us/windows/win32/taskschd/taskschedulerschema-logontype-principaltype-element
- [S23] SendInput. https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendinput
- [S24] Chromium sandbox policy features. https://chromium.googlesource.com/codesearch/chromium/src/+/HEAD/sandbox/policy/features.cc
- [S25] Microsoft Security Servicing Criteria for Windows. https://www.microsoft.com/en-us/msrc/windows-security-servicing-criteria
- [S26] Handling Passwords. https://learn.microsoft.com/en-us/windows/win32/secbp/handling-passwords
- [S27] Electron safeStorage. https://www.electronjs.org/docs/latest/api/safe-storage
- [S28] MSAL.NET token cache serialization. https://learn.microsoft.com/en-us/entra/msal/dotnet/how-to/token-cache-serialization ; MSAL Go extensions accessor. https://pkg.go.dev/github.com/AzureAD/microsoft-authentication-extensions-for-go/cache/accessor
- [S29] MSAL-based Azure CLI. https://learn.microsoft.com/en-us/cli/azure/msal-based-azure-cli
- [S30] Using MSAL.NET with WAM. https://learn.microsoft.com/en-us/entra/msal/dotnet/acquiring-tokens/desktop-mobile/wam
- [S31] MSAL for Go, apps/public. https://pkg.go.dev/github.com/AzureAD/microsoft-authentication-library-for-go/apps/public
- [S32] Git Credential Manager credential stores. https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/credstores.md
- [S33] gh auth login. https://cli.github.com/manual/gh_auth_login
- [S34] Jupyter Server configuration (`use_redirect_file`). https://jupyter-server.readthedocs.io/en/latest/other/full-config.html
- [S35] Raymond Chen, How do I get the command line of another process? https://devblogs.microsoft.com/oldnewthing/?p=15923
- [S36] MITRE ATT&CK T1555.004, Windows Credential Manager. https://attack.mitre.org/techniques/T1555/004/
- [S37] Chrome App-Bound Encryption bypasses (secondary reports; the CyberArk original now redirects). https://cybersecuritynews.com/chrome-c4-bomb-attack ; https://cyberinsider.com/this-is-how-infostealers-bypass-chromes-latest-cookie-security/
- [S38] Microsoft Graph permissions reference. https://learn.microsoft.com/en-us/graph/permissions-reference
- [S39] Microsoft identity platform refresh tokens. https://learn.microsoft.com/en-us/entra/identity-platform/refresh-tokens
- [S40] Go `os` package (UserConfigDir, Chmod on Windows). https://pkg.go.dev/os
- [S41] Go `net/smtp` PlainAuth. https://pkg.go.dev/net/smtp#PlainAuth
- [S42] Sysinternals PsExec. https://learn.microsoft.com/en-us/sysinternals/downloads/psexec
- [S43] icacls command reference (not fetched). https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/icacls
- [S44] Modify an object label (security policy setting). https://learn.microsoft.com/en-us/previous-versions/windows/it-pro/windows-10/security/threat-protection/security-policy-settings/modify-an-object-label
- [S45] Security information (WRITE_OWNER to set a label). https://learn.microsoft.com/en-us/windows/win32/secauthz/security-information
- [S48] Microsoft Q&A on TPM key protection; pcpcrypto. https://learn.microsoft.com/en-gb/answers/questions/1076208/ncryptcreatepersistedkey-any-property-to-set-user ; https://pkg.go.dev/github.com/ElMostafaIdrassi/pcpcrypto

macOS:

- [S49] Apple Developer Forums 706390 (a sandboxed process's child inherits its sandbox). https://developer.apple.com/forums/thread/706390
- [S50] TN3137: On Mac keychain APIs and implementations. https://developer.apple.com/documentation/technotes/tn3137-on-mac-keychains
- [S51] TN3127: Inside Code Signing: Requirements. https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements
- [S52] Accessing files from the macOS App Sandbox. https://developer.apple.com/documentation/security/accessing-files-from-the-macos-app-sandbox
- [S53] Protecting user data with App Sandbox. https://developer.apple.com/documentation/security/protecting-user-data-with-app-sandbox
- [S54] Apple Platform Security, Controlling app access to files. https://support.apple.com/guide/security/controlling-app-access-to-files-secddd1d86a6/web
- [S55] Protecting local app data using containers. https://developer.apple.com/documentation/xcode/protecting-local-app-data-using-containers
- [S56] Access Control Lists (keychain). https://developer.apple.com/documentation/security/access-control-lists
- [S57] security(1) manual; zalando/go-keyring macOS code. https://raw.githubusercontent.com/apple-oss-distributions/Security/main/SecurityTool/macOS/security.1 ; https://github.com/zalando/go-keyring/blob/master/keyring_darwin.go
- [S58] Silverfort, keychain item readable through the security tool (28 July 2026). https://www.silverfort.com/?p=20709
- [S59] SecKeychainAddGenericPassword. https://developer.apple.com/documentation/security/seckeychainaddgenericpassword(_:_:_:_:_:_:_:_:)
- [S60] Hardened Runtime; allow-dyld-environment-variables. https://developer.apple.com/documentation/security/hardened-runtime ; https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.cs.allow-dyld-environment-variables
- [S61] Sharing access to keychain items among a collection of apps. https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps
- [S62] ebitengine/purego. https://github.com/ebitengine/purego

Linux:

- [S63] Secret Service API specification. https://specifications.freedesktop.org/secret-service/latest/ch10.html ; https://specifications.freedesktop.org/secret-service/latest/transfer-secrets.html
- [S64] GNOME Keyring Security Philosophy. https://wiki.gnome.org/Projects/GnomeKeyring/SecurityPhilosophy
- [S65] Flatpak sandbox permissions. https://docs.flatpak.org/en/latest/sandbox-permissions.html
- [S66] snapd `home` interface and default AppArmor template. https://raw.githubusercontent.com/canonical/snapd/master/interfaces/builtin/home.go ; https://raw.githubusercontent.com/canonical/snapd/master/interfaces/apparmor/template.go
- [S67] proc(5). https://man7.org/linux/man-pages/man5/proc.5.html
- [S68] proc_pid_net(5). https://man7.org/linux/man-pages/man5/proc_pid_net.5.html
- [S69] Yama (ptrace_scope). https://www.kernel.org/doc/html/latest/admin-guide/LSM/Yama.html
- [S70] crontab(5). https://man7.org/linux/man-pages/man5/crontab.5.html
- [S71] keyrings(7). https://man7.org/linux/man-pages/man7/keyrings.7.html
- [S72] godbus session bus discovery. https://raw.githubusercontent.com/godbus/dbus/master/conn_other.go
- [S73] ArchWiki, GNOME/Keyring. https://wiki.archlinux.org/title/GNOME/Keyring
- [S74] Flatpak Secret portal. https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Secret.html

Web:

- [S76] Cookies: HTTP State Management Mechanism (RFC 6265bis draft), storage of SameSite cookies on top-level navigation (not fetched; from memory). https://datatracker.ietf.org/doc/html/draft-ietf-httpbis-rfc6265bis

---

## How this note was made

- **Drew on.** The secrets inventory and three research angles, each verified claim by claim, from this
  workflow; the code at `1a6d437` (re-read for `internal/browser/browser.go`,
  `internal/graphconfig/secretstore_windows.go`, `internal/instance/handoff.go`,
  `internal/desktop/alerts.go`, `internal/alert/alert.go`, `internal/schedule/xml.go`,
  `internal/uninstall/credentials.go`, `internal/util/securefile.go`, `go.mod`, and the design rules
  DC4, non-goal 2, OQ-5, A20 and R-E2); `2d55148` and `5523487` through `git show`.
- **Did not check.** The web pages were not fetched again for this note; [S43] and [S76] were not
  fetched at all; everything marked UNSOURCED is unsourced. No lab, no program, no tests were run.
- **The smallest checks that would settle the most:** lab L0 (does 1.7.2's Desktop start for a standard
  account), L2 (P3 in a person's session) and L7 (P2 against the real browser).
- **Counters.** Register: research and options. Borrowed lines: short quotes from Microsoft, Google,
  Apple, Chromium, freedesktop, GNOME, snapd, Jupyter and the Linux manual pages, each attributed.
  Drift: none noticed. Touched: one new worktree and branch, and this file. Persona: none. Sources
  reached: as listed above.
