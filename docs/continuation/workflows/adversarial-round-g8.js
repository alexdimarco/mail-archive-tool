export const meta = {
  name: 'adversarial-round',
  description: 'One adversarial round over an existing change set: three finders with distinct lenses, a refutation-default skeptic, fixes with tombstones, the round filed in its report, and a fresh-eyes re-verify',
  phases: [
    { title: 'Attack', detail: 'three finders with distinct lenses, each told to break it' },
    { title: 'Skeptic', detail: 'refutation-default verdict on every finding' },
    { title: 'Fix', detail: 'confirmed findings and known items fixed with tombstones; the round filed' },
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

const SHELL = `PROMPT-FREE SHELL. READ THIS FIRST. The user works with Claude Code's read block on. Any Bash command its parser cannot fully analyse, or that names a path outside /Users/alex.dimarco/repositories and ${S}, stops all work to ask the user, who has asked many times for zero interruptions. Earlier today agents caused six interruptions in two minutes with exactly these shapes: a multi-line shell script with variables and a redirect into a $(...) file name, \`python3 -c\`, and \`/usr/bin/time\` and \`/usr/bin/python3\` called by absolute path. So every Bash call is ONE simple command with literal arguments:
- NEVER: $(...), backticks, $VAR or \${VAR}; ;, &&, ||, & or a line break; heredocs (<<) or here-strings; code on stdin; \`python3 -c\`, \`sh -c\`, \`bash -c\`, \`node -e\`; \`cd\`; an environment assignment in front of a command (\`FOO=bar cmd\`); shell loops or if; an unquoted glob, (, ), ! or ~.
- NEVER an absolute path outside /Users/alex.dimarco/repositories and ${S}, in any argument or as the command itself: no /usr/bin/..., /bin/..., /opt/..., ~/..., /var/folders/..., /Applications/.... Call system tools by bare name (python3, git, grep, find, ls, wc, head, tail, sort, diff, cmp, date).
- Anything that needs more than one command, a variable, a loop, a delay, a background process, a timing, an environment variable for a child process, or output written to a computed file name: write a Python script to a file under ${S}/${A.slug}/ with the Write tool and run it as \`python3 -I <script> [literal args]\`. Inside it, use subprocess with an argument list (never shell=True), time.monotonic() for timing, env= for a child's environment, and open() for output files. The script, too, reads and writes only inside /Users/alex.dimarco/repositories and ${S}.
- Unsure whether a command is simple enough? Then it is not: put it in a script.
- Keep command output small (\`| head -N\`, \`| tail -N\`, \`-n\` limits). If a tool result says its output was saved to a file under ~/.claude, never read that file: re-run the command with a tighter filter instead.
- Read, search and edit with the Read, Edit and Write tools. There is no Glob or Grep tool here: search with one plain \`grep -rn ...\` or \`find ...\` per call, single-quoting every pattern.
- Never read ~/.claude, ~/Library, /var/folders (Go's test temp dirs), /opt/homebrew, ~/go or another Go install. Go's sources are in ${S}/go126/go/src (standard library) and ${S}/gopath/pkg/mod (modules).
- If a read is refused, do not retry it or look for another way to the same file: work from what is in the tree.`

const GOB = `GO WRAPPERS (Go 1.26.9 in the scratchpad; never use another go):
- ${S}/gobin/go -C <module dir> <go args>
- ${S}/gobin/gofmt -l <dir>
- ${S}/gobin/vet3 <module dir>   (go vet ./... for darwin, linux and windows)
- ${S}/gobin/gofor <darwin|linux|windows> <go args>   (any other cross-OS go command, e.g. \`gofor windows -C <dir> test -c -o <scratch file> ./internal/schedule\`)
- ${S}/gobin/inhome <dir under ${S}> <command> [args]   (HOME, XDG_CONFIG_HOME, APPDATA and LOCALAPPDATA pointed at that dir)
Run every test run and every built CLI binary through inhome with your own dir, so nothing reads or writes the real user's settings, sign-ins or schedules, e.g.
  ${S}/gobin/inhome ${HOMEDIR} ${S}/gobin/go -C ${WT} test ./...
(A Python driver that runs the CLI sets HOME and the rest in the child's env= itself, pointing inside ${S}.)
Other suites run on this machine at the same time. A timing-sensitive test that fails only under load: re-run it alone (\`-run '^TestName$' -count=3\`) before concluding anything.`

const SAFETY = `SAFETY:
- Commit locally on ${BR} in ${WT} only. Never push. Never touch another branch or worktree, and never run git reset --hard, git clean, git stash, git rebase, or git checkout over files you did not change.
- Never edit assureblock/, a baseline, or docs/owner-rulings-2026-10-09.md. Never delete a covers marker, add a suppression, or loosen an existing assertion. A red gate means the code is wrong; if you believe a gate or a design text is wrong, stop that part and report it.
- No real OS scheduler, crontab, launchd, Task Scheduler or credential-store writes, and nothing under ~/Library: schedules are exercised only through the code's fakes and sandboxes. No test touches the network.
- Never print or store a token or secret.
- Commit messages: write the message to a file under ${S}/msg/ with the Write tool, then run \`git -C ${WT} commit -F <file>\`. Every message ends with these two lines:
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018J6ooKQ18JxYSDhXtCmaDc`

const CONTEXT = `CONTEXT
- ${A.title}.
- Worktree ${WT}, branch ${BR}, HEAD ${A.head}. The change set under attack: ${A.changeSet}
- The repository's CLAUDE.md rules apply (you have them). Its "../assurance-kit" is ${KIT}; the procedure is ${KIT}/process/adversarial-review.md.
- The rulings log is ${WT}/docs/owner-rulings-2026-10-09.md; the fix branches' latest rulings are in §13.3 of the copy on fix/review-2026-10, quoted where they matter below. Where the log and a design disagree, the log wins.
- The report for this pass is ${WT}/${A.report}. ${A.reportNote}
- Threat model and catalog: ${A.threat}
- Tools: ${A.tools}`

const PROBE = (who, writesWT) => `${writesWT ? `You make your fixes in ${WT}.` : `You write no file in ${WT} and commit nothing.`} To run a probe or a mutation, use your own detached worktree and your own output folders under ${S}/${A.slug}/${who}/:
  git -C ${REPO} worktree add --detach ${S}/probe/${A.slug}-${who} ${BR}
(re-point it with \`git -C ${S}/probe/${A.slug}-${who} checkout --detach <sha>\` when you need another commit; build binaries from it into ${S}/${A.slug}/${who}/bin/), and remove it when you are done:
  git -C ${REPO} worktree remove --force ${S}/probe/${A.slug}-${who}`

const FINDINGS_SCHEMA = { type: 'object', properties: {
  coverage: { type: 'string' },
  findings: { type: 'array', items: { type: 'object', properties: {
    title: { type: 'string' }, claim: { type: 'string' }, where: { type: 'string' }, sequence: { type: 'string' }, impact: { type: 'string' },
    severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low'] }, reproduced: { type: 'boolean' } },
    required: ['title', 'claim', 'where', 'sequence', 'severity'] } },
  held: { type: 'array', items: { type: 'string' } } }, required: ['coverage', 'findings'] }

const VERDICTS_SCHEMA = { type: 'object', properties: {
  verdicts: { type: 'array', items: { type: 'object', properties: {
    key: { type: 'string' }, verdict: { type: 'string', enum: ['CONFIRMED', 'REFUTED', 'DUPLICATE', 'NON_GOAL'] },
    severity: { type: 'string' }, evidence: { type: 'string' }, duplicate_of: { type: 'string' } }, required: ['key', 'verdict', 'evidence'] } } },
  required: ['verdicts'] }

const FIX_SCHEMA = { type: 'object', properties: {
  fixed: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, commit: { type: 'string' }, tombstone: { type: 'string' }, red: { type: 'string' } }, required: ['key', 'commit', 'tombstone', 'red'] } },
  known: { type: 'array', items: { type: 'object', properties: { item: { type: 'string' }, outcome: { type: 'string' }, commit: { type: 'string' } }, required: ['item', 'outcome'] } },
  escalations: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, why: { type: 'string' }, encoder: { type: 'string' } }, required: ['key', 'why'] } },
  verdict_line: { type: 'string' }, report_commit: { type: 'string' }, suite: { type: 'string' }, vet: { type: 'string' }, head: { type: 'string' } },
  required: ['fixed', 'escalations', 'verdict_line', 'suite', 'head'] }

const REVERIFY_SCHEMA = { type: 'object', properties: {
  results: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, pass: { type: 'boolean' }, note: { type: 'string' } }, required: ['key', 'pass', 'note'] } },
  new_findings: { type: 'array', items: { type: 'object', properties: { title: { type: 'string' }, claim: { type: 'string' }, where: { type: 'string' }, severity: { type: 'string' } }, required: ['title', 'claim', 'where'] } },
  report_accurate: { type: 'boolean' }, report_notes: { type: 'string' } }, required: ['results', 'new_findings', 'report_accurate'] }

phase('Attack')
const finds = await parallel(A.lenses.map(L => () => run(`${SHELL}

You are an adversarial finder in ${A.roundName}. Your lens: ${L.name}. Your job is to BREAK it, not to review it. A finder is rewarded for imagination, but every claim must be concrete: file:line, the exact input, the exact sequence of steps, and what comes out.

${CONTEXT}

Attack the change set, and what it newly exposes in the code around it. A finding that faults a conceded non-goal is invalid, so read the threat model's non-goals first. The known items listed below are already being fixed; do not re-report them.

KNOWN ITEMS (not yours to report): ${A.known}

YOUR LENS
${L.focus}

${PROBE(L.key, false)}

${GOB}

Never print or store a token or secret. Return your coverage (what you read, built and ran), your findings, and what you tried that held. Report only what you can tie to the code, and say which ones a probe already reproduced.${note('attack')}`, { label: `attack ${L.key}`, phase: 'Attack', schema: FINDINGS_SCHEMA })))

const all = []
const held = []
finds.forEach((r, i) => {
  if (!r) return
  r.findings.forEach((f, j) => all.push({ key: `${A.prefix}${A.lenses[i].key}${j + 1}`, lens: A.lenses[i].name, ...f }))
  ;(r.held || []).forEach(h => held.push(`${A.lenses[i].key}: ${h}`))
})
const missing = A.lenses.filter((L, i) => !finds[i]).map(L => L.key)
log(`attack: ${all.length} findings from ${finds.filter(Boolean).length} of ${A.lenses.length} finders${missing.length ? `; no result from ${missing.join(', ')}` : ''}`)

let verdicts = []
if (all.length) {
  phase('Skeptic')
  const sk = await run(`${SHELL}

You are the skeptic of ${A.roundName}, separate from every finder. Your default verdict is NOT REAL. For every finding below, either reproduce it against the real code at ${A.head} (a test or a probe in your own worktree that shows the failure), or show from the source why it cannot happen, and record that evidence. Silent dropping is not allowed.

${CONTEXT}

VERDICTS
- CONFIRMED: reproduced, or the mechanism shown in the actual source (file:line). Give a severity (critical / high / medium / low) on the threat model, and the reproduction.
- REFUTED: the code that prevents it, or the probe that passes, as evidence.
- DUPLICATE: name the key it duplicates (keep the clearest one), including a duplicate of a known item.
- NON_GOAL: cite the non-goal or acknowledged limit it faults.

KNOWN ITEMS: ${A.known}

FINDINGS
${JSON.stringify(all, null, 1)}

${PROBE('skeptic', false)}

${GOB}

Never print or store a token or secret. Return one verdict per key.${note('skeptic')}`, { label: 'skeptic', phase: 'Skeptic', schema: VERDICTS_SCHEMA })
  if (!sk) return { round: A.roundName, stopped: 'skeptic', findings: all.map(f => ({ key: f.key, title: f.title, severity: f.severity })) }
  verdicts = sk.verdicts
}
const confirmed = verdicts.filter(v => v.verdict === 'CONFIRMED')
const unjudged = all.filter(f => !verdicts.find(v => v.key === f.key)).map(f => f.key)
log(`skeptic: ${confirmed.length} confirmed of ${all.length}${unjudged.length ? `; no verdict for ${unjudged.join(', ')}` : ''}`)

phase('Fix')
const judged = all.map(f => ({ ...f, verdict: verdicts.find(v => v.key === f.key) || { verdict: 'UNJUDGED', evidence: 'the skeptic returned no verdict' } }))
const fix = await run(`${SHELL}

You fix the confirmed findings of ${A.roundName} and its known items, and file the round in its report. You did not find these issues.

${CONTEXT}

EVERY FINDING, WITH THE SKEPTIC'S VERDICT
${JSON.stringify(judged, null, 1)}

WHAT THE FINDERS TRIED THAT HELD
${JSON.stringify(held, null, 1)}

KNOWN ITEMS THIS ROUND ALSO CLOSES
${A.known}

FOR EACH CONFIRMED FINDING AND EACH KNOWN ITEM (an UNJUDGED finding is treated as confirmed if you can reproduce it, and refuted with your evidence if you cannot)
- Write its regression test first, the tombstone, with the \`// covers:\` marker of the catalog row that states the guarantee (add a case to that row's test). If no row states it, do not add a row: list it under escalations with a skip encoder citing the closest invariant. A known item that is a test gap gets its test, shown red against a mutation of the code it guards.
- Show the tombstone RED against the code before your fix, for the right reason, then fix, then GREEN. Record the red line in the commit message. One commit per finding, or per tightly coupled group.
- If the fix would narrow a guarantee, contradict the design or a ruling, or needs an owner decision, do not make it: leave a skip encoder citing the invariant it breaks, and list it under escalations.
- Re-run ${A.recheck}.

THEN FILE THE ROUND in ${WT}/${A.report}: a "${A.roundName}" section with its scope (the change set, base and head, the three lenses), its count line "N found / C confirmed / R refuted" (duplicates and non-goals counted separately), the table | ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test | with every finding (refuted ones with their evidence) and every known item, what held, and what this round did not cover. Give the report a header with the counts and a verdict line that is true: FINDINGS OPEN while anything confirmed is unfixed or unverified, naming it. Commit it.

Finish with the unfiltered suite through inhome, vet3 and gofmt -l.

${PROBE('fix', true)}

${GOB}

${SAFETY}${note('fix')}`, { label: 'fix', phase: 'Fix', schema: FIX_SCHEMA })
if (!fix) return { round: A.roundName, stopped: 'fix', findings: judged.map(f => ({ key: f.key, title: f.title, verdict: f.verdict.verdict })) }
log(`fix: ${fix.fixed.length} fixed, ${(fix.known || []).length} known items, ${fix.escalations.length} escalated, suite ${fix.suite}`)

let rv = null
let fix2 = null
if (fix.fixed.length || fix.escalations.length || (fix.known || []).length) {
  phase('Re-verify')
  rv = await run(`${SHELL}

Fresh-eyes check of the fixes made in ${A.roundName}. You did not write them. Default to FAIL whenever a fix does not plainly close its finding.

${CONTEXT}

The code is at ${WT} (HEAD ${fix.head}).

FIXES: ${JSON.stringify(fix.fixed, null, 1)}
KNOWN ITEMS: ${JSON.stringify(fix.known || [], null, 1)}
ESCALATIONS: ${JSON.stringify(fix.escalations, null, 1)}
THE FINDINGS THEY ANSWER: ${JSON.stringify(judged.filter(f => fix.fixed.find(x => x.key === f.key) || fix.escalations.find(x => x.key === f.key)), null, 1)}

FOR EACH FIX AND KNOWN ITEM: in your own worktree, revert only the fix (keep the tombstone) and confirm the tombstone goes red for the right reason; then read the fix for a problem it introduces (a new bypass, a healthy case now refused, a status line that lies, a removal that deletes the wrong thing, a race). Re-run ${A.recheck} at ${fix.head}.
FOR EACH ESCALATION: check that the skip encoder cites the right invariant and that the escalation is real.
THE REPORT: check its counts, its table against the code and the commits, and that the header and verdict line are true.

${PROBE('reverify', false)}

${GOB}${note('reverify')}`, { label: 'reverify', phase: 'Re-verify', schema: REVERIFY_SCHEMA })
  if (rv && (rv.results.some(r => !r.pass) || rv.new_findings.length || rv.report_accurate === false)) {
    fix2 = await run(`${SHELL}

Fix what the fresh-eyes check of ${A.roundName}'s fixes found. You did not write the fixes or the check.

${CONTEXT}

The code is at ${WT} (HEAD ${fix.head}).

THE CHECK: ${JSON.stringify(rv, null, 1)}
THE FIXES IT CHECKED: ${JSON.stringify(fix.fixed, null, 1)}

For each failed result or new finding: reproduce it first; if real, fix it with the same discipline (tombstone red for the right reason, then the fix, then green; record it in the commit); if not real, say why. An issue that needs an owner decision becomes an escalation with a skip encoder. Then add a "Re-verification" part to the round's section of the report, listing each item and its outcome, keep the header and verdict line true, and commit. Finish with the unfiltered suite through inhome, vet3 and gofmt -l.

${PROBE('fix2', true)}

${GOB}

${SAFETY}${note('fix2')}`, { label: 'fix 2', phase: 'Re-verify', schema: FIX_SCHEMA })
  }
}

const count = v => verdicts.filter(x => x.verdict === v).length
return {
  round: A.roundName, branch: BR, base_head: A.head,
  head: (fix2 && fix2.head) || fix.head,
  found: all.length, confirmed: confirmed.length, refuted: count('REFUTED'), duplicate: count('DUPLICATE'), non_goal: count('NON_GOAL'), unjudged,
  confirmed_list: confirmed.map(v => ({ key: v.key, severity: v.severity, title: (all.find(f => f.key === v.key) || {}).title })),
  fixed: fix.fixed.map(x => ({ key: x.key, commit: x.commit, tombstone: x.tombstone })), known: fix.known || [], escalations: fix.escalations,
  verdict_line: (fix2 && fix2.verdict_line) || fix.verdict_line, suite: (fix2 && fix2.suite) || fix.suite,
  reverify: rv ? { failed: rv.results.filter(r => !r.pass), new_findings: rv.new_findings, report_accurate: rv.report_accurate } : null,
  fix2: fix2 ? { fixed: fix2.fixed, escalations: fix2.escalations, suite: fix2.suite } : null,
}
