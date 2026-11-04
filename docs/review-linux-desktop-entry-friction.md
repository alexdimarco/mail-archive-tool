# Friction review: the Linux app-menu download (1.5.8)

Walked 4 November 2026 in Docker, as a first-time Linux person, by a workflow subagent.
Process: `../assurance-kit/process/friction-review.md`.

**Verdict: SHIPPABLE_WITH_NOTES.** The three lines work as written, with no administrator
rights, and `desktop-file-validate` accepts the menu entry with no warnings. One real dead
end was found: on GNOME the entry is **not in the menu at all** until the person signs out
and back in, while the READ ME said it "is now in your app menu". The words are fixed in
this commit. The cause (the entry finds the program through PATH) is a design choice;
proposal P1 removes it and was checked in the same lab.

| Header | |
|---|---|
| Reviewed | branch `app-icon` at 599e2ff; fixes in the commit that adds this file |
| Download walked | `mailarchive-linux-desktop-entry.tar.gz` from `packaging/build-release.sh v0.0.0` (Docker `golang:1.26`, `zip` added), listed in SHA256SUMS; sha256 `fe126eb2…cf254d`. Re-walked after the fix: the same script's tarball, sha256 `90238922…30c7c4` |
| Environment | Docker `ubuntu:24.04` (arm64, the Mac host's own architecture), GLib 2.80.0, `desktop-file-utils`, `hicolor-icon-theme`, `python3-gi`. No display, no real GNOME or KDE session |
| Actor | a first-time Linux person (`pat`, a new non-root account, fresh home from Ubuntu's defaults); downloads placed as a browser leaves them: in `~/Downloads`, no run permission |
| Commands and results | unpack: ok. Three lines (first line edited to arm64, as the READ ME says for ARM): exit 0; program 755, icon and entry 644. `desktop-file-validate`: no errors, no warnings. GLib `Gio.DesktopAppInfo.new("mailarchive.desktop")`: refused (not loaded) while `~/.local/bin` is not on PATH; loaded after a new sign-in (Name MailArchive Desktop, Icon mailarchive, shown in menus). `~/.local/bin/mailarchive desktop` with no `~/.local/bin` on PATH: starts ("MailArchive Desktop is running…") |
| Cells walked | 15 |
| Before the fix | fully 9, partly 4, not 2; high friction 2 |
| After the fix | fully 12, partly 3, not 0; high friction 0 |

## How the menu was "read"

There is no GUI in Docker, so the menu was asked through GLib, the library GNOME Shell
lists apps with: `Gio.DesktopAppInfo` loads an entry only if the program in its `Exec`
line can be found on PATH. If it cannot, the entry is dropped: GNOME shows nothing, not a
broken item. KDE Plasma was not walked (no image); from reading, it lists the entry and
fails only on click, but that is not verified.

Ubuntu's (and Debian's) default `~/.profile` adds `~/.local/bin` to PATH **only if that
folder exists at sign-in**. For most people it does not exist until the first line makes
it, so the session that ran the three lines never has it. Fedora's `~/.bashrc` adds it
always; Arch adds it never (both from reading, not walked).

## The walk

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| 1. First-time × find the download (README table, release notes) | yes | Both rows say it is optional and to follow its READ ME.txt | — | none |
| 2. First-time × unpack | yes | One folder, four files; the file manager's Extract does the same | — | none |
| 3. First-time × step 1, put the program in the folder | yes | The browser puts the program in Downloads; the READ ME said "download … into this folder", which a browser does not do. One move by hand | II (the tarball holds no program, by the owner's ruling) | **Fixed:** "Download it … then move it here from your Downloads folder" |
| 4. First-time × amd64 or arm64 | partly | "an ARM computer, such as a Raspberry Pi": a Pi on the 32-bit system cannot run arm64. On ARM, line 1 is edited by hand | II, low | **Fixed:** "a 64-bit ARM computer, such as a Raspberry Pi running a 64-bit system". The hand edit stays (P2 would remove it) |
| 5. First-time × "open a terminal in this folder" | yes | How was not said | III | **Fixed:** "right-click an empty spot in the folder and choose Open in Terminal" |
| 6. First-time × the three lines | yes | exit 0, no administrator rights, right permissions | — | none |
| 7. First-time × look in the menu right after (GNOME) | **no → partly** | The entry is not in the menu. The READ ME said it "is now in your app menu" and covered only "if clicking it does nothing": a dead end | II (Exec relies on PATH) | **Fixed:** "If MailArchive Desktop is not in your app menu yet, or clicking it does nothing, sign out and back in once", with the reason. **P1** removes the sign-out |
| 8. First-time × after signing out and back in (Ubuntu/Debian) | yes | GLib loads it: name MailArchive Desktop, icon mailarchive, shown in menus | — | none |
| 9. First-time × a system that never puts `~/.local/bin` on PATH | **no → partly** | The entry never appears, and the READ ME had nothing to try next | II | **Fixed:** "you can always start it from a terminal with `~/.local/bin/mailarchive desktop`" (checked: it starts). **P1** makes the menu work here too (checked) |
| 10. Cold operator × validate the entry | yes | `desktop-file-validate`: nothing to report | — | none |
| 11. Cold operator × icon lookup | yes (by reading) | `hicolor/256x256/apps` is in the system's hicolor index, and Icon=mailarchive matches the file name. Not drawn on a screen | — | none |
| 12. First-time × update later | yes (by reading) | Shut down is at the top of the page (`dashboard.go`, `shutdownBtn`). "run the first line again with the new program" left out where the new program goes. The README's upgrade table said only "replace the old binary on your PATH" | III | **Fixed:** the READ ME says "put the new program in this folder, and run the first line again"; the README's Linux upgrade row names `~/.local/bin/mailarchive` and that line |
| 13. First-time × remove | partly → yes | "delete the three files" deletes the program but leaves the scheduled backups: on Linux they are crontab lines that run that program, so they would fail with no sign | III | **Fixed:** "first click the Uninstall MailArchive button … it stops your scheduled backups", then delete the files; only the last two for the menu entry alone. **P3** for the uninstall's own last step |
| 14. First-time × the README's wizard section | partly → yes | It said the wizard opens from "the `.desktop` entry" on Linux; the new entry opens MailArchive Desktop | III | **Fixed:** "`mailarchive wizard` in a terminal on Linux (the optional app-menu entry opens MailArchive Desktop, not the wizard)" |
| 15. Cold operator × checksums | yes | SHA256SUMS lists the tarball | — | none |

Not walked, and not implied: KDE Plasma's menu; a real click with a display (the browser
opening); a real Raspberry Pi; Fedora and Arch sessions (their PATH is from reading).

The drill's friction log, as it was written:

```
FRICTION: step 1: the program was downloaded to ~/Downloads, not into the unpacked folder; moved it there by hand
FRICTION: step 2: this box is ARM, so the first line had to be edited by hand (amd64 -> arm64)
FRICTION: before: 'mailarchive' is not on PATH in this session (~/.local/bin missing from PATH)
```

## What changed in this commit (words only)

- `packaging/linux/README-desktop-entry.txt` (the tarball's READ ME.txt): cells 3, 4, 5, 7,
  9, 12, 13, and the SVG line now says it can be left where it is. The three install lines
  are unchanged.
- `README.md`: the wizard section (cell 14) and the Linux upgrade row (cell 12).
- `cmd/mailarchive/ma1080_linux_entry_test.go`: MA-1080 now also pins the READ ME's words
  for cells 7, 9 and 13 (missing from the menu → sign out and back in; the start command
  that needs no PATH; Uninstall MailArchive named before deleting the files).
  Prove-fail: run on the READ ME as 599e2ff has it, red (three phrases missing, and the
  order check); with the new READ ME, green. MA-1080's row is amended to say so.
- Full suite on the Mac (`go test -count=1 -p 4 -timeout 20m ./...`): every package green
  except `internal/app`, which ran out of time at 20 minutes with no test failing (the Mac
  was loaded). Run alone, `internal/app` is green in 859 s. A first run with the default
  10-minute limit also timed out `internal/desktop` (green in the second run) and failed
  `TestFricOwnerTextNamesWhatHappenedNotTheSystemCall` once; that test is green alone.
  None of these packages reads a file this commit changes, except two `internal/app`
  README tests, which are green.

## Proposals (behaviour changes, for the owner)

- **P1 (recommended for 1.5.8): put the program's full path in the menu entry.** The third
  line becomes
  `sed "s|^Exec=mailarchive |Exec=\"$HOME/.local/bin/mailarchive\" |" mailarchive.desktop | install -D -m 644 /dev/stdin ~/.local/share/applications/mailarchive.desktop`.
  Checked in the same lab: `desktop-file-validate` accepts it, and GLib loads and shows the
  entry with **no** `~/.local/bin` on PATH. Then cells 7 and 9 need no sign-out and no
  terminal. Cost: the line is longer to paste, and MA-1080's pinned line changes, so it is
  the owner's call.

  *Settled 4 November 2026 (the lead's call, built by a workflow subagent on `app-icon`
  after 4549de1):* **built as proposed.** READ ME.txt's third line is now exactly the line
  above; the shipped `mailarchive.desktop` is unchanged (`Exec=mailarchive desktop`, still
  valid on its own), and the line rewrites only its `Exec` when it installs it. The shell
  expands `$HOME` when the line runs (a .desktop file expands neither `~` nor `$HOME`), and
  the path is double-quoted in `Exec`, so a home folder with a space works. Under the lines
  the READ ME now says the third line is long, to copy it whole, and that it writes where
  the program is into the menu entry. The "not in your app menu yet → sign out and back in
  once" paragraph stays as a fallback, with the PATH reason replaced by "some menus only
  notice a new entry after a new sign-in", which was not checked.
  - MA-1080's test now expects this line word for word (an intended change of the pinned
    text, not a weakening), and on Linux it also runs the three lines for real for a home
    folder named `pat smith` and checks the installed entry is the shipped one with only
    `Exec="<home>/.local/bin/mailarchive" desktop` changed. Prove-fail: with the old READ ME,
    red on the Mac (the line) and in Docker `golang:1.26` (the line, and the installed
    entry's `Exec`); with the new READ ME, green on both.
  - Re-walked in Docker `ubuntu:24.04` (arm64, GLib 2.80.0-6ubuntu3.10) as a new non-root
    user whose home is `/home/pat smith`, with the tarball from `build-desktop-entry.sh`
    (built on the Mac) and the program cross-compiled for linux/arm64: the three lines exit
    0 (first line edited to arm64); program 755, icon and entry 644; the installed entry
    differs from the shipped one only in `Exec="/home/pat smith/.local/bin/mailarchive"
    desktop`; `desktop-file-validate` reports nothing. GLib loads it and shows it in menus
    **in the same login session that ran the lines** (no `~/.local/bin` on PATH), and in a
    session whose PATH never has it. GLib's own launch (what a menu click calls) started
    `/home/pat smith/.local/bin/mailarchive desktop`. So cells 7 and 9 need no sign-out
    and no terminal, as far as GLib decides.
  - Limits, found and not fixed: GLib's `get_executable()` returns the raw first word of
    `Exec` with its quote (`"/home/pat` here), so anything that matches a window to its app
    by that value may not match; the launch itself is right. A home folder whose path holds
    `"`, `\`, `` ` ``, `$`, `%`, `&` or `|` would need escaping the line does not do (none is
    usual). Not walked: a real GNOME or KDE session redrawing its menu live, Fedora, Arch.
- **P2 (later): one command instead of three lines.** A verb in the program that writes the
  icon and the entry with its own full path would remove the move, the ARM edit and P1's
  long line.
- **P3 (small): the uninstall's last step on Linux** says only "delete the mailarchive
  binary (path)". It could also name the menu entry and icon, if they are there.
- **P4 (small):** started as `~/.local/bin/mailarchive desktop` with no `~/.local/bin` on
  PATH, the program says "If your browser does not open, run: mailarchive desktop
  -copy-link", a command that is not found on that system. It could name the path it was
  started from.
