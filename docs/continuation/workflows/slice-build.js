export const meta = {
  name: 'slice-build',
  description: 'Build one design slice catalog-first with prove-fail/prove-pass, check it against its design, then run its owed adversarial pass: three finders, a refutation-default skeptic, fixes with tombstones, a fresh-eyes re-verify',
  phases: [
    { title: 'Confirm', detail: 'facts the design requires before code, when the slice names any' },
    { title: 'Build', detail: 'catalog-first commit, then code; every fix prove-fail then prove-pass' },
    { title: 'Conform', detail: 'independent check against the design rows and the gates' },
    { title: 'Attack', detail: 'three finders with distinct lenses, each told to break it' },
    { title: 'Skeptic', detail: 'refutation-default verdict on every finding' },
    { title: 'Fix', detail: 'confirmed findings fixed with tombstones; report filed' },
    { title: 'Re-verify', detail: 'fresh eyes on every fix' },
  ],
}

const A = args
const S = A.scratch
const WT = A.worktree
const REPO = A.repo
const BR = A.branch
const KIT = '/Users/alex.dimarco/repositories/crescendum-tools/assurance-kit'
const HOMEDIR = `${S}/home-${A.slug}`
const note = k => (A.resumeNote && A.resumeNote[k]) ? `

RESUME NOTE (an earlier attempt at this stage was interrupted when the laptop slept)
${A.resumeNote[k]}` : ''
// Cheaper model for agents that have not run yet (owner, 2026-10-10: Opus was eating the quota).
// args.model ('sonnet') and args.effort apply to every agent, or, when resuming, only to the labels
// or stage keys in args.modelLabels, so finished agents keep their cached results.
const STAGE = l => l.startsWith('attack') ? 'attack' : l.startsWith('lens') ? 'lens' : l === 'fix 2' ? 'fix2' : l === 're-verify conditions' ? 'check' : l
const tier = l => {
  if (!A.model) return {}
  if (A.modelLabels && !A.modelLabels.includes(l) && !A.modelLabels.includes(STAGE(l))) return {}
  const effort = (A.effortBy && (A.effortBy[l] || A.effortBy[STAGE(l)])) || A.effort
  return effort ? { model: A.model, effort } : { model: A.model }
}
const run = (prompt, opts) => agent(prompt, { ...opts, ...tier(opts.label) })

const SHELL = `SHELL RULES. The user's settings make Claude Code ask a person about any command it cannot fully analyse, so follow these exactly:
- Read, search and edit with the Read, Edit and Write tools. This session has no Glob or Grep tool: search with one plain \`grep -rn ...\` or \`find ...\` per Bash call (a trailing \`| head\` is fine).
- Bash: ONE simple command per call, absolute paths, \`git -C <dir>\` and the Go wrappers below. No \`cd\`, no \`&&\`, \`;\` or \`||\`, no loops, no \`$(...)\` or backticks, no heredocs, no code on stdin, no \`python3 -c\`, \`sh -c\` or \`bash -c\`.
- No environment assignment in front of a command (\`FOO=bar cmd\` asks a person). The wrappers set what is needed.
- Single-quote every argument that holds shell punctuation: regexes, globs, \`%(...)\`, \`~\`, \`!\`, \`<\`, \`>\`, \`*\`, \`?\`, \`[\`, \`|\`.
- Never name a path outside /Users/alex.dimarco/repositories and ${S} in ANY argument, not even as a test input you expect to be refused. A path a check must refuse is made inside the repo or the scratchpad.
- Never read ~/.claude, ~/Library, /var/folders (Go's test temp dirs), /opt/homebrew, ~/go or another Go install. Go's sources are in ${S}/go126/go/src (standard library) and ${S}/gopath/pkg/mod (modules).
- If a read is refused, do not retry it or look for another way to the same file: work from what is in the tree.
- A script, if you need one, is a file under ${S} that you run as \`python3 -I <file>\`.`

const GOB = `GO WRAPPERS (Go 1.26.9 in the scratchpad; never use another go):
- ${S}/gobin/go -C <module dir> <go args>
- ${S}/gobin/gofmt -l <dir>
- ${S}/gobin/vet3 <module dir>   (go vet ./... for darwin, linux and windows)
- ${S}/gobin/gofor <darwin|linux|windows> <go args>   (any other cross-OS go command)
- ${S}/gobin/inhome <dir under ${S}> <command> [args]   (HOME, XDG_CONFIG_HOME, APPDATA and LOCALAPPDATA pointed at that dir)
Run every test run and every built CLI binary through inhome with your own dir, so nothing reads or writes the real user's settings, sign-ins or schedules, e.g.
  ${S}/gobin/inhome ${HOMEDIR} ${S}/gobin/go -C ${WT} test ./...
Other suites run on this machine at the same time. A scaled-timer test that fails only under load: re-run it alone (\`-run '^TestName$' -count=3\`) before concluding anything, and never widen a timing margin past what its catalog row allows.`

const SAFETY = `SAFETY:
- Commit locally on ${BR} in ${WT} only. Never push. Never touch another branch or worktree, and never run git reset --hard, git clean, git stash, git rebase, or git checkout over files you did not change.
- Never edit assureblock/, a baseline, or docs/owner-rulings-2026-10-09.md. Never delete a covers marker, add a suppression, or loosen an existing assertion. A red gate means the code is wrong; if you believe a gate or a design text is wrong, stop that part and report it.
- No real OS scheduler, crontab, launchd, Task Scheduler or credential-store writes, and nothing under ~/Library. Tests use fakes (httptest for Graph); no test touches the network.
- Never print or store a token or secret.
- Commit messages: write the message to a file under ${S}/msg/ with the Write tool, then run \`git -C ${WT} commit -F <file>\`. Every message ends with these two lines:
Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018J6ooKQ18JxYSDhXtCmaDc`

const CONTEXT = `CONTEXT
- Slice ${A.id}: ${A.title}.
- Worktree ${WT}, branch ${BR}, based on fix/review-2026-10 at ${A.base}.
- The repository's CLAUDE.md rules apply (you have them). Its "../assurance-kit" is ${KIT}; the process gates are in ${KIT}/process/ (adversarial-review.md, friction-review.md, scenario-catalog.md).
- The rulings log is ${WT}/docs/owner-rulings-2026-10-09.md. Its §13 amends earlier sections, and where the log and a design disagree, the log wins.
- Design of record: ${WT}/${A.design}${A.otherDesigns ? `. Also: ${A.otherDesigns}` : ''}.
- What the slice holds:
${A.scope}
- Its catalog rows: ${A.rows}`

const PROBE = (who, writesWT) => `${writesWT ? `You make your fixes in ${WT}.` : `You write no file in ${WT} and commit nothing.`} To run a probe or a mutation, use your own detached worktree:
  git -C ${REPO} worktree add --detach ${S}/probe/${A.slug}-${who} ${BR}
(re-point it with \`git -C ${S}/probe/${A.slug}-${who} checkout --detach <sha>\` when you need another commit), and remove it when you are done:
  git -C ${REPO} worktree remove --force ${S}/probe/${A.slug}-${who}`

const CONFIRM_SCHEMA = { type: 'object', properties: {
  web_available: { type: 'boolean' },
  facts: { type: 'array', items: { type: 'object', properties: {
    name: { type: 'string' }, figure: { type: 'string' }, bounded: { type: 'boolean' }, counted_as: { type: 'string' },
    sources: { type: 'array', items: { type: 'object', properties: { url: { type: 'string' }, title: { type: 'string' }, updated: { type: 'string' }, quote: { type: 'string' } }, required: ['url', 'quote'] } },
    consequence: { type: 'string' } }, required: ['name', 'figure', 'bounded', 'consequence'] } },
  design_commit: { type: 'string' }, notes: { type: 'string' } }, required: ['web_available', 'facts'] }

const BUILD_SCHEMA = { type: 'object', properties: {
  commits: { type: 'array', items: { type: 'object', properties: { sha: { type: 'string' }, subject: { type: 'string' } }, required: ['sha', 'subject'] } },
  rows: { type: 'array', items: { type: 'object', properties: { id: { type: 'string' }, state: { type: 'string', enum: ['green', 'skip-encoder', 'absent'] }, test: { type: 'string' }, note: { type: 'string' } }, required: ['id', 'state'] } },
  prove_fail: { type: 'array', items: { type: 'object', properties: { test: { type: 'string' }, mutation: { type: 'string' }, red: { type: 'string' } }, required: ['test', 'mutation', 'red'] } },
  open: { type: 'array', items: { type: 'string' } }, deviations: { type: 'array', items: { type: 'string' } }, blockers: { type: 'array', items: { type: 'string' } },
  suite: { type: 'string' }, vet: { type: 'string' }, gofmt: { type: 'string' }, race: { type: 'string' }, head: { type: 'string' } },
  required: ['commits', 'rows', 'suite', 'head'] }

const CONFORM_SCHEMA = { type: 'object', properties: {
  verdict: { type: 'string', enum: ['CONFORMS', 'GAPS_FIXED', 'BLOCKED'] },
  gaps: { type: 'array', items: { type: 'object', properties: { item: { type: 'string' }, finding: { type: 'string' }, fixed: { type: 'boolean' }, commit: { type: 'string' } }, required: ['item', 'finding', 'fixed'] } },
  weakening: { type: 'array', items: { type: 'string' } }, prove_fail_rechecked: { type: 'array', items: { type: 'string' } },
  suite: { type: 'string' }, head: { type: 'string' } }, required: ['verdict', 'gaps', 'weakening', 'suite', 'head'] }

const FINDINGS_SCHEMA = { type: 'object', properties: {
  coverage: { type: 'string' },
  findings: { type: 'array', items: { type: 'object', properties: {
    title: { type: 'string' }, claim: { type: 'string' }, where: { type: 'string' }, sequence: { type: 'string' }, impact: { type: 'string' },
    severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] }, reproduced: { type: 'boolean' } },
    required: ['title', 'claim', 'where', 'sequence', 'severity'] } } }, required: ['coverage', 'findings'] }

const VERDICTS_SCHEMA = { type: 'object', properties: {
  verdicts: { type: 'array', items: { type: 'object', properties: {
    key: { type: 'string' }, verdict: { type: 'string', enum: ['CONFIRMED', 'REFUTED', 'DUPLICATE', 'NON_GOAL'] },
    severity: { type: 'string' }, evidence: { type: 'string' }, duplicate_of: { type: 'string' } }, required: ['key', 'verdict', 'evidence'] } } },
  required: ['verdicts'] }

const FIX_SCHEMA = { type: 'object', properties: {
  fixed: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, commit: { type: 'string' }, tombstone: { type: 'string' }, red: { type: 'string' } }, required: ['key', 'commit', 'tombstone', 'red'] } },
  escalations: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, why: { type: 'string' }, encoder: { type: 'string' } }, required: ['key', 'why'] } },
  report: { type: 'string' }, report_commit: { type: 'string' }, suite: { type: 'string' }, vet: { type: 'string' }, head: { type: 'string' } },
  required: ['fixed', 'escalations', 'report', 'suite', 'head'] }

const REVERIFY_SCHEMA = { type: 'object', properties: {
  results: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, pass: { type: 'boolean' }, note: { type: 'string' } }, required: ['key', 'pass', 'note'] } },
  new_findings: { type: 'array', items: { type: 'object', properties: { title: { type: 'string' }, claim: { type: 'string' }, where: { type: 'string' }, severity: { type: 'string' } }, required: ['title', 'claim', 'where'] } },
  report_accurate: { type: 'boolean' }, report_notes: { type: 'string' } }, required: ['results', 'new_findings', 'report_accurate'] }

// ---- Confirm ----
let confirm = null
if (A.confirm) {
  phase('Confirm')
  confirm = await run(`You confirm facts that slice ${A.id}'s design requires before any code is written.

${CONTEXT}

WHAT TO CONFIRM
${A.confirm}

HOW
- Load the web tools with ToolSearch (query "select:WebSearch,WebFetch"). The source is Microsoft's own documentation (learn.microsoft.com, support.microsoft.com). Anything else is supporting evidence only.
- Send only generic questions about the product's limits. Never put anything from this repository, a person's name, a tenant or a mail item into a query or URL.
- For each fact: quote the sentence(s) that state the limit, with the URL, the page title, its last-updated date if shown, and the retrieval date ${A.today}. Say how the figure is counted. Say whether the documentation bounds the exact case the design asks about. If it does not, say so plainly. Never guess a figure.
- Then record the result in the design where it says the confirmation goes, marked "[${A.id} confirmation, ${A.today}]", with the sources, and set the figures the design derives from it. Where nothing bounds a case, write the design's own stated fallback for that case (the safe interim it names) instead of a figure. Change nothing else, and commit that one edit.
- If the web tools are unavailable, edit nothing and return web_available=false.

${SHELL}

${SAFETY}${note('confirm')}`, { label: 'confirm', phase: 'Confirm', schema: CONFIRM_SCHEMA })
  if (!confirm || confirm.web_available === false) {
    log('confirm: web tools unavailable or the agent failed; stopping before Build')
    return { id: A.id, stopped: 'confirm', confirm }
  }
  log(`confirm: ${confirm.facts.map(f => `${f.name}: ${f.figure}${f.bounded ? '' : ' (not bounded)'}`).join('; ')}`)
}

// ---- Build ----
phase('Build')
const confirmText = confirm ? `
CONFIRMED BEFORE CODE (design commit ${confirm.design_commit || 'see the design'}). Use these figures, and the design's stated fallback wherever a fact is not bounded:
${JSON.stringify(confirm.facts, null, 1)}
` : ''
const build = await run(`You build slice ${A.id}.

${CONTEXT}
${confirmText}
READ FIRST: the slice's row in the design's build-slice table and every section it cites; the catalog rows the slice lands (their text, Covers cells and mutation columns); docs/scenario-catalog.md (how R, S and MA rows are written); ${WT}/assureblock/covers_map_test.go (how rows and \`// covers:\` markers are checked; never edit it); internal/assure/ (the helpers refusal tests must use); and today's code that the slice changes.

ORDER OF WORK
1. Catalog-first commit. Add the slice's rows to docs/scenario-catalog.md exactly as the design states them, under the allocated ids (rulings §8 and §13.1), with any R or S ids the rows need that the slice lands. For every new U or S row add its covering test as a skip encoder: a Test function with the \`// covers:\` marker whose body is \`t.Skip("<row>: <what is missing>; lands in ${A.id}")\`. Run the unfiltered suite through inhome; it must pass. Commit ("catalog: ${A.id} rows, catalog-first").
2. Then build the slice in small commits. For each piece: turn its encoder (or write its case) into the real test; run it against the code as it stands and see it fail for the right reason (keep the failing line); implement; see it pass. Then prove-fail: apply the row's mutation (its last column) or the smallest mutation of your fix, watch the test go red, and revert. Record "prove-fail: <mutation> -> <test> red (<line>); prove-pass: green" in the commit message.
3. A case the design keeps open (it names a skip encoder, a later slice, or an owner question's safe interim) stays a skip encoder citing its row, with the reason naming what closes it.
4. Finish with the unfiltered suite through inhome (pass), ${S}/gobin/vet3 ${WT} (clean), ${S}/gobin/gofmt -l ${WT} (empty), and \`go test -race\` on the packages you touched (through inhome).

RULES
- Match the surrounding code: naming, comment density, error style, file layout.
- Keep today's behaviour wherever the design says so, and keep every existing test's assertions. If an existing test must change because the design changes what it asserts, stop: that is the owner's call (rulings §0, rule 2). Report it as a blocker and leave the old test as it is.
- Where the design is ambiguous, take the narrowest reading that satisfies its rows, say so in the commit, and list it under deviations. Where it is wrong or cannot be built as written, leave that part a skip encoder citing its row and list it under blockers. Do not invent design.
- Do not build what the slice table assigns to other slices.

${SHELL}

${GOB}

${SAFETY}

Return the commits (sha, subject), every row's state (green / skip-encoder / absent) with its test, the prove-fail records, what stays open, deviations, blockers, the suite, vet, gofmt and race results, and the final HEAD sha.${note('build')}`, { label: 'build', phase: 'Build', schema: BUILD_SCHEMA })
if (!build) return { id: A.id, stopped: 'build', confirm }
log(`build: ${build.commits.length} commits, suite ${build.suite}, head ${build.head}`)

// ---- Conform ----
phase('Conform')
const conform = await run(`You check slice ${A.id} as built against its design, as an independent reviewer who did not write it. Default to a gap whenever the code or a test does not plainly do what the design and the row say.

${CONTEXT}

THE BUILD REPORTED
${JSON.stringify({ rows: build.rows, open: build.open, deviations: build.deviations, blockers: build.blockers, prove_fail: build.prove_fail }, null, 1)}

CHECK
1. Every content item of the slice (its build-table row, as amended by the rulings log) is in the code, and nothing assigned to another slice is.
2. Every catalog row the slice lands is in docs/scenario-catalog.md as the design states it, and every case the row names is asserted by its covering test, not just mentioned. A skip encoder is allowed only where the design keeps the case open.
3. Re-run at least three prove-fail mutations yourself, choosing the riskiest, and check that each named test goes red for the right reason.
4. Gate weakening: read \`git -C ${WT} diff ${A.base}..HEAD\` for any deleted or changed \`// covers:\` marker, any loosened or removed assertion in an existing test, any suppression, and any edit to assureblock/, a baseline or the rulings log.
5. The unfiltered suite passes through inhome, and vet3 is clean.

FIX each plain conformance gap you find (a missing case, row text that drifted from the design, an assertion that does not test what its row says) with the same discipline: see the test fail for the right reason, fix, see it pass, and record the prove-fail in the commit. Revert a weakening rather than working around it. Report anything that needs a design or owner decision without fixing it.

${PROBE('conform', true)}

${SHELL}

${GOB}

${SAFETY}${note('conform')}`, { label: 'conform', phase: 'Conform', schema: CONFORM_SCHEMA })
if (!conform) return { id: A.id, stopped: 'conform', confirm, build }
log(`conform: ${conform.verdict}, ${conform.gaps.length} gaps, ${conform.weakening.length} weakening notes`)
if (conform.verdict === 'BLOCKED') {
  return { id: A.id, stopped: 'conform-blocked', head: conform.head, build: { commits: build.commits, rows: build.rows, blockers: build.blockers }, conform }
}

if (A.stopAfter === 'conform') {
  // The slice's owed gate is not an adversarial pass (for example a friction review): stop here.
  return { id: A.id, branch: BR, base: A.base, head: conform.head || build.head, build: { commits: build.commits, rows: build.rows, open: build.open, deviations: build.deviations, blockers: build.blockers, suite: build.suite, vet: build.vet, race: build.race }, conform: { verdict: conform.verdict, gaps: conform.gaps, weakening: conform.weakening } }
}

// ---- Attack ----
phase('Attack')
const head = conform.head || build.head
const finds = await parallel(A.lenses.map(L => () => run(`You are an adversarial finder for slice ${A.id}. Your lens: ${L.name}. Your job is to BREAK it, not to review it. A finder is rewarded for imagination, but every claim must be concrete: file:line, the exact request or input, the exact sequence of steps.

${CONTEXT}

The gate: ${A.gate}. The procedure is ${KIT}/process/adversarial-review.md (section 1). The design's security section (attacker personas, trust boundaries, acknowledged limits) and its non-goals are the threat model. A finding that faults a conceded non-goal is invalid, so read those first.

The code as built is at ${WT} (HEAD ${head}); the change is \`git -C ${WT} diff ${A.base}..HEAD\`. Attack the change, and what it newly exposes in the code around it.

YOUR LENS
${L.focus}

${PROBE(L.key, false)}

${SHELL}

${GOB}

Never print or store a token or secret. Return your coverage (what you read and tried) and your findings. Report only what you can tie to the code, and say which ones a probe already reproduced.${note('attack')}`, { label: `attack ${L.key}`, phase: 'Attack', schema: FINDINGS_SCHEMA })))

const all = []
finds.forEach((r, i) => {
  if (!r) return
  r.findings.forEach((f, j) => all.push({ key: `${A.lenses[i].key}${j + 1}`, lens: A.lenses[i].name, ...f }))
})
const missing = A.lenses.filter((L, i) => !finds[i]).map(L => L.key)
log(`attack: ${all.length} findings from ${finds.filter(Boolean).length} of ${A.lenses.length} finders${missing.length ? `; no result from ${missing.join(', ')}` : ''}`)

// ---- Skeptic ----
let verdicts = []
if (all.length) {
  phase('Skeptic')
  const sk = await run(`You are the skeptic of slice ${A.id}'s adversarial pass, separate from every finder. Your default verdict is NOT REAL. For every finding below, either reproduce it against the real code (a test or probe in your own worktree that shows the failure), or show from the source why it cannot happen, and record that evidence. Silent dropping is not allowed.

${CONTEXT}

The code is at ${WT} (HEAD ${head}); the change is \`git -C ${WT} diff ${A.base}..HEAD\`. The procedure is ${KIT}/process/adversarial-review.md (section 2).

VERDICTS
- CONFIRMED: reproduced, or the mechanism shown in the actual source (file:line). Give a severity (critical / high / medium / low) on the design's threat model, and the reproduction.
- REFUTED: the code that prevents it, or the probe that passes, as evidence.
- DUPLICATE: name the key it duplicates (keep the clearest one).
- NON_GOAL: cite the design's non-goal or acknowledged limit that it faults.

FINDINGS
${JSON.stringify(all, null, 1)}

${PROBE('skeptic', false)}

${SHELL}

${GOB}

Never print or store a token or secret. Return one verdict per key.${note('skeptic')}`, { label: 'skeptic', phase: 'Skeptic', schema: VERDICTS_SCHEMA })
  if (!sk) return { id: A.id, stopped: 'skeptic', head, findings: all.map(f => ({ key: f.key, title: f.title, severity: f.severity })) }
  verdicts = sk.verdicts
}
const confirmed = verdicts.filter(v => v.verdict === 'CONFIRMED')
const unjudged = all.filter(f => !verdicts.find(v => v.key === f.key)).map(f => f.key)
log(`skeptic: ${confirmed.length} confirmed of ${all.length}${unjudged.length ? `; no verdict for ${unjudged.join(', ')}` : ''}`)

// ---- Fix and file ----
phase('Fix')
const judged = all.map(f => ({ ...f, verdict: verdicts.find(v => v.key === f.key) || { verdict: 'UNJUDGED', evidence: 'the skeptic returned no verdict' } }))
const fix = await run(`You fix the confirmed findings of slice ${A.id}'s adversarial pass and file its report. You did not find these issues.

${CONTEXT}

The procedure is ${KIT}/process/adversarial-review.md (sections 3 and 4). The code is at ${WT} (HEAD ${head}), based on ${A.base}.

EVERY FINDING, WITH THE SKEPTIC'S VERDICT
${JSON.stringify(judged, null, 1)}

FOR EACH CONFIRMED FINDING (an UNJUDGED one is treated as confirmed if you can reproduce it, and refuted with your evidence if you cannot)
- Write its regression test first, the exploit's tombstone, with the \`// covers:\` marker of the catalog row that states the guarantee (add a case to that row's test). If no row states it, do not add a row: list it under escalations, with a skip encoder citing the closest invariant.
- Show the tombstone RED against the pre-fix code for the right reason, then fix, then GREEN. Record the red line in the commit message. One commit per finding, or per tightly coupled group.
- If the fix would narrow a guarantee, contradict the design, or needs an owner decision, do not make it: leave a skip encoder citing the invariant it breaks, and list it under escalations.

THEN FILE THE REPORT at ${WT}/docs/review-${A.slug}-adversarial.md, per section 4 of the procedure: a header with "N found / C confirmed / R refuted" (count duplicates and non-goals separately), the scope (slice, base and head commits, the three lenses), then the table | ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test | with every finding, refuted ones included with their evidence. Commit it.

Finish with the unfiltered suite through inhome, vet3 and gofmt -l.

${PROBE('fix', true)}

${SHELL}

${GOB}

${SAFETY}${note('fix')}`, { label: 'fix', phase: 'Fix', schema: FIX_SCHEMA })
if (!fix) return { id: A.id, stopped: 'fix', head, findings: judged.map(f => ({ key: f.key, title: f.title, verdict: f.verdict.verdict })) }
log(`fix: ${fix.fixed.length} fixed, ${fix.escalations.length} escalated, suite ${fix.suite}`)

// ---- Re-verify ----
let rv = null
let fix2 = null
if (fix.fixed.length || fix.escalations.length) {
  phase('Re-verify')
  rv = await run(`Fresh-eyes check of the fixes made in slice ${A.id}'s adversarial pass. You did not write them. Default to FAIL whenever a fix does not plainly close its finding.

${CONTEXT}

The code is at ${WT} (HEAD ${fix.head}). The report is ${WT}/docs/review-${A.slug}-adversarial.md.

FIXES: ${JSON.stringify(fix.fixed, null, 1)}
ESCALATIONS: ${JSON.stringify(fix.escalations, null, 1)}
THE FINDINGS THEY ANSWER: ${JSON.stringify(judged.filter(f => fix.fixed.find(x => x.key === f.key) || fix.escalations.find(x => x.key === f.key)), null, 1)}

FOR EACH FIX: in your own worktree, revert only the fix (keep the tombstone) and confirm the tombstone goes red for the right reason; then read the fix for a problem it introduces (a new bypass, a healthy case now refused, a leak, a race, a changed behaviour the design keeps).
FOR EACH ESCALATION: check that the skip encoder cites the right invariant and that the escalation is real (the fix truly needs the design's author or the owner).
THE REPORT: check its counts and that every row matches the code and the commits.

${PROBE('reverify', false)}

${SHELL}

${GOB}${note('reverify')}`, { label: 'reverify', phase: 'Re-verify', schema: REVERIFY_SCHEMA })
  if (rv && (rv.results.some(r => !r.pass) || rv.new_findings.length || rv.report_accurate === false)) {
    fix2 = await run(`Fix what the fresh-eyes check of slice ${A.id}'s adversarial fixes found. You did not write the fixes or the check.

${CONTEXT}

The code is at ${WT} (HEAD ${fix.head}). The report is ${WT}/docs/review-${A.slug}-adversarial.md.

THE CHECK: ${JSON.stringify(rv, null, 1)}
THE FIXES IT CHECKED: ${JSON.stringify(fix.fixed, null, 1)}

For each failed result or new finding: reproduce it first; if real, fix it with the same discipline (tombstone red for the right reason, then the fix, then green; record it in the commit); if not real, say why. An issue that needs an owner decision becomes an escalation with a skip encoder. Then bring the report up to date (a Re-verification section listing each item and its outcome) and commit. Finish with the unfiltered suite through inhome, vet3 and gofmt -l.

${PROBE('fix2', true)}

${SHELL}

${GOB}

${SAFETY}${note('fix2')}`, { label: 'fix 2', phase: 'Re-verify', schema: FIX_SCHEMA })
  }
}

const count = v => verdicts.filter(x => x.verdict === v).length
return {
  id: A.id, branch: BR, base: A.base,
  head: (fix2 && fix2.head) || fix.head || head,
  confirm: confirm ? confirm.facts.map(f => ({ name: f.name, figure: f.figure, bounded: f.bounded, consequence: f.consequence })) : null,
  build: { commits: build.commits, rows: build.rows, open: build.open, deviations: build.deviations, blockers: build.blockers, suite: build.suite, vet: build.vet, race: build.race },
  conform: { verdict: conform.verdict, gaps: conform.gaps, weakening: conform.weakening },
  adversarial: {
    found: all.length, confirmed: confirmed.length, refuted: count('REFUTED'), duplicate: count('DUPLICATE'), non_goal: count('NON_GOAL'), unjudged,
    confirmed_list: confirmed.map(v => ({ key: v.key, severity: v.severity, title: (all.find(f => f.key === v.key) || {}).title })),
    report: fix.report, escalations: fix.escalations, suite: fix.suite,
  },
  reverify: rv ? { failed: rv.results.filter(r => !r.pass), new_findings: rv.new_findings, report_accurate: rv.report_accurate } : null,
  fix2: fix2 ? { fixed: fix2.fixed, escalations: fix2.escalations, suite: fix2.suite, head: fix2.head } : null,
}
