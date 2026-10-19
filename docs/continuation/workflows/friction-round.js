export const meta = {
  name: 'friction-round',
  description: 'Run the friction review a branch owes: walk at least ten actor-by-scenario cells as the real actor, file the report, build the legibility fixes it finds with prove-fail, and re-verify them with fresh eyes',
  phases: [
    { title: 'Walk', detail: 'at least ten (actor, scenario) cells, ugly cells first' },
    { title: 'Fix', detail: 'in-scope friction fixes, prove-fail then prove-pass' },
    { title: 'Re-verify', detail: 'fresh eyes on the fixes and the re-walked cells' },
  ],
}

const A = args
const S = A.scratch
const WT = A.worktree
const REPO = A.repo
const BR = A.branch
const KIT = '/Users/alex.dimarco/repositories/crescendum-tools/assurance-kit'
const W = `${S}/${A.slug}`
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

const SHELL = `PROMPT-FREE SHELL. READ THIS FIRST. The user works with Claude Code's read block on. Any Bash command its parser cannot fully analyse, or that names a path outside /Users/alex.dimarco/repositories and ${S}, stops all work to ask the user, who has asked many times for zero interruptions. In the previous run of this task, agents caused six interruptions in two minutes with exactly these shapes: a multi-line shell script with variables and a redirect into a $(...) file name, \`python3 -c\`, and \`/usr/bin/time\` and \`/usr/bin/python3\` called by absolute path. So every Bash call is ONE simple command with literal arguments:
- NEVER: $(...), backticks, $VAR or \${VAR}; ;, &&, ||, & or a line break; heredocs (<<) or here-strings; code on stdin; \`python3 -c\`, \`sh -c\`, \`bash -c\`, \`node -e\`; \`cd\`; an environment assignment in front of a command (\`FOO=bar cmd\`); shell loops or if; an unquoted glob, (, ), ! or ~.
- NEVER an absolute path outside /Users/alex.dimarco/repositories and ${S}, in any argument or as the command itself: no /usr/bin/..., /bin/..., /opt/..., ~/..., /var/folders/..., /Applications/.... Call system tools by bare name (python3, git, grep, find, ls, wc, head, tail, sort, diff, cmp, date).
- Anything that needs more than one command, a variable, a loop, a delay, a second process running at the same time, a timing, an environment variable for a child process, or output written to a computed file name: write a Python script to a file under ${W}/ with the Write tool and run it as \`python3 -I <script> [literal args]\`. Inside it, use subprocess with an argument list (never shell=True), time.monotonic() for timing, env= for a child's environment (HOME, XDG_CONFIG_HOME, APPDATA and LOCALAPPDATA pointed inside ${W}/), and open() for output files. The script, too, reads and writes only inside /Users/alex.dimarco/repositories and ${S}.
- Unsure whether a command is simple enough? Then it is not: put it in a script.
- Do NOT use the bash friction() logger shown in the kit's friction-review.md section 3: it calls $(date) and would stop for the user. Log each friction finding from your Python driver or in your own notes.
- Read, search and edit with the Read, Edit and Write tools. There is no Glob or Grep tool here: search with one plain \`grep -rn ...\` or \`find ...\` per call, single-quoting every pattern (a trailing \`| head -N\` or \`| tail -N\` is fine).
- Never read ~/.claude, ~/Library, /var/folders (Go's test temp dirs), /opt/homebrew, ~/go or another Go install. Go's sources are in ${S}/go126/go/src and ${S}/gopath/pkg/mod.
- If a read is refused, do not retry it or look for another way to the same file.`

const GOB = `GO WRAPPERS (Go 1.26.9 in the scratchpad; never use another go):
- ${S}/gobin/go -C <module dir> <go args>   (build binaries with -o into ${W}/bin/)
- ${S}/gobin/gofmt -l <dir>
- ${S}/gobin/vet3 <module dir>   (go vet ./... for darwin, linux and windows)
- ${S}/gobin/gofor <darwin|linux|windows> <go args>   (any other cross-OS go command)
- ${S}/gobin/inhome <dir under ${S}> <command> [args]   (HOME, XDG_CONFIG_HOME, APPDATA and LOCALAPPDATA pointed at that dir)
Run every test run and every built binary through inhome (or from a Python driver that sets env= the same way), so nothing reads or writes the real user's settings, sign-ins or schedules, e.g.
  ${S}/gobin/inhome ${W}/home ${S}/gobin/go -C ${WT} test ./...
Other suites run on this machine at the same time. A timing-sensitive test that fails only under load: re-run it alone (\`-run '^TestName$' -count=3\`) before concluding anything.`

const SAFETY = `SAFETY:
- Commit locally on ${BR} in ${WT} only. Never push. Never touch another branch or worktree, and never run git reset --hard, git clean, git stash, git rebase, or git checkout over files you did not change.
- Never edit assureblock/, a baseline, or docs/owner-rulings-2026-10-09.md. Never delete a covers marker, add a suppression, or loosen an existing assertion. A red gate means the code is wrong; if you believe a gate or a design text is wrong, stop that part and report it.
- No real OS scheduler, crontab, launchd, Task Scheduler or credential-store writes, and nothing under ~/Library: drive schedules only through the code's fakes and dry runs. No network.
- Never print or store a token or secret.
- Commit messages: write the message to a file under ${S}/msg/ with the Write tool, then run \`git -C ${WT} commit -F <file>\`. Every message ends with these two lines:
Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018J6ooKQ18JxYSDhXtCmaDc`

const CONTEXT = `CONTEXT
- ${A.title}.
- Worktree ${WT}, branch ${BR}, HEAD ${A.head}; the branch's change set is \`git -C ${WT} log --oneline ${A.base}..HEAD\` (read the commit messages) and \`git -C ${WT} diff ${A.base}..HEAD\`.
- What the branch changes: ${A.scope}
- The repository's CLAUDE.md rules apply (you have them). Its "../assurance-kit" is ${KIT}; the procedure is ${KIT}/process/friction-review.md, and the UX contract is ${WT}/docs/ux-contract.md (X-series: what / why / remediation in every refusal).
- Earlier friction reports in ${WT}/docs/review-*-friction.md show this repository's format; follow it.
- The rulings log is ${WT}/docs/owner-rulings-2026-10-09.md (§13.3 holds g7-Q1 and g7-Q2).
- Your working folder is ${W}/ (binaries in ${W}/bin/, scratch archives and homes under ${W}/). A stopped earlier attempt left scripts in ${S}/g7c/; you may read them for ideas, but write your own under ${W}/.`

const WALK_SCHEMA = { type: 'object', properties: {
  verdict: { type: 'string', enum: ['SHIPPABLE', 'HIGH_FRICTION_NOT_SHIPPABLE'] },
  cells: { type: 'array', items: { type: 'object', properties: {
    cell: { type: 'string' }, functions: { type: 'string', enum: ['yes', 'partial', 'no'] }, friction: { type: 'string' },
    type: { type: 'string' }, high: { type: 'boolean' }, fix: { type: 'string' } }, required: ['cell', 'functions', 'friction'] } },
  findings: { type: 'array', items: { type: 'object', properties: {
    key: { type: 'string' }, cell: { type: 'string' }, type: { type: 'string', enum: ['I', 'II', 'III'] }, what: { type: 'string' },
    where: { type: 'string' }, in_scope: { type: 'boolean' }, suggested: { type: 'string' } }, required: ['key', 'type', 'what', 'in_scope'] } },
  not_covered: { type: 'array', items: { type: 'string' } },
  report: { type: 'string' }, commit: { type: 'string' } }, required: ['verdict', 'cells', 'findings', 'report', 'commit'] }

const FIX_SCHEMA = { type: 'object', properties: {
  fixed: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, commit: { type: 'string' }, test: { type: 'string' }, red: { type: 'string' } }, required: ['key', 'commit', 'test'] } },
  deferred: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, why: { type: 'string' }, owner: { type: 'string' } }, required: ['key', 'why'] } },
  verdict: { type: 'string' }, report_commit: { type: 'string' }, suite: { type: 'string' }, vet: { type: 'string' }, head: { type: 'string' } },
  required: ['fixed', 'deferred', 'verdict', 'suite', 'head'] }

const REVERIFY_SCHEMA = { type: 'object', properties: {
  results: { type: 'array', items: { type: 'object', properties: { key: { type: 'string' }, pass: { type: 'boolean' }, note: { type: 'string' } }, required: ['key', 'pass', 'note'] } },
  new_findings: { type: 'array', items: { type: 'object', properties: { what: { type: 'string' }, cell: { type: 'string' }, type: { type: 'string' } }, required: ['what'] } },
  report_accurate: { type: 'boolean' }, verdict_ok: { type: 'boolean' }, notes: { type: 'string' } }, required: ['results', 'new_findings', 'report_accurate', 'verdict_ok'] }

phase('Walk')
const walk = await run(`${SHELL}

You run the friction review that ${A.id} owes. Walk at least ten (actor, scenario) cells as the ACTUAL actor: type the commands, read the output and the errors, count the steps. Ugly failure modes are first-class cells, not an afterthought.

${CONTEXT}

ACTORS
${A.actors}

SCENARIOS (walk the ugly ones first; add any the change set suggests)
${A.scenarios}

HOW
- Build the CLI (and any other binary a cell needs) from ${WT} at ${A.head} with -o into ${W}/bin/. Make scratch archives under ${W}/ from the repository's own test fixtures or a small mbox your Python script writes; no real mail, no network.
- Run real commands through inhome. A drill that needs two processes at once, a delay, a kill at the right moment or a timing is a Python driver script.
- A cell that cannot be walked here (Windows-only behaviour, a real tenant) is reasoned from the code and its tests, and the report says so in the cell.
- Score each cell twice: does it function (yes / partial / no), and its friction (manual steps, context switches, undocumented knowledge, error-message dead ends). Classify every friction finding Type I (the ceremony is the control), II (design choice, reducible without weakening the control) or III (missing legibility or tooling: a better message, a remedy line, a status line). Say for each whether it is in this branch's scope.
- Do not fix anything in this stage.

FILE the report at ${WT}/${A.report}: the header (cells walked; fully, partially and not functioning counts; high-friction count; the verdict SHIPPABLE or HIGH_FRICTION_NOT_SHIPPABLE), the table | Cell (actor x scenario) | Functions? | Friction findings | Type | Fix / backlog item |, the findings list with keys (F1, F2, ...), and what the walk could not cover. Commit only the report.

${GOB}

${SAFETY}${note('walk')}`, { label: 'walk', phase: 'Walk', schema: WALK_SCHEMA })
if (!walk) return { id: A.id, stopped: 'walk' }
log(`walk: ${walk.cells.length} cells, ${walk.findings.length} findings, verdict ${walk.verdict}`)

phase('Fix')
const fixable = walk.findings.filter(f => f.in_scope && f.type !== 'I')
const fix = await run(`${SHELL}

You fix the in-scope findings of ${A.id}'s friction review and keep its report true. You did not walk the cells.

${CONTEXT}

THE WALK'S VERDICT: ${walk.verdict}
ITS CELLS: ${JSON.stringify(walk.cells, null, 1)}
THE FINDINGS TO FIX (in scope, Type II or III): ${JSON.stringify(fixable, null, 1)}
THE OTHERS (record them as backlog, with the slice or owner that should take each): ${JSON.stringify(walk.findings.filter(f => !fixable.includes(f)), null, 1)}

FOR EACH FINDING TO FIX
- Write the test first: it asserts what the actor should now see (for a refusal, use the assure helpers: the exact typed exit, the message naming what, why and the remedy, the side effect absent; prove the positive twin first), with the \`// covers:\` marker of the catalog row whose surface it is. Show it RED against the current code for the right reason, then fix, then GREEN, then prove-fail by reverting the fix. Record it in the commit message. One commit per finding or tightly coupled group.
- A Type II finding is fixed here only if the fix is small, keeps every control, and stays inside the design; otherwise defer it with the reason.
- Never weaken a gate, change what an existing covering test asserts, or narrow a guarantee to remove friction: defer such a finding to the owner instead.
THEN re-walk every cell a fix touched with a binary built at your head, update the report's table (Fix / backlog column, the counts) and its verdict truthfully, and commit. Finish with the unfiltered suite through inhome, vet3 and gofmt -l.

${GOB}

${SAFETY}${note('fix')}`, { label: 'fix', phase: 'Fix', schema: FIX_SCHEMA })
if (!fix) return { id: A.id, stopped: 'fix', walk: { verdict: walk.verdict, findings: walk.findings, commit: walk.commit } }
log(`fix: ${fix.fixed.length} fixed, ${fix.deferred.length} deferred, verdict ${fix.verdict}, suite ${fix.suite}`)

let rv = null
let fix2 = null
if (fix.fixed.length) {
  phase('Re-verify')
  rv = await run(`${SHELL}

Fresh-eyes check of the friction fixes made for ${A.id}. You did not write them. Default to FAIL whenever a fix does not plainly remove the friction it claims to remove.

${CONTEXT}

The code is at ${WT} (HEAD ${fix.head}); the report is ${WT}/${A.report}.
FIXES: ${JSON.stringify(fix.fixed, null, 1)}
DEFERRED: ${JSON.stringify(fix.deferred, null, 1)}
THE FINDINGS THEY ANSWER: ${JSON.stringify(walk.findings, null, 1)}

FOR EACH FIX: in your own detached worktree (\`git -C ${REPO} worktree add --detach ${S}/probe/${A.slug}-reverify ${BR}\`, removed with \`git -C ${REPO} worktree remove --force ${S}/probe/${A.slug}-reverify\` when you are done), revert only the fix and confirm its test goes red for the right reason; re-walk the cell with a binary built at ${fix.head} as the actor would; check the fix keeps every control and introduces no new friction elsewhere.
FOR EACH DEFERRAL: check that it truly needs the design's author or the owner.
THE REPORT: check its counts, its table against the code, and that the verdict is true. You write no file in ${WT}.

${GOB}${note('reverify')}`, { label: 'reverify', phase: 'Re-verify', schema: REVERIFY_SCHEMA })
  if (rv && (rv.results.some(r => !r.pass) || rv.new_findings.length || !rv.report_accurate || !rv.verdict_ok)) {
    fix2 = await run(`${SHELL}

Fix what the fresh-eyes check of ${A.id}'s friction fixes found. You did not write the fixes or the check.

${CONTEXT}

The code is at ${WT} (HEAD ${fix.head}); the report is ${WT}/${A.report}.
THE CHECK: ${JSON.stringify(rv, null, 1)}
THE FIXES IT CHECKED: ${JSON.stringify(fix.fixed, null, 1)}

For each failed result or new finding: reproduce it first; if real, fix it with the same discipline (test red for the right reason, fix, green, prove-fail recorded in the commit); if not real, say why. Then add a "Re-verification" section to the report listing each item and its outcome, keep the counts and the verdict true, and commit. Finish with the unfiltered suite through inhome, vet3 and gofmt -l.

${GOB}

${SAFETY}${note('fix2')}`, { label: 'fix 2', phase: 'Re-verify', schema: FIX_SCHEMA })
  }
}

return {
  id: A.id, branch: BR, head: (fix2 && fix2.head) || fix.head,
  walk: { verdict: walk.verdict, cells: walk.cells.length, high: walk.cells.filter(c => c.high).length,
    functions: { yes: walk.cells.filter(c => c.functions === 'yes').length, partial: walk.cells.filter(c => c.functions === 'partial').length, no: walk.cells.filter(c => c.functions === 'no').length },
    findings: walk.findings.map(f => ({ key: f.key, type: f.type, in_scope: f.in_scope, what: f.what.slice(0, 160) })), not_covered: walk.not_covered || [] },
  fix: { fixed: fix.fixed, deferred: fix.deferred, verdict: fix.verdict, suite: fix.suite },
  reverify: rv ? { failed: rv.results.filter(r => !r.pass), new_findings: rv.new_findings, report_accurate: rv.report_accurate, verdict_ok: rv.verdict_ok } : null,
  fix2: fix2 ? { fixed: fix2.fixed, deferred: fix2.deferred, verdict: fix2.verdict, suite: fix2.suite } : null,
}
