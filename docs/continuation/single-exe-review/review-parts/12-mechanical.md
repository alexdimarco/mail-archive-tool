## The mechanical check

`docs/design-single-executable.md` carries a BUILD STATUS line naming this file, and this file exists. No
test in this repository reads a design's BUILD STATUS line (a search of `assureblock/`, `internal/` and
`cmd/` finds none), so the check is by hand, as it was for the Thunderbird pre-sync review. Nothing in the
design is built. The catalog, the UX contract, `assureblock/`, the baselines, the rulings log, `CLAUDE.md`
and the permission settings were not touched by the review or by revision 2.

## Residual risk of this review

- **The synthesis owner wrote the design.** It judged nothing, and the record behind every row is in the
  run's journal, but the composition of the conditions (which rescue's change wins where two collide) is
  the owner's. A second reviewer should read revision 2 against the critic's six re-check items before SE0:
  the composed identity predicate traced case by case, the one schedule-kind mechanism, the composed
  no-argument table, the Windows kill scope and the files-in-use path, the id and question allocation, and
  the owner-ruling surface (that no "none needed" hides a locked amendment).
- **The rescues saw one finding each.** The per-finding rescue found 46 modest changes; only the critic saw
  them together, and it found twelve conflicts. The re-run of the stale lenses on revision 2 (SC{{RERUN}}) is
  the check that the composition did not open a new hole.
- **Two model families.** Finders, merge and most skeptic verdicts ran on Claude Fable 5.1; the later
  skeptics, the rescues and the critic on Claude Opus 5.5. A finding's two skeptics may have run on
  different models. No verdict was taken from a mix within one agent.
- **Vendor behaviour from memory.** Several facts are the agents' reading of how Windows, macOS and the MSI
  engine behave (Restart Manager under `/qn` and exit 3010, LaunchServices activating a running bundle on a
  second open, Gatekeeper app translocation, `GetConsoleWindow` under ConPTY, the MSI downgrade guard).
  Each carries a lab item in revision 2 for that reason. Lens 9's check that `zenity` does no work at
  import time was made against the upstream source at v0.10.12, because the module cache is not readable
  here; L-L2 stays.
- **Relaunches.** Results were replayed from the run's own journal, only when the finding's title matched;
  failed attempts were re-run and never counted. The replay file and the scripts that built it are session
  scratch, not part of the repository.
