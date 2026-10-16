#!/usr/bin/env python3
"""Assemble revision 2 of the design from the eight drafts and check its structure.

usage: python3 -I assemble_rev2.py DRAFTS_OUTPUT OUT_FILE REPORT_DIR
"""
import collections
import hashlib
import json
import os
import re
import sys

EXPECTED_H2 = ["## 1. Problem", "## 2. Properties", "## 3. Non-goals", "## 4. Mechanism", "## 5. Security",
               "## 6. Owner-facing text", "## 7. Migration and coexistence", "## 8. Operator walks",
               "## 9. Test strategy", "## 10. Catalog rows proposed", "## 11. Build slices",
               "## 12. Honesty of claims", "## 13. Owner questions", "## 14.", "## 15."]
EXPECTED_H3 = ["4.1", "4.2", "4.3", "4.4", "4.5", "4.6", "4.7", "4.8", "4.9", "4.10", "5.1", "5.2", "5.3", "5.4",
               "7.1", "7.2", "7.3", "7.4", "7.5", "7.6", "9.1", "9.2", "9.3", "9.4"]


def result_of(o):
    for k in ("result", "returnValue", "output", "value"):
        if isinstance(o.get(k), dict) and "drafts" in o[k]:
            return o[k]
    if "drafts" in o:
        return o
    raise SystemExit("no drafts in output")


def main():
    src, out, rep = sys.argv[1], sys.argv[2], sys.argv[3]
    with open(src, encoding="utf-8") as fh:
        res = result_of(json.load(fh))
    md = {}
    for d in res["drafts"]:
        dr = d.get("draft") or {}
        md[d["id"]] = (dr.get("markdown") or "").strip("\n")
        if not md[d["id"]]:
            raise SystemExit(f"empty draft from {d['id']}")
    g3 = md["G3"]
    m = re.search(r"^### 4\.7\b", g3, flags=re.M)
    if not m:
        raise SystemExit("G3 has no ### 4.7 heading to split at")
    g3a, g3b = g3[:m.start()].rstrip("\n"), g3[m.start():].strip("\n")
    parts = [md["G1"], md["G2"], g3a, md["G4"], g3b, md["G5"], md["G6"], md["G7"], md["G8"]]
    text = "\n\n".join(parts) + "\n"
    if not re.search(r"^## 4\. Mechanism", text, flags=re.M):
        text = re.sub(r"^(### 4\.1\b)", "## 4. Mechanism\n\n\\1", text, count=1, flags=re.M)
        print("inserted '## 4. Mechanism'")
    heads = re.findall(r"^(#{1,4}) (.+)$", text, flags=re.M)
    h2 = [f"## {t}" for l, t in heads if l == "##"]
    h3 = [t.split()[0] for l, t in heads if l == "###"]
    probs = []
    pos = -1
    for e in EXPECTED_H2:
        idx = next((i for i, h in enumerate(h2) if h.startswith(e)), None)
        if idx is None:
            probs.append(f"missing H2 {e}")
        elif idx < pos:
            probs.append(f"H2 out of order {e}")
        else:
            pos = idx
    dup = [h for h, n in collections.Counter(h2).items() if n > 1]
    if dup:
        probs.append(f"duplicate H2 {dup}")
    for e in EXPECTED_H3:
        if e not in h3:
            probs.append(f"missing H3 {e}")
    dup3 = [h for h, n in collections.Counter(h3).items() if n > 1]
    if dup3:
        probs.append(f"duplicate H3 {dup3}")
    sc_cited = sorted({int(x) for x in re.findall(r"\bSC(\d+)\b", text)})
    missing_sc = sorted(set(range(1, 54)) - set(sc_cited))
    old_ids = sorted(set(re.findall(r"\bMA-(?:58\d|59\d|60\d)\b", text)))
    em = text.count("—")
    with open(out, "w", encoding="utf-8") as fh:
        fh.write(text)
    digest = hashlib.sha256(text.encode("utf-8")).hexdigest()
    os.makedirs(rep, exist_ok=True)
    with open(os.path.join(rep, "folded.md"), "w", encoding="utf-8") as fh:
        for d in res["drafts"]:
            dr = d.get("draft") or {}
            fh.write(f"# {d['id']} {d['title']}\n\n## folded\n")
            for f in dr.get("folded", []):
                fh.write(f"- {f['condition']}: {f['where']}\n")
            fh.write("\n## open_issues\n")
            for x in dr.get("open_issues", []):
                fh.write(f"- {x}\n")
            fh.write("\n## cross_refs\n")
            for x in dr.get("cross_refs", []):
                fh.write(f"- [{x['depends_on']}] {x['sentence']}\n")
            fh.write("\n")
    print("wrote", out, len(text), "bytes", text.count("\n"), "lines; sha256", digest)
    print("structure problems:", probs or "none")
    print("SCs never cited:", missing_sc or "none")
    print("revision-1 ids still present:", old_ids or "none")
    print("em-dashes in text:", em)


if __name__ == "__main__":
    main()
