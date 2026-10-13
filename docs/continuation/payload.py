"""Build the Windows MSI payload the way packaging/build-release.sh does, without touching dist/ or the repo."""
import hashlib
import os
import shutil
import subprocess
import sys
import zipfile

S = "/private/tmp/claude-503/-Users-alex-dimarco-repositories/86c4cdf3-3c8e-4270-a184-12f76f0288e7/scratchpad"
REPO = "/Users/alex.dimarco/repositories/mail-archive-tool"
OUT = "/Users/alex.dimarco/repositories/mail-archive-builds/" + sys.argv[1] + "/windows-msi-payload"
GO = S + "/gobin/go"
HOME = S + "/home-build"
os.makedirs(HOME, exist_ok=True)

STAGE = OUT + "/stage"
PAY = STAGE + "/mailarchive-desktop-windows-msi-payload"
shutil.rmtree(OUT, ignore_errors=True)
os.makedirs(PAY + "/branding")
os.makedirs(PAY + "/docs")

env = dict(os.environ)
env.update({"HOME": HOME, "XDG_CONFIG_HOME": HOME + "/.config",
            "GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0"})

builds = [
    ("./cmd/mailarchive-desktop", "mailarchive-desktop.exe", ["-ldflags", "-H=windowsgui"]),
    ("./cmd/mailarchive-gui", "mailarchive-gui.exe", ["-ldflags", "-H=windowsgui"]),
    ("./cmd/mailarchive", "mailarchive.exe", []),
]
for pkg, name, flags in builds:
    r = subprocess.run([GO, "-C", REPO, "build"] + flags + ["-o", PAY + "/" + name, pkg],
                       env=env, capture_output=True, text=True)
    print(name, "ok" if r.returncode == 0 else "FAILED " + r.stderr[-400:])
    if r.returncode != 0:
        sys.exit(1)

shutil.copy(REPO + "/packaging/mailarchive-desktop/README-payload.txt", PAY + "/READ ME.txt")
bdir = REPO + "/packaging/mailarchive-desktop/branding"
for f in os.listdir(bdir):
    shutil.copy(os.path.join(bdir, f), PAY + "/branding/" + f)
shutil.copy(REPO + "/LICENSE", PAY + "/docs/LICENSE.txt")
shutil.copy(REPO + "/packaging/release-notes.md", PAY + "/docs/release-notes.md")
shutil.copy(REPO + "/docs/goback.md", PAY + "/docs/goback.md")
shutil.copy(REPO + "/docs/graph-app-setup.md", PAY + "/docs/graph-app-setup.md")

zpath = OUT + "/mailarchive-desktop-windows-msi-payload.zip"
with zipfile.ZipFile(zpath, "w", zipfile.ZIP_DEFLATED) as z:
    for dirpath, _, files in sorted(os.walk(PAY)):
        for f in sorted(files):
            p = os.path.join(dirpath, f)
            z.write(p, os.path.relpath(p, STAGE))
shutil.rmtree(STAGE)

h = hashlib.sha256(open(zpath, "rb").read()).hexdigest()
open(OUT + "/SHA256SUMS", "w").write(f"{h}  mailarchive-desktop-windows-msi-payload.zip\n")
print(h, os.path.getsize(zpath) // 1024, "KiB")
with zipfile.ZipFile(zpath) as z:
    for n in z.namelist():
        print(" ", n)
