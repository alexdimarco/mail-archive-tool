# Adversarial + doc-coherence review — mailbox categories

> As of 2026-09-04, over the tree at `277213d` (the categories and extractable
> slices merged). Method: the assurance-kit adversarial pass run as a workflow —
> two attacker lenses (outside aggressor via mail content; insider/integrity)
> over the untrusted category-string paths (the PST multi-value blob parser,
> X-Mozilla-Keys, Graph), skeptic-verified against the built binary, plus a
> doc-coherence check of the new status/categories prose. 3 adversarial findings
> confirmed (all low), 0 refuted; 4 doc-coherence findings. Fixes landed in
> `0723e99`.

The headline attack classes were closed in the build and re-confirmed here:
`parseMVUnicode` is fuzz-clean (bounds the untrusted count before allocating,
no panic or over-read), category markup is HTML-escaped and control-stripped,
and a category value can never forge a Status segment or shadow a header field
on `reindex -rebuild` read-back (its own dedicated page field). The confirmed
findings are hardening: a missing memory bound on the sibling mbox tag reader
and a gate-strength mismatch in the extractable count.

## Adversarial findings

| # | Lens | Sev. | Verdict | Finding | Disposition |
|---|---|---|---|---|---|
| INT-CAT-1 | insider-integrity | low | CONFIRMED | emlPresent applies a weaker gate than extract, so status/verify over-report the extractable cou | Fixed (0723e99): emlPresent applies extract's identical path gate. MA-194. |
| OUTAGG-CAT-1 | outside | low | CONFIRMED | mbox/maildir X-Mozilla-Keys category reader has no size/count bound (memory amplification) | Fixed (0723e99): mozillaCategories caps scanned bytes and token count. MA-191. |
| OUTAGG-CAT-2 | outside | low | PARTIAL | PST single-valued (PT_UNICODE) Keywords read is uncapped while the sibling PT_MV_UNICODE branch | Fixed (0723e99), informational: the single-value Keywords read is capped like the MV branch. |

## Doc-coherence findings

| # | Sev. | Finding | Disposition |
|---|---|---|---|
| F1 | high | Design K1 (and its §4 test list) still says the mbox reader reads a "Keywords" header for categories | design K1/§4 corrected to X-Mozilla-Keys only (0723e99) |
| F2 | medium | Design §4 build-order describes categories rendered/read-back "in the Status row"; design K1/K3/§3.1 | design §4 corrected to the dedicated Categories field (0723e99) |
| F3 | medium | Three references cite MA-194 for the real-PST-categories lab row, which is actually MA-197 (MA-194 i | three MA-194 lab refs corrected to MA-197 (0723e99) |
| F4 | low | MA-179's enumerated Graph $select is missing `categories`, which the code now sends and MA-192 docum | MA-179 $select enumeration gains categories (0723e99) |
