# Adversarial pass — delegated (device-code) Graph capture (slice a)

**Reviewed:** the built code — `internal/graph/delegated.go`, `internal/graph/graph.go`
(`userSeg`, `httpTransport`), `internal/util/securefile.go`, and the app/CLI wiring
(`internal/app/graph.go` `buildGraphClient`, `cmd/mailarchive` `runGraph`/`job.go`) —
against the design's new attack surface: a **bearer refresh token at rest** and the
**device-code flow**. Personas: outside aggressor, malicious/curious local user,
integrity/concurrency. **9 findings raised · 9 verified · 0 BLOCKER · 0 HIGH.** The
new surface is either mitigated in code or documented Type-I; the residuals are LOW
and fail closed. No code change required to ship; the LOW notes are recorded.

| # | Persona | Claim | Verdict | Evidence / disposition |
|---|---|---|---|---|
| A1 | outside | Device-code phishing: a victim runs the tool but enters an **attacker's** code, signing the attacker's session into the victim's mailbox | CONFIRMED (Type-I, documented) | The tool only ever redeems the code from its OWN `DeviceAuth` (`deviceConsent`: `conf.DeviceAuth` → `DeviceAccessToken(ctx, da)` on that same `da`) — it never accepts an externally-supplied code. The residual social-engineering vector is inherent to RFC 8628; `graph-app-setup.md` tells the operator to verify the consent screen names `mailarchive` and to enter only a code the tool just printed. (D-C6) |
| A2 | local | The refresh token is readable by another local user | REFUTED | Written `0600` (`storeTokenCache` `Chmod(0o600)`; `os.CreateTemp` is `0600`; dir `0700`) under `os.UserConfigDir()`, never inside `-out`; read back only through `util.ReadSecureFile` which refuses a group/world-readable file (`0o077`-clear). Root can read it — same posture as the client-secret file, stated plaintext-at-rest (D-C7). |
| A3 | outside | A spoofed `/me` (or a tampered cache `upn`) makes the tool archive, or claim to archive, the wrong mailbox | REFUTED | The mailbox walked is the LIVE `/me` UPN (`buildGraphClient` calls `client.Me`), never the token's self-asserted subject nor the cache's stored `upn` (that field is only a label + default-path input). A `-mailbox` that disagrees with live `/me` is refused before any walk. In prod `/me` is TLS to graph.microsoft.com. |
| A4 | local | A planted **symlink** at the token-cache path redirects a read or a write | REFUTED | Read: `ReadSecureFile` uses `Lstat` + `O_NOFOLLOW` and refuses a non-regular file (re-checked on the open descriptor). Write: `os.CreateTemp` (random, `O_EXCL`) + `os.Rename` over the path replaces the symlink itself, never writes through it. |
| A5 | concurrency | Two runs sharing one cache clobber each other's rotated refresh token, bricking one | CONFIRMED (LOW, mitigated) | `storeTokenIfNewer` persists only a later-expiry token, and `deviceSource.Token` reloads the cache and retries once on a refresh failure a concurrent rotation could explain (D-C2). A tight race (loser reads before the winner writes) still fails ONE run — closed, no corruption, self-heals next run. Doc: one schedule per mailbox. |
| A6 | local | The refresh/access token leaks into a log or an error | REFUTED | The logger prints only `Signed in as <upn>` and the device *user code* (short-lived, single-use, meant to be shown). oauth2 refresh errors carry the token endpoint's error body (`error`/`error_description`), never the refresh token we POST; `statusErr` bounds Graph error bodies to 2 KB and Graph never echoes the bearer. |
| A7 | integrity | A crash mid-write leaves a torn token read later as valid | REFUTED | Atomic temp-file + `Sync` + `Rename` (`storeTokenCache`); a crash before rename keeps the prior valid token, after rename has the new one. A torn temp is never renamed into place. Fails toward re-consent. |
| A8 | local | An oversize/garbage cache wedges the tool or is trusted | REFUTED | `ReadSecureFile` bounds the cache at 64 KB; malformed JSON is a hard error naming "delete it and sign in again"; a missing cache is `os.ErrNotExist`-branched (interactive re-consent, or an unattended refusal with the sign-in remedy). |
| A9 | usability-as-security | A `-mailbox` mismatch is only caught AFTER an interactive sign-in + a cache write | CONFIRMED (LOW) | `NewDelegated` may consent and write the cache before `buildGraphClient`'s `Me`-vs-`-mailbox` check refuses. The sign-in is the operator's OWN and the cache is legitimately theirs; only the wrong-`-mailbox` RUN is refused (no wrong archive). Acceptable; a wasted consent is the whole cost. |

**Verdict: PASS (ship).** The device flow adds no BLOCKER/HIGH. Two LOW residuals
(A5 tight-race single-run failure; A9 post-consent mismatch) are fail-closed and
documented, not data-loss. The read-only (GET-only) core is unchanged: `Mail.Read`
delegated, no write scope, `userSeg` the only routing change. The prove-fail records
are in the slice-a commit.
