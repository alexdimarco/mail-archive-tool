import hashlib
import os
import sys

root = "/Users/alex.dimarco/repositories/mail-archive-builds/" + sys.argv[1]
lines = []
for dirpath, _, files in sorted(os.walk(root)):
    for f in sorted(files):
        if f == "SHA256SUMS.txt":
            continue
        p = os.path.join(dirpath, f)
        h = hashlib.sha256(open(p, "rb").read()).hexdigest()
        lines.append(f"{h}  {os.path.relpath(p, root)}  ({os.path.getsize(p) // 1024} KiB)")
open(os.path.join(root, "SHA256SUMS.txt"), "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
