#!/usr/bin/env python3
"""Lab script for the "Sync Thunderbird before archiving" design (docs/design-thunderbird-presync.md, section 9.4).

macOS only. Standard library only. Run it yourself in Terminal; it is NOT run by MailArchive.

What it does and does not do
  * It reads your Thunderbird profile list and the file sizes/times (never mail contents) to see how Thunderbird
    behaves, and it writes ONE report file in your home folder (mailarchive-presync-lab-report.txt) to paste back.
  * The "start-quit", "agent" and "quiet" steps START your real Thunderbird hidden and ask it to quit through its own
    quit request. Starting Thunderbird makes it do what it normally does when it starts: connect to your mail
    servers, fetch mail, possibly send anything waiting in its Outbox, show notifications, sync calendars and chat.
    Empty the Outbox first. Close Thunderbird first (the script refuses to run if it is already running).
  * It never kills or signals any process, never edits a Thunderbird file, and never sends keystrokes.
  * The "agent" step installs a temporary launchd job in ~/Library/LaunchAgents and removes it afterwards.
  * macOS will probably show a permission prompt: "<program> wants to control Thunderbird". WRITE DOWN WHICH PROGRAM
    NAME THE PROMPT SHOWS. That is the main thing this lab is for.

Usage (from the folder that holds this file):
  python3 presync-lab-mac.py check                  # read-only: bundle id, profiles, help text, idle time
  python3 presync-lab-mac.py start-quit             # hidden start, observe, ask Thunderbird to quit (from Terminal)
  python3 presync-lab-mac.py agent                  # the same start and quit, run by a launchd job (the real question)
  python3 presync-lab-mac.py quiet --minutes 8      # start hidden and log file changes while you send yourself an email

Run them in that order. After each, the report file grows. When done, paste the whole report back.
"""
import argparse
import getpass
import hashlib
import os
import plistlib
import re
import subprocess
import sys
import time

APP = "/Applications/Thunderbird.app"
BUNDLE_ID = "org.mozilla.thunderbird"
REPORT = os.path.expanduser("~/mailarchive-presync-lab-report.txt")
AGENT_LABEL = "com.mailarchive.presynclab"
AGENT_PLIST = os.path.expanduser("~/Library/LaunchAgents/%s.plist" % AGENT_LABEL)
QUIT_SCRIPT = 'if application id "%s" is running then tell application id "%s" to quit' % (BUNDLE_ID, BUNDLE_ID)


def log(msg):
    line = "[%s] %s" % (time.strftime("%Y-%m-%d %H:%M:%S"), msg)
    print(line)
    with open(REPORT, "a") as f:
        f.write(line + "\n")


def run(argv, timeout=30, env_c=False):
    """Run one command with an argument list (never a shell). Returns (rc, stdout, stderr)."""
    env = dict(os.environ)
    if env_c:
        env["LC_ALL"] = "C"
    try:
        p = subprocess.run(argv, capture_output=True, text=True, timeout=timeout, env=env)
        return p.returncode, p.stdout.strip(), p.stderr.strip()
    except subprocess.TimeoutExpired:
        return 124, "", "timed out after %ds" % timeout
    except OSError as e:
        return 127, "", str(e)


def thunderbird_pids():
    pids = set()
    for name in ("thunderbird", "thunderbird-bin"):
        rc, out, _ = run(["/usr/bin/pgrep", "-x", "-u", str(os.getuid()), name])
        if rc == 0:
            pids.update(int(x) for x in out.split() if x.isdigit())
    return sorted(pids)


def profiles():
    """The profiles Thunderbird itself lists, from profiles.ini (read only)."""
    base = os.path.expanduser("~/Library/Thunderbird")
    ini = os.path.join(base, "profiles.ini")
    found = []
    try:
        cur = {}
        for raw in open(ini, encoding="utf-8", errors="replace").read().splitlines():
            line = raw.strip()
            if line.startswith("["):
                if cur.get("Path"):
                    found.append(cur)
                cur = {}
            elif "=" in line:
                k, v = line.split("=", 1)
                cur[k.strip()] = v.strip()
        if cur.get("Path"):
            found.append(cur)
    except OSError as e:
        log("cannot read %s: %s" % (ini, e))
    out = []
    for p in found:
        path = p["Path"] if p.get("IsRelative") == "0" else os.path.join(base, p["Path"])
        out.append((p.get("Name", "?"), os.path.normpath(path), p.get("Default") == "1"))
    return out


def pick_profile(args):
    if getattr(args, "profile", None):
        return os.path.abspath(os.path.expanduser(args.profile))
    ps = profiles()
    if not ps:
        sys.exit("No profile found in ~/Library/Thunderbird/profiles.ini. Pass --profile <folder>.")
    for name, path, default in ps:
        if default:
            return path
    return ps[0][1]


def confirm(what):
    print()
    print(what)
    print("Type YES to continue, anything else to stop: ", end="", flush=True)
    if sys.stdin.readline().strip() != "YES":
        sys.exit("Stopped. Nothing was started.")


def idle_seconds():
    rc, out, _ = run(["/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4"], timeout=15)
    m = re.search(r'"HIDIdleTime"\s*=\s*(\d+)', out) if rc == 0 else None
    return int(m.group(1)) / 1e9 if m else None


def describe_process(pid):
    rc, out, err = run(["/bin/ps", "-p", str(pid), "-o", "pid=,etime=,comm="], env_c=True)
    log("L-M4 ps pid/etime/comm: rc=%d %r %s" % (rc, out, err))
    rc, out, err = run(["/bin/ps", "-p", str(pid), "-o", "args="], env_c=True)
    log("L-M4 ps args: rc=%d %r %s" % (rc, out, err))


def check_lock(profile):
    lock = os.path.join(profile, "lock")
    try:
        log("L-M3 lock: %s -> %s" % (lock, os.readlink(lock)))
    except OSError as e:
        log("L-M3 lock: %s is not a readable symlink (%s)" % (lock, e))
    parent = os.path.join(profile, "parent.lock")
    log("L-M3 parent.lock exists: %s" % os.path.exists(parent))


def cmd_check(args):
    log("=== check (read only) ===")
    log("macOS: %s" % run(["/usr/bin/sw_vers"])[1].replace("\n", "; "))
    try:
        info = plistlib.load(open(APP + "/Contents/Info.plist", "rb"))
        log("L-M1 bundle id: %s (expected %s); version %s" % (info.get("CFBundleIdentifier"), BUNDLE_ID, info.get("CFBundleShortVersionString")))
    except Exception as e:
        log("L-M1 cannot read %s Info.plist: %s" % (APP, e))
    for name, path, default in profiles():
        log("profile listed by Thunderbird: name=%s default=%s path=%s exists=%s" % (name, default, path, os.path.isdir(path)))
    log("Thunderbird running now: %s" % (thunderbird_pids() or "no"))
    rc, out, err = run([APP + "/Contents/MacOS/thunderbird", "-help"], timeout=20)
    quit_lines = [l for l in (out + "\n" + err).splitlines() if re.search(r"quit|headless|remote|new-instance|no-remote", l, re.I)]
    log("L-M9 'thunderbird -help' rc=%d; lines mentioning quit/headless/remote: %s" % (rc, quit_lines or "none"))
    s = idle_seconds()
    log("L-M10 owner idle seconds from ioreg HIDIdleTime: %s (move the mouse and rerun to see it reset)" % s)
    log("check done. Report: %s" % REPORT)


def start_hidden(profile):
    if thunderbird_pids():
        sys.exit("Thunderbird is already running (pids %s). Close it first; this lab only runs when it is not." % thunderbird_pids())
    t0 = time.time()
    rc, out, err = run(["/usr/bin/open", "-g", "-j", APP, "--args", "-profile", profile])
    log("L-M2 open -g -j rc=%d out=%r err=%r" % (rc, out, err))
    pids = []
    for _ in range(40):
        time.sleep(1)
        pids = thunderbird_pids()
        if pids:
            break
    log("L-M2 Thunderbird pids after %.0fs: %s" % (time.time() - t0, pids or "none (did it start?)"))
    return pids


def quit_request():
    """The one quit request: the constant AppleScript, addressed by bundle id, with a hard 20 s limit."""
    t0 = time.time()
    rc, out, err = run(["/usr/bin/osascript", "-e", QUIT_SCRIPT], timeout=20)
    log("L-M8 osascript quit rc=%d out=%r err=%r (took %.1fs). rc 0 = request accepted; 'not allowed'/-1743 = no permission; -1712 = timed out waiting for the prompt" % (rc, out, err, time.time() - t0))
    return rc


def wait_gone(seconds):
    for i in range(seconds):
        if not thunderbird_pids():
            log("L-M8 Thunderbird exited after %ds" % i)
            return True
        time.sleep(1)
    log("L-M8 Thunderbird STILL RUNNING after %ds (pids %s)" % (seconds, thunderbird_pids()))
    return False


def observe_and_quit(profile, who):
    pids = start_hidden(profile)
    if not pids:
        return
    time.sleep(10)
    for pid in pids:
        describe_process(pid)
    check_lock(profile)
    log("Look at your screen and Dock now: is there a Thunderbird window or Dock icon (L-M2)? Waiting 20s.")
    time.sleep(20)
    log("%s: now asking Thunderbird to quit. A permission prompt may appear. NOTE THE PROGRAM NAME IT SHOWS." % who)
    quit_request()
    wait_gone(90)


def cmd_start_quit(args):
    profile = pick_profile(args)
    log("=== start-quit from Terminal, profile %s ===" % profile)
    confirm("This starts your real Thunderbird hidden with profile\n  %s\nfor about 2 minutes, then asks it to quit.\nIt may fetch mail, send what is in the Outbox, and show notifications. The Outbox should be empty." % profile)
    observe_and_quit(profile, "Terminal run")
    log("start-quit done.")


def cmd_agent(args):
    profile = pick_profile(args)
    log("=== agent (launchd job) run, profile %s ===" % profile)
    confirm("This installs a temporary launchd job, starts it now (%s), and removes it. The job starts Thunderbird hidden with\n  %s\nthen asks it to quit.\nStay at the computer: a permission prompt may appear and WAITS for you." % (AGENT_LABEL, profile))
    uid = os.getuid()
    me = os.path.abspath(__file__)
    agent_out = os.path.expanduser("~/mailarchive-presync-lab-agent.log")
    plist = {
        "Label": AGENT_LABEL,
        "ProgramArguments": ["/usr/bin/python3", me, "agent-job", "--profile", profile],
        "RunAtLoad": False,
        "StandardOutPath": agent_out,
        "StandardErrorPath": agent_out,
    }
    os.makedirs(os.path.dirname(AGENT_PLIST), exist_ok=True)
    with open(AGENT_PLIST, "wb") as f:
        plistlib.dump(plist, f)
    try:
        log("bootstrap: rc=%s" % (run(["/bin/launchctl", "bootstrap", "gui/%d" % uid, AGENT_PLIST])[0],))
        log("kickstart: rc=%s" % (run(["/bin/launchctl", "kickstart", "gui/%d/%s" % (uid, AGENT_LABEL)])[0],))
        deadline = time.time() + 240
        while time.time() < deadline:
            time.sleep(5)
            rc, out, _ = run(["/bin/launchctl", "print", "gui/%d/%s" % (uid, AGENT_LABEL)])
            if rc != 0 or "state = not running" in out:
                break
        log("launchd job finished or timed out. Its output follows:")
        try:
            for line in open(agent_out, errors="replace").read().splitlines():
                log("  agent| " + line)
        except OSError as e:
            log("  no agent output: %s" % e)
    finally:
        run(["/bin/launchctl", "bootout", "gui/%d/%s" % (uid, AGENT_LABEL)])
        try:
            os.remove(AGENT_PLIST)
            log("removed temporary job %s" % AGENT_PLIST)
        except OSError as e:
            log("could not remove %s (%s): delete it by hand" % (AGENT_PLIST, e))
    log("L-M8 key question: did the permission prompt name 'python3'/'Python' (the scheduled program) and did this run's quit work without the Terminal grant covering it? Record what you saw.")


def cmd_agent_job(args):
    """Runs INSIDE launchd. No confirmation prompt: it was confirmed by the parent."""
    profile = pick_profile(args)
    print("agent-job start, uid=%d, profile=%s" % (os.getuid(), profile), flush=True)
    t0 = time.time()
    pids = start_hidden(profile)
    if pids:
        time.sleep(10)
        for pid in pids:
            describe_process(pid)
        time.sleep(20)
        quit_request()
        wait_gone(90)
    print("agent-job end after %.0fs" % (time.time() - t0), flush=True)


def snapshot(roots):
    snap = {}
    for root in roots:
        for dp, dns, fns in os.walk(root, followlinks=False):
            for fn in fns:
                p = os.path.join(dp, fn)
                try:
                    st = os.lstat(p)
                    snap[p] = (st.st_size, int(st.st_mtime))
                except OSError:
                    pass
    return snap


def cmd_quiet(args):
    profile = pick_profile(args)
    roots = [os.path.join(profile, d) for d in ("ImapMail", "Mail") if os.path.isdir(os.path.join(profile, d))]
    log("=== quiet test, profile %s, watching %s for %d minutes ===" % (profile, roots, args.minutes))
    confirm("This starts Thunderbird hidden for %d minutes and logs file sizes and times (never contents) under the mail folders.\nDuring the test, send yourself an email to an IMAP account. Close Thunderbird first; the Outbox should be empty." % args.minutes)
    pids = start_hidden(profile)
    if not pids:
        return
    log("send yourself an email NOW. The log notes every second in which any file changed.")
    prev = snapshot(roots)
    t0 = time.time()
    last_change = t0
    while time.time() - t0 < args.minutes * 60:
        time.sleep(2)
        cur = snapshot(roots)
        changed = [os.path.relpath(p, profile) for p in cur if prev.get(p) != cur[p]] + [os.path.relpath(p, profile) for p in prev if p not in cur]
        if changed:
            last_change = time.time()
            log("L-M5 +%.0fs changed %d file(s): %s" % (last_change - t0, len(changed), changed[:6]))
        prev = cur
    log("L-M5 last file change was +%.0fs; quiet for the final %.0fs" % (last_change - t0, time.time() - last_change))
    log("Now asking Thunderbird to quit.")
    quit_request()
    wait_gone(90)
    log("quiet test done.")


def main():
    ap = argparse.ArgumentParser(description="Lab for the Thunderbird pre-sync design (macOS).")
    ap.add_argument("cmd", choices=["check", "start-quit", "agent", "agent-job", "quiet"])
    ap.add_argument("--profile", help="Thunderbird profile folder (default: the one profiles.ini marks default)")
    ap.add_argument("--minutes", type=int, default=8)
    args = ap.parse_args()
    if sys.platform != "darwin":
        sys.exit("This lab is for macOS.")
    log("--- %s run by %s ---" % (args.cmd, getpass.getuser()))
    {"check": cmd_check, "start-quit": cmd_start_quit, "agent": cmd_agent, "agent-job": cmd_agent_job, "quiet": cmd_quiet}[args.cmd](args)


if __name__ == "__main__":
    main()
