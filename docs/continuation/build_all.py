import os
import subprocess
import sys

S = "/private/tmp/claude-503/-Users-alex-dimarco-repositories/86c4cdf3-3c8e-4270-a184-12f76f0288e7/scratchpad"
REPO = "/Users/alex.dimarco/repositories/mail-archive-tool"
OUT = "/Users/alex.dimarco/repositories/mail-archive-builds/" + sys.argv[1]
GO = S + "/gobin/go"
HOME = S + "/home-build"
os.makedirs(HOME, exist_ok=True)

targets = [
    ("darwin", "arm64"),
    ("darwin", "amd64"),
    ("linux", "amd64"),
    ("windows", "amd64"),
]
cmds = ["mailarchive", "mailarchive-desktop", "mailarchive-gui"]

failed = []
for goos, goarch in targets:
    d = f"{OUT}/{goos}-{goarch}"
    os.makedirs(d, exist_ok=True)
    for c in cmds:
        name = c + (".exe" if goos == "windows" else "")
        env = dict(os.environ)
        env.update({
            "HOME": HOME,
            "XDG_CONFIG_HOME": HOME + "/.config",
            "GOOS": goos,
            "GOARCH": goarch,
            "CGO_ENABLED": "0",
        })
        r = subprocess.run(
            [GO, "-C", REPO, "build", "-trimpath", "-o", f"{d}/{name}", f"./cmd/{c}"],
            env=env, capture_output=True, text=True)
        status = "ok" if r.returncode == 0 else "FAILED"
        print(f"{goos}/{goarch} {c}: {status}")
        if r.returncode != 0:
            failed.append((goos, goarch, c, r.stderr[-500:]))
for f in failed:
    print("----", f)
sys.exit(1 if failed else 0)
