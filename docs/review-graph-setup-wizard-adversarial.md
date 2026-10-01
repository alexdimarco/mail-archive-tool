# Adversarial pass — Graph setup wizard (slice a + the write surface)

**Reviewed (built code):** `internal/graphconfig` (config + file/Credential-Manager
stores), `internal/server/setup.go` + `setup_page.go` (the wizard handler), and the
`cmd/mailarchive` wiring (`runSetup`, `runGraph` config/secret resolution). New
surface: a loopback credential-write endpoint + a client secret at rest. Personas:
outside aggressor (browser/network), local user, integrity. **11 findings · 11
verified · 0 BLOCKER · 0 HIGH.**

| # | Persona | Claim | Verdict | Evidence / disposition |
|---|---|---|---|---|
| A1 | outside | A page on another site drives `127.0.0.1` to write credentials (CSRF) | REFUTED | POST requires a per-process CSRF token the cross-site page cannot read (same-origin DOM), AND a loopback `Origin` (browsers set `Origin` on cross-site POSTs → rejected), AND a loopback `Host`. Defence in depth; prove-fail test MA-267 goes red if the guard is removed. |
| A2 | outside | DNS-rebinding (hostname → 127.0.0.1) bypasses the Origin check | REFUTED | The handler also requires `IsLoopback(r.Host)`; a rebound request carries the attacker's hostname in `Host` → refused (MA-267 third case). |
| A3 | outside | A cross-site `<form>` (no fetch) posts the config | REFUTED | Triple backstop: the JSON body won't come from a urlencoded form (decode 400), the CSRF token is unknown to the attacker, and `Origin` is cross-site. |
| A4 | local | The secret is echoed to the client or logged | REFUTED | MA-266: the POST response and every GET report only `configured`, never the value; the handler logs no request body; the plaintext is dropped (`in.Secret = ""`) once stored. |
| A5 | local | The secret lands in the config file | REFUTED | MA-266 asserts the on-disk config bytes never contain the secret; it lives only in the store. |
| A6 | local | The secret at rest is readable by another user | REFUTED (file) / LAB (Win) | File store: `0600`, read through `util.ReadSecureFile` (loose mode refused) — plaintext at rest, same posture as `-client-secret-file` (stated). Windows: Credential Manager, the user's own vault (MA-264, lab). |
| A7 | outside | A huge POST body exhausts memory | REFUTED | `http.MaxBytesReader(w, r.Body, 64KB)` bounds the request. |
| A8 | local | The account→filename mapping allows path traversal | REFUTED | The account key is a non-sensitive `graph:<hash8>-<hash8>`; `fileStore.path` maps anything outside `[A-Za-z0-9_-]` to `_`, so no separator/`..` reaches the path. |
| A9 | integrity | A crash mid-write leaves a torn config/secret | REFUTED | Both use `util.WriteFileAtomic0600` (temp+fsync+rename); a crash yields old-or-whole, never torn. |
| A10 | outside | The wizard is reachable from the network | REFUTED | `mailarchive setup` refuses a non-loopback `-addr` (MA-265); the reader `serve` gains NO write route (W-C1), so R19's read-only posture is intact. |
| A11 | integrity | An Origin-absent request slips through | CONFIRMED (LOW, backstopped) | A same-origin fetch may omit `Origin`, so the handler allows an absent `Origin` — but the CSRF token AND the loopback `Host` are still required, so this is not exploitable on its own. Noted. |

**Verdict: PASS (ship).** 0 BLOCKER/HIGH. The write surface is loopback-only,
CSRF- and rebind-guarded, and the secret is write-only (never echoed, logged, or in
the config file). LOW: Origin-absent is allowed but backstopped by token+Host (A11);
a failed store Delete on reconfigure is best-effort (an old secret could linger —
cosmetic). The CSRF token uses `crypto/rand` with a weak fallback only if the OS RNG
fails, where Origin+Host still gate. Prove-fail recorded (MA-267). Windows Credential
Manager (MA-264) is lab-pending.
