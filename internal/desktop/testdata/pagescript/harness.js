// The Overview's page script run against a small stand-in for the browser's document (MA-986):
// the Desktop's Go tests run no browser, so ma986_page_behaviour_test.go runs the served
// dashboard.js here, under Node, and reads what it did. The stand-in has only what the script
// uses: elements, attributes, children, the selectors the script queries, click and input events,
// fetch (answered per route by the scenario, each after its own delay), location.reload and
// sessionStorage. It never touches a real network, file or browser.
//
// Usage: node harness.js DASHBOARD_JS SCENARIO [ENCRYPTION_JS]. Each check prints "ok NAME" or
// "FAIL NAME: why"; the scenario ends with "done". ENCRYPTION_JS, when given, runs after DASHBOARD_JS
// in the same page, as the Overview loads encryption.js after dashboard.js (MA-1070).
'use strict';
const fs = require('fs');
const vm = require('vm');

const code = fs.readFileSync(process.argv[2], 'utf8');
const scenario = process.argv[3];
const extra = process.argv[4] ? fs.readFileSync(process.argv[4], 'utf8') : null;
let failed = false;
function check(cond, name, why) {
  if (cond) console.log('ok ' + name);
  else { failed = true; console.log('FAIL ' + name + ': ' + (why || '')); }
}

// ---- the document -----------------------------------------------------------------------
class El {
  constructor(tag) {
    this.tagName = String(tag).toUpperCase(); this.children = []; this.parent = null; this.attrs = {};
    this.text = ''; this.listeners = {}; this.onclick = null; this.value = ''; this.checked = false;
    this.disabled = false; this.style = {};
  }
  get dataset() {
    const el = this;
    const attr = k => 'data-' + String(k).replace(/[A-Z]/g, c => '-' + c.toLowerCase());
    return new Proxy({}, { get: (_, k) => el.attrs[attr(k)], set: (_, k, v) => { el.attrs[attr(k)] = String(v); return true; } });
  }
  get id() { return this.attrs.id || ''; } set id(v) { this.attrs.id = String(v); }
  get type() { return this.attrs.type || ''; } set type(v) { this.attrs.type = String(v); }
  get className() { return this.attrs.class || ''; } set className(v) { this.attrs.class = String(v); }
  get title() { return this.attrs.title || ''; } set title(v) { this.attrs.title = String(v); }
  get hidden() { return 'hidden' in this.attrs; } set hidden(v) { if (v) this.attrs.hidden = ''; else delete this.attrs.hidden; }
  get htmlFor() { return this.attrs.for; } set htmlFor(v) { this.attrs.for = String(v); }
  get content() { return this.attrs.content; }
  get href() { return this.attrs.href || ''; } set href(v) { this.attrs.href = String(v); }
  get download() { return this.attrs.download; } set download(v) { this.attrs.download = String(v); }
  get classList() {
    const el = this, list = () => (el.attrs.class || '').split(/\s+/).filter(Boolean);
    return {
      contains: c => list().includes(c),
      add: c => { const l = list(); if (!l.includes(c)) l.push(c); el.attrs.class = l.join(' '); },
      remove: c => { el.attrs.class = list().filter(x => x !== c).join(' '); },
      toggle: (c, on) => { const l = list().filter(x => x !== c); if (on) l.push(c); el.attrs.class = l.join(' '); },
    };
  }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
  removeAttribute(k) { delete this.attrs[k]; }
  hasAttribute(k) { return k in this.attrs; }
  get textContent() { return this.tagName === '#TEXT' ? this.text : this.text + this.children.map(c => c.textContent).join(''); }
  set textContent(v) { this.children.forEach(c => { c.parent = null; }); this.children = []; this.text = String(v); }
  get firstChild() { return this.children[0] || null; }
  get nextElementSibling() { const s = this.parent ? this.parent.children : []; return s[s.indexOf(this) + 1] || null; }
  get isConnected() { let n = this; while (n.parent) n = n.parent; return n === doc.root; }
  detach(n) { if (n.parent) { const s = n.parent.children; s.splice(s.indexOf(n), 1); n.parent = null; } }
  append(...ns) { ns.forEach(n => { if (typeof n === 'string') n = doc.createTextNode(n); this.detach(n); n.parent = this; this.children.push(n); }); }
  appendChild(n) { this.append(n); return n; }
  prepend(...ns) { ns.reverse().forEach(n => { this.detach(n); n.parent = this; this.children.unshift(n); }); }
  removeChild(n) { this.detach(n); return n; }
  remove() { this.detach(this); }
  after(...ns) { const p = this.parent; let i = p.children.indexOf(this) + 1; ns.forEach(n => { this.detach(n); n.parent = p; p.children.splice(i++, 0, n); }); }
  before(...ns) { const p = this.parent; let i = p.children.indexOf(this); ns.forEach(n => { this.detach(n); n.parent = p; p.children.splice(i++, 0, n); }); }
  addEventListener(ev, f) { (this.listeners[ev] = this.listeners[ev] || []).push(f); }
  click() {
    if (this.disabled) return;
    // A link with a download name clicked is a file the browser saves (MA-1131's Save this record).
    if (this.tagName === 'A' && 'download' in this.attrs) downloads.push(this.attrs.download);
    (this.listeners.click || []).forEach(f => f({ preventDefault() {} })); if (this.onclick) this.onclick();
  }
  fire(ev) { (this.listeners[ev] || []).forEach(f => f({})); }
  // press is a key pressed while this element has the keyboard (keydown), as a browser sends it.
  press(key) { let stopped = false; (this.listeners.keydown || []).forEach(f => f({ key, isComposing: false, preventDefault() { stopped = true; } })); return stopped; }
  focus() { doc.activeElement = this; }
  scrollIntoView(o) { scrolled.push(this); scrollOpts.push(o); }
  all() { const r = []; const walk = n => n.children.forEach(c => { if (c.tagName !== '#TEXT') { r.push(c); walk(c); } }); walk(this); return r; }
  matches(sel) { return sel.split(',').some(s => matchComplex(this, s.trim())); }
  querySelectorAll(sel) { const r = this.all().filter(e => e.matches(sel)); r.item = i => r[i]; return r; }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
  closest(sel) { let n = this; while (n && n.tagName !== '#ROOT') { if (n.matches(sel)) return n; n = n.parent; } return null; }
}
function matchCompound(el, s) {
  const re = /([a-zA-Z][\w-]*)|\.([\w-]+)|#([\w-]+)|\[([\w-]+)(?:=(?:'([^']*)'|"([^"]*)"|([^\]]*)))?\]/g;
  let m, pos = 0;
  while ((m = re.exec(s))) {
    if (m.index !== pos) throw new Error('selector the stand-in cannot read: ' + s);
    pos = re.lastIndex;
    if (m[1] && el.tagName !== m[1].toUpperCase()) return false;
    if (m[2] && !el.classList.contains(m[2])) return false;
    if (m[3] && el.id !== m[3]) return false;
    if (m[4]) { if (!(m[4] in el.attrs)) return false; const v = m[5] ?? m[6] ?? m[7]; if (v !== undefined && el.attrs[m[4]] !== v) return false; }
  }
  if (pos !== s.length) throw new Error('selector the stand-in cannot read: ' + s);
  return true;
}
function matchComplex(el, s) {
  const parts = s.split(/\s+/);
  if (!matchCompound(el, parts[parts.length - 1])) return false;
  let n = el.parent;
  for (let i = parts.length - 2; i >= 0; i--) {
    while (n && n.tagName !== '#ROOT' && !matchCompound(n, parts[i])) n = n.parent;
    if (!n || n.tagName === '#ROOT') return false;
    n = n.parent;
  }
  return true;
}
const doc = {
  root: new El('#root'), activeElement: null,
  createElement: t => new El(t),
  createTextNode: t => { const n = new El('#text'); n.text = String(t); return n; },
  getElementById(id) { return this.root.all().find(e => e.id === id) || null; },
  querySelector(s) { return this.root.querySelector(s); },
  querySelectorAll(s) { return this.root.querySelectorAll(s); },
};
doc.body = doc.createElement('body'); doc.root.append(doc.body);
function el(tag, attrs, ...kids) {
  const e = doc.createElement(tag);
  Object.entries(attrs || {}).forEach(([k, v]) => e.setAttribute(k, v));
  kids.forEach(k => e.append(typeof k === 'string' ? doc.createTextNode(k) : k));
  return e;
}

// ---- the Overview, as the server draws it ------------------------------------------------
// row is one row of Your archives: state "current", "other", "running" (a delete under way).
function row(path, state) {
  const li = el('li', { class: 'archive-row', 'data-path': path, 'data-delete-ok': 'true', 'data-delete-why': '' });
  if (state === 'running') li.setAttribute('data-delete-running', '');
  li.append(el('span', { class: 'mono archive-path' }, path));
  const st = el('span', { class: 'row-state' });
  if (state === 'running') st.append(el('span', { class: 'archive-tag' }, 'Being deleted'));
  else {
    if (state === 'current') st.append(el('span', { class: 'archive-tag current' }, 'Current archive'));
    else st.append(el('button', { class: 'btn secondary', type: 'button', 'data-switch-archive': '', 'data-path': path }, 'Switch to this archive'));
    st.append(el('button', { class: 'btn secondary', type: 'button', 'data-pause-archive': '', 'data-path': path }, 'Pause (make read-only)'));
  }
  li.append(st);
  if (state !== 'running') {
    li.append(el('button', { class: 'btn secondary', type: 'button', 'data-unlist-archive': '', 'data-path': path }, 'Remove from list'));
    li.append(el('button', { class: 'btn danger', type: 'button', 'data-delete-archive': '', 'data-path': path }, 'Delete…'));
  }
  return li;
}
// page draws the Overview: rows [[path, state]], the archive it was drawn for, the held results.
function page(rows, drawnFor, held) {
  doc.body.append(el('meta', { name: 'csrf', content: 'c'.repeat(64) }), el('meta', { name: 'drawn-for', content: drawnFor }));
  doc.body.append(el('nav', { class: 'nav' }, el('a', { href: '#sec-archive', id: 'navArchive' }, 'Archive now'), el('a', { href: '#sec-backup', id: 'navRunNow' }, 'Run now'),
    el('a', { href: 'http://127.0.0.1:1/s/x/', target: '_blank', rel: 'noopener' }, 'Open archive'), el('a', { href: '#sec-encryption', id: 'navEncryption' }, 'Encryption'),
    el('a', { href: '#sec-health' }, 'Status & health')));
  doc.body.append(el('div', { id: 'heldDeletes', 'data-held-deletes': JSON.stringify(held || []), hidden: '' }));
  doc.body.append(el('div', { class: 'status-grid' }, el('div', { class: 'status-card' }, 'Scheduled backup')));
  const card = el('div', { class: 'card', id: 'sec-archives' }, el('h2', {}, 'Your archives'), el('p', { class: 'card-sub warn', id: 'switchLocks' }, 'Switching to another archive locks ' + drawnFor + '.'));
  const ul = el('ul', { class: 'archive-list' });
  rows.forEach(([p, s]) => ul.append(row(p, s)));
  card.append(ul); doc.body.append(card);
  doc.body.append(el('div', { class: 'card', id: 'sec-archive' }, el('button', { class: 'btn', id: 'archiveBtn' }, 'Archive now'), el('div', { class: 'note', id: 'captureResult', hidden: '' })));
  doc.body.append(el('div', { class: 'card', id: 'sec-backup' }, el('button', { class: 'btn', id: 'runBackupBtn', type: 'button' }, 'Run now'), el('div', { class: 'note', id: 'runBackupNote', hidden: '' })));
  const outDir = el('input', { id: 'outDir' }); outDir.value = drawnFor;
  doc.body.append(el('div', { class: 'card', id: 'sec-settings' }, outDir, el('div', { class: 'note', id: 'settingsNote', hidden: '' }),
    el('div', { class: 'note warnbox', id: 'settingsConfirm', hidden: '' }, el('p', { id: 'settingsConfirmText' }))));
  doc.body.append(el('div', { class: 'card', id: 'sec-encryption' }, el('h2', {}, 'Encryption'), el('p', { class: 'card-sub', id: 'encState' }, "Unlocked: you can read this archive's messages."),
    el('button', { class: 'btn secondary', type: 'button', id: 'encLock' }, 'Lock')));
  doc.body.append(el('div', { class: 'card', id: 'sec-health' }, el('h2', {}, 'Status & health'), el('pre', {}, 'Posture: OK')));
  doc.body.append(el('div', { class: 'card sec', id: 'sec-uninstall' }, el('h2', {}, 'Uninstall MailArchive'), el('button', { class: 'btn secondary', type: 'button' }, 'Uninstall MailArchive…')));
}

// encSteps draws the Overview's Make a new encrypted archive… button and its steps card, as
// dashboard.go draws them (MA-1070; ma1070_step_in_view_test.go pins the served markup's ids): the
// card is hidden until a step needs it, each step with its own status line.
function encSteps() {
  doc.body.prepend(el('div', { class: 'card', id: 'noArchive' }, el('h2', {}, 'No archive chosen yet'),
    el('button', { class: 'btn secondary', type: 'button', 'data-create-encrypted': '' }, 'Make a new encrypted archive…')));
  const input = (id, type) => { const i = el('input', { id, type }); return i; };
  doc.body.append(el('div', { class: 'card', id: 'sec-encsteps', hidden: '' }, el('h2', {}, 'Encryption'),
    el('div', { id: 'encCreateForm', hidden: '' }, el('p', { class: 'card-sub', role: 'status', 'data-step-status': '', hidden: '' }),
      el('p', { class: 'card-sub', id: 'encCreatePath' }), el('p', { class: 'card-sub', id: 'encCreateHint' }),
      el('label', { for: 'encCreatePw' }, 'Password for the new archive'), input('encCreatePw', 'password'),
      el('label', { for: 'encCreatePw2' }, 'Type it again'), input('encCreatePw2', 'password'),
      input('encCreateRemember', 'checkbox'), input('encCreateProgram', 'checkbox'),
      el('button', { class: 'btn', type: 'button', id: 'encCreateKeyBtn' }, 'Continue — show the recovery key')),
    el('div', { id: 'encNewKeyBox', hidden: '' }, el('p', { class: 'card-sub', role: 'status', 'data-step-status': '', hidden: '' }),
      el('p', { class: 'mono', id: 'encNewKey', tabindex: '-1' }), el('p', { class: 'card-sub warn', id: 'encNewKeyWarn' }),
      el('button', { class: 'btn secondary', type: 'button', id: 'encSaveFileBtn' }, 'Save the recovery file (for lost key files)…'),
      el('button', { class: 'btn secondary', type: 'button', id: 'encSavedBtn' }, 'I have saved the recovery key'),
      el('div', { id: 'encTypeBack', hidden: '' }, el('p', { class: 'card-sub', role: 'status', 'data-step-status': '', hidden: '' }),
        el('label', { for: 'encTypedKey' }, 'Type the recovery key you saved'), input('encTypedKey', 'text'),
        el('button', { class: 'btn', type: 'button', id: 'encUseKeyBtn' }, 'Use this recovery key'),
        el('button', { class: 'btn secondary', type: 'button', id: 'encShowAgainBtn' }, 'Show it again')),
      el('button', { class: 'btn secondary', type: 'button', id: 'encCancelKeyBtn' }, 'Cancel')),
    el('p', { class: 'small', id: 'encNote', hidden: '' })));
}
// encLocked replaces the Encryption card with a locked archive's (its Unlock box), or one whose key
// files are missing (its recovery key box), as dashboard.go draws them, each with its status line.
function encLocked(kind) {
  const old = doc.getElementById('sec-encryption'); if (old) old.remove();
  const card = el('div', { class: 'card', id: 'sec-encryption' }, el('h2', {}, 'Encryption'), el('p', { class: 'card-sub', role: 'status', 'data-step-status': '', hidden: '' }));
  if (kind === 'locked') card.append(el('form', { id: 'encUnlockForm' }, el('label', { for: 'encSecret' }, 'Password or recovery key'), el('input', { id: 'encSecret', type: 'password' }),
    el('button', { class: 'btn', type: 'submit', id: 'encUnlock' }, 'Unlock')));
  if (kind === 'missing') card.append(el('div', { id: 'encRestoreForm' }, el('label', { for: 'encRestoreKey' }, 'Recovery key'), el('input', { id: 'encRestoreKey', type: 'text' }),
    el('button', { class: 'btn', type: 'button', id: 'encRestoreFileBtn' }, 'Choose the recovery file and put back the key file')));
  if (kind === 'unlocked') card.append(el('button', { class: 'btn secondary', type: 'button', id: 'encLock' }, 'Lock'));
  doc.body.append(card);
}
// statusOf is the step's own status line's words ('' when it is hidden).
const statusOf = id => { const s = doc.getElementById(id).querySelector('[data-step-status]'); return s && !s.hidden ? s.textContent : ''; };

// restoreCard draws the Restore to Thunderbird card as dashboard.go draws it (MA-1131;
// ma1131_restore_flow_test.go pins the served markup's step ids and status lines): its button, and
// the panel's five steps, each opening with its own status line.
function restoreCard() {
  const st = () => el('p', { class: 'card-sub', role: 'status', 'data-step-status': '', hidden: '' });
  const radio = (id, value, on) => { const r = el('input', { type: 'radio', name: 'rtbWhat', id, value }); r.checked = on; return r; };
  doc.body.append(el('div', { class: 'card', id: 'sec-restore-tb' }, el('h2', {}, 'Restore to Thunderbird'),
    el('button', { class: 'btn secondary', id: 'rtbBtn', type: 'button' }, 'Restore to Thunderbird…'),
    el('div', { id: 'rtbPanel', hidden: '' },
      el('div', { id: 'rtbProfileStep', hidden: '' }, st(), el('div', { id: 'rtbProfiles' })),
      el('div', { id: 'rtbWhatStep', hidden: '' }, st(), radio('rtbWhole', 'whole', true), radio('rtbChoose', 'choose', false), el('div', { id: 'rtbTree', hidden: '' }),
        el('button', { class: 'btn', id: 'rtbCheck', type: 'button' }, 'Check'), el('button', { class: 'btn secondary', id: 'rtbBack', type: 'button', hidden: '' }, 'Change profile'),
        el('button', { class: 'btn secondary', id: 'rtbClose', type: 'button' }, 'Cancel')),
      el('div', { id: 'rtbPlanStep', hidden: '' }, st(), el('ul', { id: 'rtbPlan' }), el('div', { id: 'rtbTickBox', hidden: '' }, el('input', { type: 'checkbox', id: 'rtbTick' })),
        el('div', { id: 'rtbAgainBox', hidden: '' }, el('p', { id: 'rtbAgainWhy' }), el('input', { type: 'checkbox', id: 'rtbAgain' })),
        el('button', { class: 'btn', id: 'rtbGo', type: 'button' }, 'Restore'), el('button', { class: 'btn secondary', id: 'rtbPlanCancel', type: 'button' }, 'Cancel')),
      el('div', { id: 'rtbRunStep', hidden: '' }, st(), el('p', { id: 'rtbProgress' }), el('p', { id: 'rtbWait' }),
        el('button', { class: 'btn secondary', id: 'rtbStop', type: 'button' }, 'Cancel the restore')),
      el('div', { id: 'rtbResultStep', hidden: '' }, st(), el('p', { id: 'rtbResult' }),
        el('button', { class: 'btn secondary', id: 'rtbSave', type: 'button', hidden: '' }, 'Save this record'), el('span', { id: 'rtbRecordNote', hidden: '' }),
        el('button', { class: 'btn secondary', id: 'rtbDone', type: 'button' }, 'Close'))),
    el('div', { class: 'note', id: 'rtbNote', role: 'status', hidden: '' })));
}
// The words each restore step says when it is brought into view (MA-1131; the builder's, under the
// owner-delegated rule), W-WAIT as macOS and Linux say it, and the check's line (R6-10).
const rtbWords = {
  profile: 'This computer has more than one Thunderbird profile. Choose the one to restore into, then click Next.',
  what: 'Next, choose what to restore: the whole archive, or the folders you tick. Then click Check: nothing is written until you click Restore.',
  tree: 'Tick the folders to restore; a folder brings its subfolders. The number beside each is how many messages in it and its subfolders can go back. Then click Check.',
  plan: 'This is what the restore will do; nothing is written yet. Click Restore to go ahead, or Cancel.',
  planEnc: 'This is what the restore will do; nothing is written yet. The restored mail will not be encrypted: tick the box below if you understand, and the second one, then click Restore.',
  run: 'The restore is running. Leave Thunderbird closed until it finishes. Cancel the restore stops it, and nothing is added to Thunderbird.',
  done: 'The restore has finished. What happened is said below. Save this record keeps a copy as a text file.',
  stopped: 'The restore was stopped. What happened is said below. Save this record keeps a copy as a text file.',
  wait: 'While this runs, leave Thunderbird closed. If Thunderbird says it is already open or running, close that message and wait for the restore to finish.',
  checking: 'Checking what was written: every message is read back before Thunderbird can show the folder…',
};
const untilTrue = async (f, ms) => { for (let i = 0; i < (ms || 600) / 5 && !f(); i++) await wait(5); return f(); };
const reset = () => { doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; Object.keys(replies).forEach(k => delete replies[k]); };

// ---- the browser around it -------------------------------------------------------------
const posts = [], gets = [], scrolled = [], scrolledTop = [], scrollOpts = [], blobs = [], downloads = [];
let reloads = 0, reduceMotion = false, storageOff = false;
const loadListeners = [];
const replies = {}, delays = {};
const NOANSWER = Symbol('no answer');
const store = {};
function fetchStub(path, opts) {
  const body = opts && opts.body ? JSON.parse(opts.body) : null;
  if (opts && opts.method === 'POST') posts.push({ path, body }); else gets.push(path);
  const r = typeof replies[path] === 'function' ? replies[path](body) : (replies[path] || { ok: true });
  const ms = typeof delays[path] === 'function' ? delays[path](body) : (delays[path] || 0);
  // NOANSWER: the server is gone (killed, crashed): the fetch fails, as a browser's does.
  if (r === NOANSWER) return new Promise((res, rej) => setTimeout(() => rej(new TypeError('Failed to fetch')), ms));
  return new Promise(res => setTimeout(() => res({ json: () => Promise.resolve(r), status: 200 }), ms));
}
// boot runs the page script on the page as drawn, as a tab loading it would (a reload is a new
// boot: the same tab's sessionStorage, a fresh script).
let ctx = null;
function boot() {
  ctx = {
    document: doc, fetch: fetchStub, atob: s => Buffer.from(s, 'base64').toString('latin1'),
    Uint8Array, Blob, JSON, Array, Promise, Object, String, Number, Error, Proxy,
    URL: { createObjectURL: b => { blobs.push(b); return 'blob:1'; }, revokeObjectURL() {} },
    location: { reload() { reloads++; }, hash: '' },
    // storageOff: a browser that keeps nothing for the tab (a private window, blocked site data):
    // every call throws, as such a browser's does.
    sessionStorage: { getItem: k => { if (storageOff) throw new Error('no storage'); return k in store ? store[k] : null; },
      setItem: (k, v) => { if (storageOff) throw new Error('no storage'); store[k] = String(v); }, removeItem: k => { if (storageOff) throw new Error('no storage'); delete store[k]; } },
    setTimeout: (f, ms) => setTimeout(f, Math.min(ms, 20)), setInterval: () => 0, clearTimeout, console,
    scrollTo: (x, y) => { scrolledTop.push([x, y]); },
    confirm: () => { throw new Error('the page asked through confirm()'); },
    prompt: () => { throw new Error('the page asked through prompt()'); },
    // The person's motion setting (prefers-reduced-motion), set per scenario.
    matchMedia: q => ({ matches: /prefers-reduced-motion:\s*reduce/.test(q) && reduceMotion, media: q }),
  };
  ctx.window = ctx; ctx.addEventListener = (ev, f) => { if (ev === 'load') loadListeners.push(f); };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  if (extra) vm.runInContext(extra, ctx);
}
const wait = ms => new Promise(r => setTimeout(r, ms));
const rb = Buffer.from('the record', 'utf8').toString('base64');
function plan(path, name, token) {
  return { ok: true, token, row: path, folder: path, name, kind: 'plain', files: 3, bytes: 10, size: '10 bytes', schedules: [], checkYourself: [], notes: [],
    paused: false, synced: '', kept: 0, unrecorded: 0, unrecordedNames: [], stores: [], rerun: false,
    typeLine: "Type the folder's name, " + name + ', to delete it', doesNotReach: 'What Delete does not reach: …', logLine: 'deletions.log', threeLine: 'three', planText: 'plan', planName: 'plan.txt' };
}
function result(fields) {
  return Object.assign({ ok: true, id: 'H1', row: '', folder: '', name: '', status: 'complete', lines: ['Deleted.'], rowRemoved: false, unfinished: false,
    record: 'the record', recordBase64: rb, recordName: 'record.txt', rows: [], page: '' }, fields);
}
const byText = (root, t) => root.querySelectorAll('button').find(b => b.textContent === t) || null;
const shown = id => { const x = doc.getElementById(id); return !!x && !x.hidden; };
// typeAndDelete opens li's panel (its plan already answered), types name and presses Delete permanently.
async function typeAndDelete(li, name) {
  const inp = li.querySelector('.delete-panel input'); inp.value = name; inp.fire('input');
  byText(li, 'Delete permanently').click();
}

// The words each step says when it is brought into view (MA-1070; lead's words, owner-delegated
// 2026-11-02: what happened first, then what to do next).
const stepWords = {
  create: 'The folder is chosen. Next, choose a password for the new archive: type it below, then type it again.',
  keyCreate: 'The password is set. Next, save the recovery key shown below (groups of letters and digits) somewhere safe, such as a password manager, then click I have saved the recovery key.',
  keyRestore: 'The new password is set. Next, save the new recovery key shown below (groups of letters and digits) somewhere safe, such as a password manager, then click I have saved the recovery key.',
  keyNew: 'A new recovery key is made. Next, save it (the groups of letters and digits below) somewhere safe, such as a password manager, then click I have saved the recovery key.',
  typeBack: 'The recovery key is hidden now. Type it below from where you saved it: the groups of letters and digits, like ABCD-EFGH-…, not the recovery file. Show it again brings it back.',
  unlock: 'This archive is encrypted and locked. Next, type its password or recovery key below and click Unlock.',
  missing: "This archive's key files are missing. Next, type its recovery key below and choose a new password, then click Choose the recovery file and put back the key file.",
};
const lastScroll = () => ({ el: scrolled[scrolled.length - 1], opts: scrollOpts[scrollOpts.length - 1] || {} });
const scenarios = {
  // Make a new encrypted archive… (MA-1070; the owner's report of 2026-11-03: once the folder was
  // chosen the page seemed to stop, waiting for a password far down the page). When the wizard's
  // windows answer, the password step is brought into view, the keyboard is in its first box, and
  // its status line says what is needed next; smoothly, or at once for a person who asked for
  // reduced motion. No password typed is said anywhere. Positive twin first: the wizard backed out
  // of moves nothing.
  async createStep() {
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps();
    boot(); await wait(30);
    const b = doc.querySelector('[data-create-encrypted]');
    replies['api/encryption/create'] = { ok: true, canceled: true };
    b.focus(); b.click(); await wait(40);
    const s0 = scrolled.length;
    check(scrolled.length === 0 && doc.activeElement === b && doc.getElementById('encCreateForm').hidden, 'the wizard backed out of moves nothing');
    replies['api/encryption/create'] = { ok: true, token: 'T1', path: 'D:\\Mail\\Enc', hint: 'Use at least 12 characters.' };
    b.click(); await wait(40);
    const form = doc.getElementById('encCreateForm'), pw = doc.getElementById('encCreatePw'), ls = lastScroll();
    check(!form.hidden && !doc.getElementById('sec-encsteps').hidden && scrolled.length > s0 && ls.el === form, 'the password step is brought into view', JSON.stringify({ n: scrolled.length, id: ls.el && ls.el.id }));
    check(doc.activeElement === pw, 'the keyboard is in its first box', doc.activeElement && doc.activeElement.id);
    check(statusOf('encCreateForm') === stepWords.create && form.querySelector('[data-step-status]').getAttribute('role') === 'status', 'its status line says what is needed next', statusOf('encCreateForm'));
    check(ls.opts.behavior === 'smooth', 'it scrolls smoothly when the person has not asked for reduced motion', JSON.stringify(ls.opts));
    pw.value = 'a secret typed by the person'; doc.getElementById('encCreatePw2').value = 'a secret typed by the person';
    replies['api/encryption/create/key'] = { ok: false, error: 'The two passwords do not match.' };
    doc.getElementById('encCreateKeyBtn').click(); await wait(40);
    check(!doc.root.all().some(e => e.tagName !== 'INPUT' && e.textContent.includes('a secret typed by the person')), 'no password typed is said on the page');
    // Reduced motion: the same step, brought into view at once.
    doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; reduceMotion = true;
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps();
    boot(); await wait(30);
    doc.querySelector('[data-create-encrypted]').click(); await wait(40);
    const rs = lastScroll();
    check(rs.el === doc.getElementById('encCreateForm') && rs.opts.behavior === 'auto' && doc.activeElement === doc.getElementById('encCreatePw'), 'for reduced motion it is brought into view at once', JSON.stringify(rs.opts));
  },
  // The recovery key's steps (MA-1070, MA-1071): once the key is made — for a new archive, a new
  // recovery key or a key file put back — its box is brought into view with the keyboard on the key
  // itself (amended 2026-11-04, the review's LOW-4: it was on Save the recovery file, so Enter there
  // began the owner's second report's confusion) and says what to save; after I have saved the recovery key, the box to type it back is in
  // view, the keyboard in it, and its status line says it wants the recovery key's groups of letters
  // and digits, not the recovery file. Positive twin first: a key that could not be made moves nothing.
  async keySteps() {
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps();
    boot(); await wait(30);
    replies['api/encryption/create'] = { ok: true, token: 'T1', path: 'D:\\Mail\\Enc', hint: '' };
    doc.querySelector('[data-create-encrypted]').click(); await wait(40);
    const kb = doc.getElementById('encCreateKeyBtn'), s0 = scrolled.length;
    replies['api/encryption/create/key'] = { ok: false, error: 'That has 5 characters; use at least 12.' };
    kb.focus(); kb.click(); await wait(40);
    check(scrolled.length === s0 && doc.getElementById('encNewKeyBox').hidden, 'a key that could not be made moves nothing');
    replies['api/encryption/create/key'] = { ok: true, token: 'T1', recoveryKey: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2', warning: 'Save this recovery key.' };
    kb.click(); await wait(40);
    const box = doc.getElementById('encNewKeyBox');
    check(!box.hidden && lastScroll().el === box && doc.activeElement === doc.getElementById('encNewKey') && statusOf('encNewKeyBox') === stepWords.keyCreate,
      "the new archive's recovery key is brought into view, the keyboard on the key itself", JSON.stringify({ id: lastScroll().el && lastScroll().el.id, a: doc.activeElement && doc.activeElement.id, s: statusOf('encNewKeyBox') }));
    doc.getElementById('encSavedBtn').click(); await wait(20);
    const tb = doc.getElementById('encTypeBack');
    check(!tb.hidden && lastScroll().el === tb && doc.activeElement === doc.getElementById('encTypedKey') && doc.getElementById('encNewKey').textContent === '',
      'after I have saved the recovery key, the box to type it back is in view with the keyboard in it, the key hidden');
    check(statusOf('encTypeBack') === stepWords.typeBack, 'its status line wants the recovery key, not the recovery file', statusOf('encTypeBack'));
    // The key typed back: a refusal is said under the box, in view, nothing moved; once it takes
    // effect the box goes, and what happened is brought into view. The earlier step's line is gone.
    const s1 = scrolled.length;
    replies['api/encryption/new-recovery-key/confirm'] = { ok: false, error: 'That has a typing mistake.' };
    doc.getElementById('encUseKeyBtn').click(); await wait(40);
    const refusedStill = scrolled.length === s1;
    replies['api/encryption/new-recovery-key/confirm'] = { ok: true, lines: ['The encrypted archive was made in D:\\Mail\\Enc.'], running: true };
    doc.getElementById('encUseKeyBtn').click(); await wait(40);
    const note = doc.getElementById('encNote');
    check(refusedStill && statusOf('encNewKeyBox') === '' && box.hidden && lastScroll().el === note && note.textContent === 'The encrypted archive was made in D:\\Mail\\Enc.',
      'once the key takes effect what happened is in view; a refusal moves nothing, and one step is said at a time', JSON.stringify({ refusedStill, s: statusOf('encNewKeyBox'), id: lastScroll().el && lastScroll().el.id }));
    // A new recovery key on an unlocked archive's card.
    doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null;
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps();
    doc.getElementById('sec-encryption').append(el('button', { class: 'btn secondary', type: 'button', id: 'encNewKeyBtn' }, 'Make a new recovery key'));
    boot(); await wait(30);
    replies['api/encryption/new-recovery-key'] = { ok: true, token: 'T2', recoveryKey: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2', warning: '' };
    doc.getElementById('encNewKeyBtn').click(); await wait(40);
    check(lastScroll().el === doc.getElementById('encNewKeyBox') && doc.activeElement === doc.getElementById('encNewKey') && statusOf('encNewKeyBox') === stepWords.keyNew,
      'a new recovery key is brought into view, the keyboard on the key itself', statusOf('encNewKeyBox'));
    // A key file put back, from the recovery file chosen in its window.
    doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null;
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps(); encLocked('missing');
    doc.getElementById('encRestoreForm').append(el('input', { id: 'encRestorePw', type: 'password' }), el('input', { id: 'encRestorePw2', type: 'password' }));
    boot(); await wait(30);
    replies['api/encryption/restore'] = { ok: true, token: 'T3', recoveryKey: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2', warning: '' };
    doc.getElementById('encRestoreFileBtn').click(); await wait(40);
    check(lastScroll().el === doc.getElementById('encNewKeyBox') && doc.activeElement === doc.getElementById('encNewKey') && statusOf('encNewKeyBox') === stepWords.keyRestore,
      "after the recovery file's window, the new recovery key is brought into view, the keyboard on the key itself", statusOf('encNewKeyBox'));
  },
  // Open an archive folder… on an encrypted archive (MA-1070's sweep): the page drawn for it after the
  // folder picker is at its Encryption card, the keyboard in the Unlock box (or, when its key files are
  // missing, in the recovery key box), its status line saying what is needed next, once. Positive twins
  // first: a page drawn with nothing asked is not moved; an archive that needs no unlock is not moved.
  async openThenUnlock() {
    const key = 'mailarchive-step-unlock';
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encLocked('locked');
    doc.body.append(el('div', { class: 'card', id: 'sec-open' }, el('button', { class: 'btn', type: 'button', 'data-open-archive': '' }, 'Open an archive folder…')));
    boot(); await wait(30);
    check(scrolled.length === 0 && doc.activeElement === null && statusOf('sec-encryption') === '', 'a page drawn with nothing asked is not moved');
    replies['api/open-archive'] = { ok: true, path: 'F:\\Enc' }; replies['api/settings'] = { ok: true };
    doc.querySelector('[data-open-archive]').click(); await wait(60);
    const marked = store[key] === '1' && reloads >= 1;
    const fresh = kind => { doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; page([['F:\\Enc', 'current']], 'F:\\Enc'); encLocked(kind); const s0 = scrolled.length; boot(); return s0; };
    let s0 = fresh('locked'); await wait(30);
    const card = doc.getElementById('sec-encryption');
    check(marked && scrolled.slice(s0).includes(card) && doc.activeElement === doc.getElementById('encSecret') && statusOf('sec-encryption') === stepWords.unlock && !(key in store),
      'the page drawn for the encrypted archive opened is at its Unlock box, the keyboard in it, once', JSON.stringify({ marked, a: doc.activeElement && doc.activeElement.id, s: statusOf('sec-encryption'), store }));
    check(!reduceMotion && lastScroll().opts.behavior === 'auto', 'a page just drawn moves at once, never smoothly (the browser would cancel it)', JSON.stringify(lastScroll().opts));
    loadListeners.forEach(f => f()); await wait(10);
    check(scrolled[scrolled.length - 1] === card, 'and again once the browser has put back its scroll');
    s0 = fresh('locked'); await wait(30);
    check(scrolled.length === s0 && doc.activeElement === null, 'the next page drawn is not moved');
    store[key] = '1'; s0 = fresh('unlocked'); await wait(30);
    check(scrolled.length === s0 && doc.activeElement === null && !(key in store), 'an archive that needs no unlock is not moved, and the mark goes');
    store[key] = '1'; s0 = fresh('missing'); await wait(30);
    check(scrolled.slice(s0).includes(doc.getElementById('sec-encryption')) && doc.activeElement === doc.getElementById('encRestoreKey') && statusOf('sec-encryption') === stepWords.missing,
      'one whose key files are missing is at its recovery key box', statusOf('sec-encryption'));
  },
  // After "archive made" (MA-1072; the owner's pick Q5 of 2026-11-04 and the review's MED-3: the page
  // reloaded after eight seconds and nothing showed the first backup): what it said is kept for the
  // page drawn next, which lands at Your archives on the new archive's row, says it there with the
  // keyboard on the row, and follows the first backup there until it ends — running, then finished or
  // failed. A browser that keeps nothing for the tab is not reloaded: the backup is followed in place.
  // Positive twin first: a page drawn with nothing kept is not moved and says nothing.
  async madeLanding() {
    const key = 'mailarchive-made-archive', enc = 'D:\\Mail\\Enc';
    const made = ['The encrypted archive was made in D:\\Mail\\Enc.', 'Its first backup is running now: its row in Your archives says how it goes. Switch to it to read the archive.'];
    const until = async (f, ms) => { for (let i = 0; i < (ms || 400) / 5 && !f(); i++) await wait(5); return f(); };
    const toTypeBack = async () => {
      replies['api/encryption/create'] = { ok: true, token: 'T1', path: enc, hint: '' };
      doc.querySelector('[data-create-encrypted]').click(); await wait(40);
      replies['api/encryption/create/key'] = { ok: true, token: 'T1', recoveryKey: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2', warning: '' };
      doc.getElementById('encCreateKeyBtn').click(); await wait(40);
      doc.getElementById('encSavedBtn').click(); await wait(20);
    };
    const fresh = () => { doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; page([['E:\\Cur', 'current'], [enc, 'other']], 'E:\\Cur'); encSteps(); const s0 = scrolled.length; boot(); return s0; };
    page([['E:\\Cur', 'current'], [enc, 'other']], 'E:\\Cur'); encSteps();
    boot(); await wait(30);
    check(scrolled.length === 0 && doc.activeElement === null && !doc.querySelector('[data-made-note]'), 'a page drawn with nothing kept is not moved and says nothing');
    await toTypeBack();
    replies['api/encryption/new-recovery-key/confirm'] = { ok: true, lines: made, running: true, path: enc, job: '7' };
    const r0 = reloads;
    doc.getElementById('encUseKeyBtn').click(); await until(() => reloads > r0);
    let kept = null; try { kept = JSON.parse(store[key]); } catch (e) {}
    check(reloads === r0 + 1 && !!kept && kept.path === enc && kept.said === made.join('\n') && kept.job === '7', 'archive made: what it said is kept for the page drawn next, which is drawn', JSON.stringify({ reloads, kept }));
    // The page drawn next: the first backup runs, then finishes.
    let n = 0;
    replies['api/activity'] = () => (++n < 4 ? { running: true, job: '7', log: ['Reading Inbox…'] } : { running: false, job: '7', result: 'Archived — 3 message(s) exported, 3 indexed.', error: '' });
    let s0 = fresh(); await wait(5);
    let li = doc.querySelectorAll('.archive-row')[1], note = li.querySelector('[data-made-note]');
    check(!!note && note.getAttribute('role') === 'status' && note.textContent.startsWith(made.join('\n')) && scrolled.slice(s0).includes(li) && doc.activeElement === li && !(key in store),
      "the page drawn next is at Your archives, on the new archive's row, saying what was made, the keyboard on the row, once", JSON.stringify({ note: note && note.textContent, a: doc.activeElement && doc.activeElement.tagName, store }));
    const running = await until(() => note.textContent.endsWith('Its first backup is running: Reading Inbox…'));
    const finished = await until(() => note.textContent.endsWith('Its first backup finished: Archived — 3 message(s) exported, 3 indexed.'));
    check(running && finished, 'its first backup is followed on its row: running, then finished', note.textContent);
    // A first backup that failed is said as failed; one whose slot another task took since is said as unseen.
    store[key] = JSON.stringify({ path: enc, said: made[0], job: '8' });
    replies['api/activity'] = { running: false, job: '8', result: '', error: 'The backup could not read its mail source.' };
    fresh(); await wait(5);
    note = doc.querySelectorAll('.archive-row')[1].querySelector('[data-made-note]');
    const failed = await until(() => note.textContent.endsWith('Its first backup did not finish: The backup could not read its mail source.'));
    store[key] = JSON.stringify({ path: enc, said: made[0], job: '9' });
    replies['api/activity'] = { running: false, job: '10', result: 'Archived — 1 message(s) exported, 1 indexed.', error: '' };
    fresh(); await wait(5);
    note = doc.querySelectorAll('.archive-row')[1].querySelector('[data-made-note]');
    const unseen = await until(() => note.textContent.endsWith('Its first backup has ended, but another task ran after it, so how it ended is not shown here.'));
    check(failed && unseen, "a failed first backup is said as failed; another task's result is never said as its own", note.textContent);
    // A browser that keeps nothing for the tab: no reload, the first backup followed in place.
    storageOff = true; n = 0;
    replies['api/activity'] = () => (++n < 3 ? { running: true, job: '11', log: ['Reading Inbox…'] } : { running: false, job: '11', result: 'Archived — 2 message(s) exported, 2 indexed.', error: '' });
    doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null;
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps(); boot(); await wait(30);
    await toTypeBack();
    replies['api/encryption/new-recovery-key/confirm'] = { ok: true, lines: made, running: true, path: enc, job: '11' };
    const r1 = reloads;
    doc.getElementById('encUseKeyBtn').click();
    const inPlace = await until(() => doc.getElementById('encNote').textContent.endsWith('Its first backup finished: Archived — 2 message(s) exported, 2 indexed.'));
    await wait(40);
    storageOff = false;
    check(inPlace && reloads === r1 && doc.getElementById('encNote').textContent.startsWith(made.join('\n')), 'where the browser keeps nothing, the page is not drawn again and the first backup is followed in place', doc.getElementById('encNote').textContent);
  },
  // What Make a new encrypted archive… says before any step needs the person (MA-1073; the review's
  // MED-1: a folder refused after the wizard's windows was said 2,553 pixels below the window, so the
  // page again seemed to stop): the hint while the windows are open, a refusal, an add already in
  // progress and Cancel are said on a line beside the button, the keyboard left on it, nothing moved,
  // the steps card far below left hidden. Positive twin first: windows that answer take the line away
  // and the password step takes over.
  async createBeside() {
    const hint = 'Answer the questions in the windows that open on your computer. They may be behind this browser window: if they are not in view, move or minimize this window.';
    const fresh = () => { doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps(); boot(); };
    fresh(); await wait(30);
    let b = doc.querySelector('[data-create-encrypted]');
    const beside = () => { const x = b.nextElementSibling; return x && x.hasAttribute('data-beside-note') && !x.hidden ? x.textContent : ''; };
    const farHidden = () => doc.getElementById('sec-encsteps').hidden && doc.getElementById('encNote').hidden;
    replies['api/encryption/create'] = { ok: true, token: 'T1', path: 'D:\\Mail\\Enc', hint: '' };
    delays['api/encryption/create'] = 40;
    b.focus(); b.click(); await wait(10);
    const during = beside() === hint && b.nextElementSibling.getAttribute('role') === 'status' && farHidden();
    await wait(80);
    check(during && beside() === '' && !doc.getElementById('encCreateForm').hidden && doc.activeElement === doc.getElementById('encCreatePw'),
      'while the windows are open the hint is said beside the button; once they answer it goes and the password step takes over', JSON.stringify({ during, beside: beside() }));
    delete delays['api/encryption/create'];
    for (const [reply, said, name] of [
      [{ ok: false, error: 'Could not make an encrypted archive: an encrypted archive is made in a new or empty folder: D:\\Docs holds files; choose a new or empty folder.' },
        'Could not make an encrypted archive: an encrypted archive is made in a new or empty folder: D:\\Docs holds files; choose a new or empty folder.', 'a folder refused'],
      [{ ok: false, error: 'An add is already in progress — finish the steps on your computer, then try again.' }, 'An add is already in progress — finish the steps on your computer, then try again.', 'an add already in progress'],
      [{ ok: true, canceled: true }, 'Nothing was made.', 'Cancel in the windows']]) {
      fresh(); await wait(30);
      b = doc.querySelector('[data-create-encrypted]');
      replies['api/encryption/create'] = reply;
      b.focus(); const s0 = scrolled.length; b.click(); await wait(40);
      check(beside() === said && doc.activeElement === b && scrolled.length === s0 && farHidden(), name + ' is said beside the button, the keyboard still on it, nothing moved', JSON.stringify({ beside: beside(), s: scrolled.length - s0 }));
    }
  },
  // Switch to this archive, and Use this archive, on a locked encrypted archive (MA-1074; the review's
  // MED-2: the Unlock box was 1,200 to 2,070 pixels down, 32 to 34 Tab presses away): the page drawn
  // for it is at its Unlock box with the keyboard in it, once, as after Open an archive folder….
  // Positive twin first: a switch refused asks nothing of the page drawn next.
  async switchThenUnlock() {
    const key = 'mailarchive-step-unlock';
    const fresh = kind => { doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; page([['F:\\Enc', 'current']], 'F:\\Enc'); encLocked(kind); const s0 = scrolled.length; boot(); return s0; };
    const atUnlock = s0 => { const card = doc.getElementById('sec-encryption'); return scrolled.slice(s0).includes(card) && doc.activeElement === doc.getElementById('encSecret') && statusOf('sec-encryption') === stepWords.unlock && !(key in store); };
    page([['E:\\Cur', 'current'], ['F:\\Enc', 'other']], 'E:\\Cur');
    boot(); await wait(30);
    const sw = () => doc.querySelectorAll('.archive-row')[1].querySelector('[data-switch-archive]');
    replies['api/settings'] = { ok: false, error: 'That folder cannot be used.' };
    sw().click(); await wait(80);
    check(!(key in store) && reloads === 0, 'a switch refused asks nothing of the page drawn next, and draws none', JSON.stringify({ store, reloads }));
    replies['api/settings'] = { ok: true };
    sw().click(); await wait(80);
    const marked = store[key] === '1' && reloads >= 1;
    let s0 = fresh('locked'); await wait(30);
    check(marked && atUnlock(s0), 'after Switch to this archive, the page drawn for the locked archive is at its Unlock box, the keyboard in it, once', JSON.stringify({ marked, a: doc.activeElement && doc.activeElement.id, s: statusOf('sec-encryption') }));
    // Use this archive (the empty page's offer of the archive used last).
    doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null;
    page([], '');
    doc.body.append(el('div', { class: 'card', id: 'noArchive' }, el('button', { class: 'btn', type: 'button', id: 'useOffer', 'data-path': 'F:\\Enc' }, 'Use this archive')));
    boot(); await wait(30);
    const r0 = reloads;
    doc.getElementById('useOffer').click(); await wait(80);
    const marked2 = store[key] === '1' && reloads > r0;
    s0 = fresh('locked'); await wait(30);
    check(marked2 && atUnlock(s0), 'after Use this archive, the same', JSON.stringify({ marked2, a: doc.activeElement && doc.activeElement.id }));
  },
  // The recovery key and the keyboard (MA-1076): when a key appears the keyboard is on the key itself,
  // not on Save the recovery file (the review's LOW-4); Enter in the box it is typed back in uses it,
  // as Use this recovery key does (the owner's pick Q4); the recovery file's contents refused there
  // are taken out of the box, a typing mistake is left to fix (LOW-6); once a new recovery key takes
  // effect the keyboard is on the line saying so (LOW-6), the page not drawn again. Positive twin
  // first: the button still uses the key.
  async keyKeyboard() {
    const confirmPath = 'api/encryption/new-recovery-key/confirm', sent = () => posts.filter(p => p.path === confirmPath);
    page([['E:\\Cur', 'current']], 'E:\\Cur'); encSteps();
    doc.getElementById('sec-encryption').append(el('button', { class: 'btn secondary', type: 'button', id: 'encNewKeyBtn' }, 'Make a new recovery key'));
    boot(); await wait(30);
    replies['api/encryption/new-recovery-key'] = { ok: true, token: 'T2', recoveryKey: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2', warning: '' };
    doc.getElementById('encNewKeyBtn').click(); await wait(40);
    const k = doc.getElementById('encNewKey');
    check(doc.activeElement === k && k.textContent === 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2', 'when the key appears the keyboard is on the key itself, not on Save the recovery file', doc.activeElement && doc.activeElement.id);
    doc.getElementById('encSavedBtn').click(); await wait(20);
    const box = doc.getElementById('encTypedKey');
    replies[confirmPath] = { ok: false, error: 'That has a typing mistake.' };
    box.value = 'ABCX-EFGH-IJKL-MNOP-QRST-UVWX-YZ2';
    let n0 = sent().length;
    doc.getElementById('encUseKeyBtn').click(); await wait(40);
    const byButton = sent().length === n0 + 1 && sent()[n0].body.typed === 'ABCX-EFGH-IJKL-MNOP-QRST-UVWX-YZ2' && box.value === 'ABCX-EFGH-IJKL-MNOP-QRST-UVWX-YZ2';
    n0 = sent().length;
    const otherKey = box.press('a'); await wait(30);
    const quiet = sent().length === n0 && !otherKey;
    const stopped = box.press('Enter'); await wait(40);
    check(byButton && quiet && stopped && sent().length === n0 + 1 && sent()[n0].body.typed === 'ABCX-EFGH-IJKL-MNOP-QRST-UVWX-YZ2',
      'Enter in the type-back box uses the key, as the button does; other keys do not; a typing mistake is left in the box to fix', JSON.stringify({ byButton, quiet, stopped, n: sent().length - n0 }));
    replies[confirmPath] = { ok: false, error: "That is the recovery file's contents, not the recovery key.", clear: true };
    box.value = '{ "kind": "mailarchive-recovery-file" }';
    box.press('Enter'); await wait(40);
    check(box.value === '', "the recovery file's contents refused are taken out of the box", box.value);
    replies[confirmPath] = { ok: true, lines: ['The new recovery key is in effect.'] };
    box.value = 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ2';
    const r0 = reloads;
    box.press('Enter'); await wait(60);
    const note = doc.getElementById('encNote');
    check(doc.activeElement === note && lastScroll().el === note && note.textContent === 'The new recovery key is in effect.' && reloads === r0,
      'once the key takes effect the keyboard is on the line saying so, the page not drawn again', JSON.stringify({ a: doc.activeElement && doc.activeElement.id, reloads: reloads - r0 }));
  },
  // Two Delete… clicks, the first one's plan slower: only the last one's panel opens, and each
  // panel's label is its own box's (del-s3b-A1-01).
  async race() {
    page([['D:\\Mail\\MailArchive', 'current'], ['E:\\Old copy\\MailArchive', 'other']], 'D:\\Mail\\MailArchive');
    boot(); await wait(30);
    const [a, b] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = x => plan(x.path, 'MailArchive', x.path === a.dataset.path ? 'TOK-A' : 'TOK-B');
    delays['api/archive/delete/plan'] = x => (x.path === a.dataset.path ? 120 : 10);
    a.querySelector('[data-delete-archive]').click(); await wait(5);
    b.querySelector('[data-delete-archive]').click(); await wait(250);
    const panels = doc.querySelectorAll('.delete-panel');
    check(panels.length === 1 && panels[0].closest('li') === b, 'only the last Delete… clicked opens its panel', 'panels=' + panels.length);
    const lab = b.querySelector('.delete-panel label');
    const target = lab && doc.getElementById(lab.htmlFor);
    check(!!target && target.closest('li') === b, "the panel's label names its own box");
    check(doc.activeElement && doc.activeElement.closest('li') === b, 'the cursor is in the last panel opened');
    // Remove from list after a slow Delete…: only the question opens.
    replies['api/archive/unlist'] = { ok: true, ask: true, question: 'Remove from list? The folder and its files are kept.', named: [] };
    delays['api/archive/delete/plan'] = 120;
    b.querySelector('[data-delete-archive]').click(); await wait(5);
    a.querySelector('[data-unlist-archive]').click(); await wait(250);
    check(doc.querySelectorAll('.delete-panel').length === 0 && doc.querySelectorAll('.unlist-ask').length === 1, 'a slower plan does not open beside a later Remove from list');
    // Delete… after a slow Remove from list: only the panel opens.
    delays['api/archive/unlist'] = 120; delays['api/archive/delete/plan'] = 10;
    a.querySelector('[data-unlist-archive]').click(); await wait(5);
    b.querySelector('[data-delete-archive]').click(); await wait(250);
    check(doc.querySelectorAll('.unlist-ask').length === 0 && doc.querySelectorAll('.delete-panel').length === 1, 'a slower Remove from list does not open beside a later panel');
    const ids = doc.root.all().filter(e => e.tagName === 'INPUT' && /^delTyped/.test(e.id)).map(e => e.id);
    check(new Set(ids).size === ids.length, 'no two delete boxes share an id', ids.join(','));
  },
  // A Delete… opened on another row while a delete waits for its reply: the waiting delete keeps
  // its panel and its "Keep this page open" note, and its reply closes only its own panel, never
  // the one opened meanwhile (del-s3b leftover 1).
  async twoPanels() {
    page([['D:\\Mail\\A', 'other'], ['E:\\Mail\\B', 'other'], ['F:\\Cur', 'current']], 'F:\\Cur');
    boot(); await wait(30);
    const [a, b] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = x => plan(x.path, x.path === a.dataset.path ? 'A' : 'B', x.path === a.dataset.path ? 'TOK-A' : 'TOK-B');
    replies['api/archive/delete'] = result({ id: 'HA', row: 'D:\\Mail\\A', folder: 'D:\\Mail\\A', rowRemoved: true, rows: ['D:\\Mail\\A'] });
    delays['api/archive/delete'] = 200;
    a.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(a, 'A'); await wait(20);
    b.querySelector('[data-delete-archive]').click(); await wait(40);
    const keep = a.querySelector('.delete-panel [role=alert]');
    check(!!keep && !keep.hidden && /Keep this page open/.test(keep.textContent), "the waiting delete keeps its panel and its 'Keep this page open' note");
    const lab = b.querySelector('.delete-panel label');
    const target = lab && doc.getElementById(lab.htmlFor);
    check(!!target && target.closest('li') === b, "the other row's panel opens, its label naming its own box");
    await wait(300);
    check(!a.querySelector('.delete-panel') && !!doc.querySelector('[data-held-id="HA"]'), "the delete's reply closes its own panel and shows its result");
    check(!!b.querySelector('.delete-panel'), 'the panel opened meanwhile stays open');
    // A delete refused once it answered waits no more: the next panel opened closes its panel.
    replies['api/archive/delete'] = { ok: false, error: 'Could not write the log. Click Delete… again to try again.', planUsed: true };
    delays['api/archive/delete'] = 10;
    await typeAndDelete(b, 'B'); await wait(40);
    replies['api/archive/unlist'] = { ok: true, ask: true, question: 'Remove from list? The folder and its files are kept.', named: [] };
    const cur = doc.querySelectorAll('.archive-row')[2];
    cur.querySelector('[data-unlist-archive]').click(); await wait(40);
    check(!b.querySelector('.delete-panel') && !!cur.querySelector('.unlist-ask'), 'a refused delete\'s panel closes when another opens');
  },
  // A delete stopped at step 2 of a folder listed under two spellings, one the page's archive:
  // every spelling the server names is marked, and the page's Archive now and Run now go (A1-02).
  async alias() {
    page([['D:\\Mail\\Archive', 'other'], ['d:\\mail\\archive', 'current']], 'd:\\mail\\archive');
    boot(); await wait(30);
    const [upper, lower] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Archive', 'Archive', 'TOK');
    replies['api/archive/delete'] = result({ ok: false, id: 'H9', row: 'D:\\Mail\\Archive', folder: 'D:\\Mail\\Archive', status: 'not fully deleted', unfinished: true,
      rows: ['D:\\Mail\\Archive', 'd:\\mail\\archive'], page: 'unfinished', error: 'stopped' });
    upper.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(upper, 'Archive'); await wait(40);
    const del = posts.find(p => p.path === 'api/archive/delete');
    check(del && del.body.drawnFor === 'd:\\mail\\archive', 'the delete names the archive the page was drawn for');
    check(upper.hasAttribute('data-delete-unfinished') && lower.hasAttribute('data-delete-unfinished'), 'both spellings are marked not finished');
    check(!lower.querySelector('[data-pause-archive]') && !!lower.querySelector('[data-delete-archive]'), "the other spelling's Pause goes and its Delete… stays");
    check(!shown('archiveBtn') && !shown('runBackupBtn') && !shown('navArchive') && !shown('navRunNow'), "the page's Archive now, Run now and their sidebar links go");
    check(doc.root.all().some(e => e.textContent === 'Delete not finished: click Delete again' && e.className === 'archive-tag' && e.parent && e.parent.id === 'sec-archive'), 'Archive now says the delete is not finished in its place');
  },
  // A delete of the page's archive refused after its start (a backup took the lock): nothing was
  // removed, so the row and the page keep their buttons (A2-02).
  async refusedAfterStart() {
    page([['D:\\Mail\\Cur', 'current'], ['E:\\Other', 'other']], 'D:\\Mail\\Cur');
    boot(); await wait(30);
    const cur = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Cur', 'Cur', 'TOK');
    replies['api/archive/delete'] = result({ ok: false, id: 'H2', row: 'D:\\Mail\\Cur', folder: 'D:\\Mail\\Cur', status: 'refused after start',
      lines: ['A backup of this archive is running. Try again when it ends.'], rows: ['D:\\Mail\\Cur'], page: '', error: 'running' });
    cur.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(cur, 'Cur'); await wait(40);
    check(!!doc.querySelector('[data-held-id]'), 'the refused delete\'s result is shown');
    check(shown('archiveBtn') && shown('runBackupBtn') && shown('navRunNow'), 'Archive now and Run now stay: the archive is intact');
    check(!doc.root.all().some(e => /was deleted/.test(e.text)), 'nothing says the archive was deleted');
    check(!cur.hasAttribute('data-delete-unfinished') && !!cur.querySelector('[data-pause-archive]'), 'the row keeps Pause');
  },
  // A held result of a row removed, drawn at a page load where a new archive has the same
  // spelling: the new archive's row and the page's buttons are left as the server drew them (A1-04).
  async heldAtLoad() {
    const held = [result({ id: 'H3', row: 'D:\\Mail\\X', folder: 'D:\\Mail\\X', rowRemoved: true })];
    page([['D:\\Mail\\X', 'current'], ['E:\\Y', 'other']], 'D:\\Mail\\X', held);
    boot(); await wait(30);
    const x = doc.querySelectorAll('.archive-row')[0];
    const h = doc.querySelector('[data-held-id]');
    check(!!h && !h.closest('li'), 'the removed row\'s result is at the top, not inside a row');
    check(!!x.querySelector('[data-delete-archive]') && !!x.querySelector('[data-pause-archive]') && !x.classList.contains('unavailable'), "the new archive's row keeps its buttons");
    check(shown('archiveBtn') && shown('runBackupBtn'), "the page's Archive now and Run now stay");
  },
  // Reloads the page starts while a delete it asked for has not answered are held, so the reply
  // is not lost; the result then holds them until it is dismissed (A2-03).
  async inFlight() {
    page([['D:\\Mail\\Big', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const big = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Big', 'Big', 'TOK');
    replies['api/archive/delete'] = result({ id: 'H4', row: 'D:\\Mail\\Big', folder: 'D:\\Mail\\Big', rowRemoved: true, rows: ['D:\\Mail\\Big'] });
    delays['api/archive/delete'] = 200;
    big.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(big, 'Big'); await wait(20);
    ctx.reloadPage(); // a Switch's or a Pause's timed reload, while the delete runs
    await wait(20);
    check(reloads === 0, 'no reload while the delete has not answered', 'reloads=' + reloads);
    await wait(300);
    const h = doc.querySelector('[data-held-id]');
    check(!!h, 'the delete\'s result is shown when it answers');
    check(reloads === 0, 'the held result still holds the reload');
    check(!!h && !h.querySelector('[data-held-wait]').hidden, 'the result says the page refreshes when it is dismissed');
    // A refusal before the start, with a reload held meanwhile: the page is drawn again.
    replies['api/archive/delete'] = { ok: false, error: 'Could not write the log.', planUsed: true };
    delays['api/archive/delete'] = 100;
    h.remove(); // dismissed: no result holds the page now
    const other = doc.querySelectorAll('.archive-row')[1];
    replies['api/archive/delete/plan'] = plan('E:\\Cur', 'Cur', 'TOK2');
    other.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(other, 'Cur'); await wait(20);
    ctx.reloadPage(); await wait(20);
    check(reloads === 0, 'held while the second delete has not answered');
    await wait(200);
    check(reloads === 1, 'drawn again once it answered with nothing held', 'reloads=' + reloads);
  },
  // A row drawn as being deleted (a reload or another tab while it ran): the page asks until no
  // delete is under way, then is drawn again with the result (A2-03).
  async runningRow() {
    page([['D:\\Mail\\Big', 'running'], ['E:\\Cur', 'current']], 'E:\\Cur');
    let asked = 0;
    replies['api/activity'] = () => { asked++; return { running: false, deleting: asked < 3 }; };
    boot();
    await wait(400);
    check(asked >= 3, 'the page asks while the delete runs', 'asked=' + asked);
    check(reloads === 1, 'and is drawn again once it has ended', 'reloads=' + reloads);
  },
  // A result dismissed in another tab: this tab's Dismiss takes it away too and the page is drawn
  // again, and its reloads are no longer held (A2-05).
  async notHeld() {
    const held = [result({ id: 'H5', row: 'D:\\Gone', folder: 'D:\\Gone', rowRemoved: true })];
    page([['E:\\Cur', 'current']], 'E:\\Cur', held);
    store['mailarchive-deletion-record-saved-H5'] = '1';
    replies['api/archive/delete/dismiss'] = { ok: false, error: 'That result is no longer held: reload the page.', notHeld: true };
    boot(); await wait(30);
    const h = doc.querySelector('[data-held-id]');
    byText(h, 'Dismiss').click(); await wait(40);
    check(!doc.querySelector('[data-held-id]'), 'the result goes');
    check(reloads === 1, 'the page is drawn again', 'reloads=' + reloads);
    ctx.reloadPage();
    check(reloads === 2, 'a later reload is not held');
  },
  // A reload held while a result was held is kept when that result is closed for a new Delete…:
  // the page is drawn again rather than a plan opened on a page drawn for another archive (A2-06).
  async heldReload() {
    const held = [result({ id: 'H6', row: 'D:\\Mail\\Kept', folder: 'D:\\Mail\\Kept', status: 'not fully deleted', unfinished: true })];
    page([['D:\\Mail\\Kept', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur', held);
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Kept', 'Kept', 'TOK');
    boot(); await wait(30);
    ctx.reloadPage(); // a Switch saved while the result was held
    check(reloads === 0, 'the reload is held while the result is');
    const kept = doc.querySelectorAll('.archive-row')[0];
    kept.querySelector('[data-delete-archive]').click(); await wait(20);
    const h = doc.querySelector('[data-held-id]');
    check(!!h && !h.querySelector('.held-ask').hidden, 'Delete… asks first about the record not saved');
    h.querySelector('[data-held-close]').click(); await wait(60);
    check(reloads === 1, 'closing the last result draws the page again', 'reloads=' + reloads);
    check(!posts.some(p => p.path === 'api/archive/delete/plan'), 'no plan is asked for on the page being drawn again');
  },
  // A record saved is remembered across a reload of the tab: Dismiss then does not ask again
  // (§8.1; conformance gap 4). One not saved still asks.
  async savedMark() {
    const held = [result({ id: 'H7', row: 'D:\\A', folder: 'D:\\A', rowRemoved: true }), result({ id: 'H8', row: 'D:\\B', folder: 'D:\\B', rowRemoved: true })];
    page([['E:\\Cur', 'current']], 'E:\\Cur', held);
    boot(); await wait(30);
    const heldOf = id => doc.querySelector('[data-held-id="' + id + '"]');
    byText(heldOf('H7'), 'Save deletion record').click();
    check(store['mailarchive-deletion-record-saved-H7'] === '1', 'saving the record is remembered for this tab');
    // The tab is reloaded: a fresh page reads the mark.
    doc.body.children.slice().forEach(c => c.remove()); posts.length = 0; doc.activeElement = null;
    page([['E:\\Cur', 'current']], 'E:\\Cur', held);
    boot(); await wait(30);
    const a = heldOf('H7'), b = heldOf('H8');
    byText(a, 'Dismiss').click(); await wait(30);
    check(a.querySelector('.held-ask').hidden && posts.some(p => p.path === 'api/archive/delete/dismiss' && p.body.id === 'H7'), 'a saved record is dismissed without asking again');
    byText(b, 'Dismiss').click(); await wait(30);
    check(!b.querySelector('.held-ask').hidden && !posts.some(p => p.body && p.body.id === 'H8'), 'a record not saved is asked about first');
  },
  // A refusal that spent the plan's token leaves Delete permanently disabled; one that kept it (a
  // name that does not match) gives it back (conformance gap 3).
  async planUsed() {
    page([['D:\\Mail\\P', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const p = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\P', 'P', 'TOK');
    replies['api/archive/delete'] = { ok: false, error: 'The name you typed does not match.' };
    p.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(p, 'P'); await wait(40);
    check(!byText(p, 'Delete permanently').disabled, 'a refusal that kept the plan gives Delete permanently back');
    replies['api/archive/delete'] = { ok: false, error: 'Could not write the log. Click Delete… again to try again.', planUsed: true };
    byText(p, 'Delete permanently').click(); await wait(40);
    check(byText(p, 'Delete permanently').disabled, 'a refusal that spent the plan leaves it disabled');
    check(/Delete… again/.test(p.querySelector('.delete-panel [role=alert]').textContent), 'and says to click Delete… again');
  },
  // Archive now and Run now name the archive the page was drawn for (A1-03, A3-03).
  async drawnFor() {
    page([['E:\\Cur', 'current']], 'E:\\Cur');
    replies['api/capture'] = { ok: false, error: 'stale' };
    replies['api/run-backup'] = { ok: false, error: 'stale' };
    boot(); await wait(30);
    doc.getElementById('archiveBtn').click(); await wait(30);
    doc.getElementById('runBackupBtn').click(); await wait(30);
    for (const path of ['api/capture', 'api/run-backup']) {
      const p = posts.find(x => x.path === path);
      check(!!p && p.body && p.body.drawnFor === 'E:\\Cur', path + ' names the archive the page was drawn for');
    }
  },
  // The panel's words (MA-1019, B6, B7, B14): "1 file" and "N files", the kept files by name with
  // "and N more" past those the plan sent, and the folder's name in the prompt on one line.
  async panelWords() {
    page([['D:\\Mail\\Archive-Four', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const a = doc.querySelectorAll('.archive-row')[0];
    const names = Array.from({ length: 10 }, (_, i) => 'k' + (i + 1));
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\Archive-Four', 'Archive-Four', 'TOK'),
      { files: 1, kept: 12, keptNames: names, typeLine: "Type the folder's name, Archive-Four, to delete it (capital letters count)" });
    a.querySelector('[data-delete-archive]').click(); await wait(40);
    const said = t => a.querySelector('.delete-panel').all().some(e => e.tagName === 'P' && e.textContent === t);
    check(said('Delete removes 1 file (10 bytes).'), 'one file is "1 file"');
    check(said("Kept (not the archive's): " + names.join(', ') + ' and 2 more'), 'the kept files are named, and the rest counted');
    const lab = a.querySelector('.delete-panel label'), nm = lab && lab.querySelector('.del-name');
    check(!!nm && nm.textContent === 'Archive-Four' && lab.textContent === "Type the folder's name, Archive-Four, to delete it (capital letters count)",
      "the prompt keeps the folder's name in one unbreakable piece");
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\Archive-Four', 'Archive-Four', 'TOK2'), { files: 3, kept: 1, keptNames: ['my-notes.txt'] });
    a.querySelector('[data-delete-archive]').click(); await wait(40);
    check(said('Delete removes 3 files (10 bytes).') && said("Kept (not the archive's): my-notes.txt"), 'more files are "N files", and one kept file is named alone');
    // A folder whose name is also in the prompt's own words ("a" in "name"): the piece kept on
    // one line is the name itself, between its commas (the 1.5.3 friction fixes' adversarial pass, L2).
    doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null;
    page([['D:\\Mail\\a', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const sa = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\a', 'a', 'TOK3'), { typeLine: "Type the folder's name, a, to delete it (capital letters count)" });
    sa.querySelector('[data-delete-archive]').click(); await wait(40);
    const sl = sa.querySelector('.delete-panel label'), sn = sl && sl.querySelector('.del-name');
    check(!!sn && sn.textContent === 'a' && sl.children[0].text === "Type the folder's name, " && sl.textContent === "Type the folder's name, a, to delete it (capital letters count)",
      "a name that is also in the prompt's words is the piece kept on one line");
  },
  // The 1.5.3 friction review's wording items 50 and 57 (MA-1033): the plan names its sources as
  // "Mail folders: NAMES", never "Stores: store 1 …", and a delete's result is headed "Result for
  // FOLDER", never "Delete of FOLDER".
  async words153() {
    page([['D:\\Mail\\Gone', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const g = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\Gone', 'Gone', 'TOK'), { stores: [{ number: 1, name: 'Inbox' }, { number: 2, name: 'Sent' }] });
    g.querySelector('[data-delete-archive]').click(); await wait(40);
    const ps = g.querySelector('.delete-panel').all().filter(e => e.tagName === 'P').map(e => e.textContent);
    check(ps.includes('Mail folders: Inbox, Sent') && !ps.some(t => t.startsWith('Stores') || t.includes('store 1')), "the plan names its mail folders", JSON.stringify(ps));
    replies['api/archive/delete'] = result({ id: 'HG', row: 'D:\\Mail\\Gone', folder: 'D:\\Mail\\Gone', rowRemoved: true, rows: ['D:\\Mail\\Gone'] });
    await typeAndDelete(g, 'Gone'); await wait(40);
    const held = doc.querySelector('[data-held-id="HG"]'), b = held && held.querySelector('b');
    check(!!b && b.textContent === 'Result for D:\\Mail\\Gone' && !held.textContent.includes('Delete of'), 'the result is headed "Result for" its folder', b && b.textContent);
  },
  // The panel's mail folders by their labels (MA-1038, the 1.5.3 re-walk's N16): a store's label
  // names the mail folders inside it; a reply without one still names the store.
  async mailFolderLabels() {
    page([['D:\\Mail\\M', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const m = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\M', 'M', 'TOK'), { stores: [{ number: 1, name: 'mbox', label: 'mbox (Inbox, Sent)' }, { number: 2, name: 'Old' }] });
    m.querySelector('[data-delete-archive]').click(); await wait(40);
    const ps = m.querySelector('.delete-panel').all().filter(e => e.tagName === 'P').map(e => e.textContent);
    check(ps.includes('Mail folders: mbox (Inbox, Sent), Old'), 'the panel names the mail folders inside each store', JSON.stringify(ps));
  },
  // A re-run's panel warns of a file still held (MA-1039, the 1.5.3 re-walk's N17), right after the
  // line that it finishes a delete; a plan with none held says nothing of it.
  async heldLine() {
    page([['D:\\Mail\\H', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const h = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\H', 'H', 'TOK'), { finishesLine: 'This finishes the delete begun D.' });
    h.querySelector('[data-delete-archive]').click(); await wait(40);
    const said = () => h.querySelector('.delete-panel').all().filter(e => e.tagName === 'P').map(e => e.textContent);
    const none = !said().some(t => t.includes('still in use'));
    const warn = '1 file this delete must remove is still in use by another program: x.html. Close Outlook or any program using it before you delete, or this delete will stop at it again.';
    replies['api/archive/delete/plan'] = Object.assign(plan('D:\\Mail\\H', 'H', 'TOK2'), { finishesLine: 'This finishes the delete begun D.', heldLine: warn });
    h.querySelector('[data-delete-archive]').click(); await wait(40);
    const ps = said(), at = ps.indexOf(warn);
    check(none && at === ps.indexOf('This finishes the delete begun D.') + 1, 'the panel warns of the held file after its finishing line', JSON.stringify(ps));
  },
  // Several deletes not finished (MA-1041, the 1.5.3 re-walk's N9): the note keeps its lines, and
  // the disabled Remove's tooltip is the reply's own, plural; a reply without one keeps the
  // one-delete words.
  async uninstallWaitsMany() {
    page([['E:\\Cur', 'current']], 'E:\\Cur');
    doc.getElementById('sec-uninstall').append(el('button', { class: 'btn secondary', type: 'button', id: 'uninstallBtn' }, 'Uninstall MailArchive…'),
      el('div', { class: 'device-box', id: 'uninstallPanel', hidden: '' }, el('pre', { class: 'mono', id: 'uninstallPlan' }),
        el('div', {}, el('button', { class: 'btn', id: 'uninstallGo', type: 'button' }, 'Remove'), el('button', { class: 'btn secondary', id: 'uninstallCancel', type: 'button' }, 'Cancel'))),
      el('div', { class: 'note', id: 'uninstallNote', hidden: '' }));
    boot(); await wait(30);
    const planReply = extra => Object.assign({ ok: true, done: false, schedules: [], configFiles: [], deletionsLog: 'C:\\cfg\\deletions.log', deletionsLogNote: 'Copy deletions.log first.', keptArchives: [], clearedSignIn: false, binaryNote: '' }, extra);
    const go = doc.getElementById('uninstallGo'), note = doc.getElementById('uninstallNote'), open = doc.getElementById('uninstallBtn');
    replies['api/uninstall'] = planReply({ refuseUntil: 'Uninstall will refuse until the delete of E:\\Old is finished …' });
    open.click(); await wait(40);
    const one = go.disabled && go.title === 'Uninstall waits until that delete is finished.';
    const many = 'Uninstall will refuse until these 2 deletes are finished …\n  - E:\\A: mailarchive delete-archive -out "E:\\A"\n  - E:\\B: mailarchive delete-archive -out "E:\\B"\nWait for their results …';
    replies['api/uninstall'] = planReply({ refuseUntil: many, refuseWaits: 'Uninstall waits until those deletes are finished.' });
    open.click(); await wait(40);
    check(one && go.disabled && go.title === 'Uninstall waits until those deletes are finished.' && note.textContent === many, 'several deletes: the note keeps its lines and the tooltip is plural', go.title);
  },
  // Remove from list's words after its reload name the archive, are not said twice beside the row,
  // and give way to the next result (MA-1042, the 1.5.3 re-walk's N14). Positive twin first: a
  // reload with nothing removed shows no such note.
  async unlistNote() {
    page([['D:\\Mail\\Old', 'other'], ['D:\\Mail\\Next', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const twin = !doc.querySelector('[data-unlisted-note]');
    const [old] = doc.querySelectorAll('.archive-row');
    const said = 'Removed from Your archives. The folder and its files are kept.';
    replies['api/archive/unlist'] = x => (x.confirm ? { ok: true, lines: [said] } : { ok: true, ask: true, question: 'Remove from list? The folder and its files are kept.', named: [] });
    old.querySelector('[data-unlist-archive]').click(); await wait(40);
    byText(old, 'Yes').click(); await wait(40);
    check(twin && !old.all().some(e => e.className === 'archive-tag' && e.textContent === 'Removed from Your archives.'), 'the row does not say again what the note beside it says');
    doc.body.children.slice().forEach(c => c.remove());
    page([['D:\\Mail\\Next', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const n = doc.querySelector('[data-unlisted-note]');
    check(!!n && n.querySelector('b') && n.querySelector('b').textContent === 'Result for D:\\Mail\\Old', 'the note after the reload names the archive', n && n.textContent);
    const next = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Next', 'Next', 'TOK');
    replies['api/archive/delete'] = result({ id: 'HN', row: 'D:\\Mail\\Next', folder: 'D:\\Mail\\Next', rowRemoved: true, rows: ['D:\\Mail\\Next'] });
    next.append(el('span', { class: 'card-sub row-why', 'data-unlist-why': '' }, 'Remove from list is not offered for it now. Why.'));
    next.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(next, 'Next'); await wait(40);
    check(!doc.querySelector('[data-unlisted-note]') && !!doc.querySelector('[data-held-id="HN"]'), 'the next result takes its place');
    check(!next.querySelector('[data-unlist-why]'), "a removed row's Remove from list reason goes with its button");
  },
  // A row this page marks "Delete not finished" drops its Remove from list reason, as the server
  // draws such a row (MA-1042, N13; the settle pass's LOW-6). Positive twin first: another row's
  // reason, whose delete was not asked, stays.
  async unfinishedDropsWhy() {
    page([['D:\\Mail\\Half', 'other'], ['D:\\Mail\\Keep', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const [half, keep] = doc.querySelectorAll('.archive-row');
    [half, keep].forEach(li => li.append(el('span', { class: 'card-sub row-why', 'data-unlist-why': '' }, 'Remove from list is not offered for it now. Why.')));
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Half', 'Half', 'TOK');
    replies['api/archive/delete'] = result({ id: 'HH', row: 'D:\\Mail\\Half', folder: 'D:\\Mail\\Half', unfinished: true, rows: ['D:\\Mail\\Half'] });
    half.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(half, 'Half'); await wait(40);
    const marked = half.hasAttribute('data-delete-unfinished');
    check(!!keep.querySelector('[data-unlist-why]') && marked && !half.querySelector('[data-unlist-why]'),
      'a row marked Delete not finished drops its Remove from list reason; another row keeps its own',
      JSON.stringify({ keep: !!keep.querySelector('[data-unlist-why]'), marked, half: !!half.querySelector('[data-unlist-why]') }));
  },
  // Dismissing the result of a delete of the page's own archive draws the page again from its top
  // (MA-1042, N12). Positive twin first: dismissing another archive's result does not.
  async dismissToTop() {
    page([['D:\\Mail\\Cur', 'current'], ['E:\\Other', 'other']], 'D:\\Mail\\Cur');
    boot(); await wait(30);
    const [cur, other] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = x => plan(x.path, x.path === 'E:\\Other' ? 'Other' : 'Cur', 'TOK-' + x.path);
    replies['api/archive/delete'] = result({ id: 'HO', row: 'E:\\Other', folder: 'E:\\Other', rowRemoved: true, rows: ['E:\\Other'], page: '' });
    other.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(other, 'Other'); await wait(40);
    store['mailarchive-deletion-record-saved-HO'] = '1'; doc.querySelector('[data-held-id="HO"]').dataset.saved = '1';
    byText(doc.querySelector('[data-held-id="HO"]'), 'Dismiss').click(); await wait(40);
    const twin = reloads === 1 && !('mailarchive-scroll-top' in store);
    replies['api/archive/delete'] = result({ id: 'HC', row: 'D:\\Mail\\Cur', folder: 'D:\\Mail\\Cur', rowRemoved: true, rows: ['D:\\Mail\\Cur'], page: 'deleted' });
    cur.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(cur, 'Cur'); await wait(40);
    doc.querySelector('[data-held-id="HC"]').dataset.saved = '1';
    byText(doc.querySelector('[data-held-id="HC"]'), 'Dismiss').click(); await wait(40);
    const flagged = reloads === 2 && store['mailarchive-scroll-top'] === '1';
    // The page drawn again (the empty page) starts at its top, once.
    doc.body.children.slice().forEach(c => c.remove());
    page([['E:\\Other2', 'other']], '');
    boot(); await wait(30);
    check(twin && flagged && scrolledTop.length === 1 && scrolledTop[0][1] === 0 && !('mailarchive-scroll-top' in store), 'dismissing the deleted archive\'s result draws the page again from its top',
      JSON.stringify({ twin, flagged, scrolledTop }));
  },
  // After a reload the person started with Dismiss or Remove from list, the page drawn again is at
  // Your archives, its heading focused (MA-1065; the 1.5.6 friction review's note B: the browser left
  // it at the Microsoft 365 card). Positive twin first: a page drawn with nothing asked is not moved.
  // The result of a delete of the page's own archive still draws the page again from its top (N12),
  // not at Your archives.
  async backToArchives() {
    const key = 'mailarchive-scroll-archives';
    const fresh = rows => { doc.body.children.slice().forEach(c => c.remove()); doc.activeElement = null; page(rows, 'D:\\Mail\\Cur'); const s0 = scrolled.length; boot(); return s0; };
    const atArchives = s0 => { const card = doc.getElementById('sec-archives'), h2 = card && card.querySelector('h2');
      return !!h2 && scrolled.slice(s0).includes(card) && doc.activeElement === h2 && h2.getAttribute('tabindex') === '-1' && !(key in store); };
    page([['D:\\Mail\\Cur', 'current'], ['E:\\Other', 'other'], ['F:\\Old', 'other']], 'D:\\Mail\\Cur');
    const s0 = scrolled.length; boot(); await wait(30);
    check(scrolled.length === s0 && doc.activeElement === null && !(key in store), 'a page drawn with nothing asked is not moved');
    const other = doc.querySelectorAll('.archive-row')[1];
    replies['api/archive/delete/plan'] = x => plan(x.path, x.path === 'E:\\Other' ? 'Other' : 'Cur', 'TOK-' + x.path);
    replies['api/archive/delete'] = result({ id: 'HO', row: 'E:\\Other', folder: 'E:\\Other', rowRemoved: true, rows: ['E:\\Other'], page: '' });
    other.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(other, 'Other'); await wait(40);
    doc.querySelector('[data-held-id="HO"]').dataset.saved = '1';
    byText(doc.querySelector('[data-held-id="HO"]'), 'Dismiss').click(); await wait(40);
    const asked = reloads === 1 && store[key] === '1' && !('mailarchive-scroll-top' in store);
    let s1 = fresh([['D:\\Mail\\Cur', 'current'], ['F:\\Old', 'other']]); await wait(30);
    check(asked && atArchives(s1), 'after Dismiss, the page drawn again is at Your archives, its heading focused', JSON.stringify({ asked, reloads, store }));
    const old = doc.querySelectorAll('.archive-row')[1];
    replies['api/archive/unlist'] = x => (x.confirm ? { ok: true, lines: ['Removed from Your archives. The folder and its files are kept.'] } : { ok: true, ask: true, question: 'Remove from list? The folder and its files are kept.', named: [] });
    old.querySelector('[data-unlist-archive]').click(); await wait(40);
    byText(old, 'Yes').click(); await wait(80);
    const asked2 = reloads === 2 && store[key] === '1';
    s1 = fresh([['D:\\Mail\\Cur', 'current']]); await wait(30);
    check(asked2 && atArchives(s1), 'after Remove from list, the page drawn again is at Your archives, its heading focused', JSON.stringify({ asked2, reloads }));
    const cur = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete'] = result({ id: 'HC', row: 'D:\\Mail\\Cur', folder: 'D:\\Mail\\Cur', rowRemoved: true, rows: ['D:\\Mail\\Cur'], page: 'deleted' });
    cur.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(cur, 'Cur'); await wait(40);
    doc.querySelector('[data-held-id="HC"]').dataset.saved = '1';
    byText(doc.querySelector('[data-held-id="HC"]'), 'Dismiss').click(); await wait(40);
    check(reloads === 3 && store['mailarchive-scroll-top'] === '1' && !(key in store), "the page's own archive deleted still starts at the top, not at Your archives", JSON.stringify(store));
  },
  // The question asked on Dismiss of a result whose record was not saved opens with the cursor on
  // Keep it (MA-1069; the 1.5.6 friction review's note A: it sat on Close without saving, so Enter
  // twice lost the record). Enter there keeps the result and moves to Save deletion record, so Enter
  // twice saves the record. Positive twin first: Close without saving, clicked, still closes a result
  // without its record.
  async keepItFocused() {
    const held = [result({ id: 'HK', row: 'D:\\Gone', folder: 'D:\\Gone', rowRemoved: true }), result({ id: 'HL', row: 'D:\\Gone2', folder: 'D:\\Gone2', rowRemoved: true })];
    page([['E:\\Cur', 'current']], 'E:\\Cur', held);
    boot(); await wait(30);
    const heldOf = id => doc.querySelector('[data-held-id="' + id + '"]');
    const twin = heldOf('HL');
    byText(twin, 'Dismiss').click(); await wait(20);
    twin.querySelector('[data-held-close]').click(); await wait(40);
    check(posts.some(p => p.path === 'api/archive/delete/dismiss' && p.body.id === 'HL') && store['mailarchive-deletion-record-saved-HL'] === undefined,
      'Close without saving, clicked, still closes a result without its record');
    const h = heldOf('HK');
    byText(h, 'Dismiss').click(); await wait(20);
    const keep = h.querySelector('[data-held-keep]');
    check(!h.querySelector('.held-ask').hidden && doc.activeElement === keep, 'the question opens with the cursor on Keep it',
      doc.activeElement && doc.activeElement.textContent);
    doc.activeElement.click(); await wait(20); // Enter
    doc.activeElement.click(); await wait(20); // Enter again
    check(h.querySelector('.held-ask').hidden && store['mailarchive-deletion-record-saved-HK'] === '1' && !posts.some(p => p.body && p.body.id === 'HK') && !!heldOf('HK'),
      'Enter twice keeps the result and saves its record', JSON.stringify({ active: doc.activeElement && doc.activeElement.textContent, store }));
  },
  // Save deletion record says on screen what it asked of the browser (MA-1042, N20), as Save this
  // plan does. Positive twin first: nothing is said before it is clicked.
  async saveRecordLine() {
    const held = [result({ id: 'HS', row: 'D:\\Gone', folder: 'D:\\Gone', rowRemoved: true, recordName: 'MailArchive deletion record - Gone - 2026-11-02.txt' })];
    page([['E:\\Cur', 'current']], 'E:\\Cur', held);
    boot(); await wait(30);
    const h = doc.querySelector('[data-held-id="HS"]');
    const st = h.querySelector('.del-actions [role=status]');
    const before = !!st && st.hidden;
    byText(h, 'Save deletion record').click(); await wait(10);
    check(before && !st.hidden && st.textContent === 'Your browser was asked to save the deletion record as MailArchive deletion record - Gone - 2026-11-02.txt (usually in Downloads).',
      'Save deletion record says what it asked of the browser', st && st.textContent);
  },
  // The page's layout fixes (MA-1020, B13, B14): Save this plan says so on screen and sends
  // nothing; a delete's result is brought into view; Remove from list's words are said again at
  // the top of Your archives after the reload it starts, once.
  async pageLayout() {
    page([['D:\\Mail\\Old', 'other'], ['D:\\Mail\\Gone', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const [old, gone] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Gone', 'Gone', 'TOK');
    gone.querySelector('[data-delete-archive]').click(); await wait(40);
    const before = posts.length;
    byText(gone, 'Save this plan').click(); await wait(10);
    const st = gone.querySelector('.delete-panel [role=status]');
    check(!!st && !st.hidden && st.textContent === 'Your browser was asked to save the plan as plan.txt (usually in Downloads). Nothing was deleted.' && posts.length === before,
      'Save this plan says the browser was asked to save it, and sends nothing');
    replies['api/archive/delete'] = result({ id: 'HG', row: 'D:\\Mail\\Gone', folder: 'D:\\Mail\\Gone', rowRemoved: true, rows: ['D:\\Mail\\Gone'] });
    await typeAndDelete(gone, 'Gone'); await wait(40);
    check(scrolled.length === 1 && scrolled[0] === doc.querySelector('[data-held-id="HG"]'), 'the result is brought into view');
    const said = 'Removed from Your archives. The folder and its files are kept.';
    replies['api/archive/unlist'] = x => (x.confirm ? { ok: true, lines: [said] } : { ok: true, ask: true, question: 'Remove from list? The folder and its files are kept.', named: [] });
    old.querySelector('[data-unlist-archive]').click(); await wait(40);
    byText(old, 'Yes').click(); await wait(60);
    // The tab is reloaded: a fresh page reads what was said, once.
    doc.body.children.slice().forEach(c => c.remove());
    page([['E:\\Cur', 'current'], ['F:\\Other', 'other']], 'E:\\Cur');
    boot(); await wait(30);
    const n = doc.querySelector('[data-unlisted-note]');
    // Headed by its folder (MA-1042, the 1.5.3 re-walk's N14), then the words exactly.
    const first = n && n.parent && n.parent.id === 'sec-archives' && n.textContent === 'Result for D:\\Mail\\Old' + said;
    doc.body.children.slice().forEach(c => c.remove());
    page([['E:\\Cur', 'current'], ['F:\\Other', 'other']], 'E:\\Cur');
    boot(); await wait(30);
    check(first && !doc.querySelector('[data-unlisted-note]'), "Remove from list's words are said at the top of Your archives after its reload, once");
  },
  // A delete of the page's own archive that got past step 4: the page says so at the top, and
  // its cards are greyed and inert, each saying so, until the result is dismissed; the switch
  // note about it goes, and the sidebar's reader and Encryption links (MA-1017). Positive twin
  // first: a delete of another archive leaves them all.
  async deletedCurrent() {
    const cards = ['sec-archive', 'sec-backup', 'sec-settings', 'sec-encryption', 'sec-health'];
    page([['D:\\Mail\\Cur', 'current'], ['E:\\Other', 'other']], 'D:\\Mail\\Cur');
    boot(); await wait(30);
    const [cur, other] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = x => plan(x.path, x.path === 'E:\\Other' ? 'Other' : 'Cur', 'TOK-' + x.path);
    replies['api/archive/delete'] = result({ id: 'HO', row: 'E:\\Other', folder: 'E:\\Other', rowRemoved: true, rows: ['E:\\Other'], page: '' });
    other.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(other, 'Other'); await wait(40);
    check(!doc.root.all().some(e => e.hasAttribute('inert')) && !doc.getElementById('archiveGone') && !!doc.getElementById('switchLocks') && shown('archiveBtn'),
      "a delete of another archive leaves the page's own controls");
    replies['api/archive/delete'] = result({ id: 'HC', row: 'D:\\Mail\\Cur', folder: 'D:\\Mail\\Cur', rowRemoved: true, rows: ['D:\\Mail\\Cur'], page: 'deleted' });
    cur.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(cur, 'Cur'); await wait(40);
    const said = 'This archive was deleted: click Dismiss on its result to refresh this page.';
    const top = doc.getElementById('archiveGone');
    check(!!top && top.textContent === said && top.getAttribute('role') === 'status', 'the top of the page says the archive was deleted and to dismiss its result');
    check(cards.every(id => { const c = doc.getElementById(id); return c.hasAttribute('inert') && c.classList.contains('archive-gone'); }) &&
      doc.querySelector('.status-grid').hasAttribute('inert'), 'its cards and status tiles are greyed and do nothing');
    check(cards.every(id => { const t = doc.getElementById(id).querySelector('.gone-tag'); return !!t && t.textContent === said; }), 'each card says so at its top');
    check(!doc.getElementById('switchLocks'), 'the switch note about the deleted archive is cleared');
    check(doc.querySelectorAll('.nav a[target]').every(a => a.hidden) && doc.getElementById('navEncryption').hidden && !doc.querySelector('.nav a[href="#sec-health"]').hidden,
      "the sidebar's reader and Encryption links go; the others stay");
    check(!doc.getElementById('sec-archives').hasAttribute('inert') && !doc.getElementById('sec-uninstall').hasAttribute('inert') && !!doc.querySelector('[data-held-id="HC"]'),
      'Your archives, its result and Uninstall stay usable');
  },
  // A delete of the page's own archive not finished after step 4 cleared it (the reply's page
  // "partial", the 1.5.3 friction fixes' adversarial pass H1): its cards are greyed as for
  // "deleted", in "not fully deleted" words, and its row says the delete is not finished.
  // Positive twin first: "unfinished" (a stop at step 2, still the archive in use) greys nothing.
  async partialCurrent() {
    const cards = ['sec-archive', 'sec-backup', 'sec-settings', 'sec-encryption', 'sec-health'];
    page([['D:\\Mail\\Cur', 'current'], ['E:\\Other', 'other']], 'D:\\Mail\\Cur');
    boot(); await wait(30);
    const cur = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Cur', 'Cur', 'TOK');
    replies['api/archive/delete'] = result({ ok: false, id: 'HU', row: 'D:\\Mail\\Cur', folder: 'D:\\Mail\\Cur', status: 'not fully deleted', unfinished: true,
      rows: ['D:\\Mail\\Cur'], page: 'unfinished', error: 'stopped' });
    cur.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(cur, 'Cur'); await wait(40);
    check(!doc.root.all().some(e => e.hasAttribute('inert')) && !doc.getElementById('archiveGone') && !!doc.getElementById('switchLocks'),
      'a delete stopped at step 2 greys none of the page');
    // A fresh page drawn for the same archive, as before the delete that goes past step 4.
    doc.body.children.slice().forEach(c => c.remove());
    page([['D:\\Mail\\Cur', 'current'], ['E:\\Other', 'other']], 'D:\\Mail\\Cur');
    boot(); await wait(30);
    const cur2 = doc.querySelectorAll('.archive-row')[0];
    replies['api/archive/delete/plan'] = plan('D:\\Mail\\Cur', 'Cur', 'TOK2');
    replies['api/archive/delete'] = result({ ok: false, id: 'HP', row: 'D:\\Mail\\Cur', folder: 'D:\\Mail\\Cur', status: 'not fully deleted', unfinished: true,
      rows: ['D:\\Mail\\Cur'], page: 'partial', error: 'stopped' });
    cur2.querySelector('[data-delete-archive]').click(); await wait(40);
    await typeAndDelete(cur2, 'Cur'); await wait(40);
    const said = 'This archive was not fully deleted: click Dismiss on its result to refresh this page.';
    const top = doc.getElementById('archiveGone');
    check(!!top && top.textContent === said, 'the top of the page says the archive was not fully deleted');
    check(cards.every(id => { const c = doc.getElementById(id); const t = c.querySelector('.gone-tag'); return c.hasAttribute('inert') && c.classList.contains('archive-gone') && !!t && t.textContent === said; }) &&
      doc.querySelector('.status-grid').hasAttribute('inert'), 'its cards and status tiles are greyed, do nothing and say so');
    check(cur2.hasAttribute('data-delete-unfinished') && !doc.getElementById('switchLocks'), 'its row says the delete is not finished, and the switch note goes');
  },
  // Open an archive folder… on a folder holding the delete marker (MA-1052, DM-17): the server lists
  // it itself and answers its words with no path; the page shows them, never sends the save that
  // would switch to it, keeps the words across the page drawn again, and draws it again. Positive
  // twin first: an ordinary folder is answered with its path, which the page saves.
  async openMarker() {
    page([['E:\\Cur', 'current']], 'E:\\Cur');
    doc.body.append(el('div', { class: 'card', id: 'sec-open' }, el('button', { class: 'btn', type: 'button', 'data-open-archive': '' }, 'Open an archive folder…')));
    replies['api/open-archive'] = { ok: true, path: 'F:\\Ordinary' };
    replies['api/settings'] = { ok: true };
    boot(); await wait(30);
    const btn = doc.querySelector('[data-open-archive]');
    btn.click(); await wait(60);
    check(posts.some(p => p.path === 'api/settings' && p.body.out === 'F:\\Ordinary'), 'an ordinary folder is saved as the current archive');
    posts.length = 0; const before = reloads;
    btn.disabled = false; // the page a browser draws again gives the button back
    const said = 'Added to Your archives: G:\\Half. A delete of this archive did not finish: to finish it, click Delete… on its row.';
    replies['api/open-archive'] = { ok: true, added: true, note: said };
    btn.click(); await wait(60);
    const n = doc.getElementById('settingsNote');
    check(!posts.some(p => p.path === 'api/settings'), 'a marker folder is never sent to the save');
    check(!!n && !n.hidden && n.textContent === said, "the server's words are shown", n && n.textContent);
    check(store['mailarchive.settingsNote'] === said && reloads === before + 1, 'they are kept across the page drawn again, which shows its row', JSON.stringify({ kept: store['mailarchive.settingsNote'], reloads }));
  },
  // Delete… of a folder gone whose delete this computer began (MA-1057, §15.8 question 2): the plan
  // route asks whether it was finished elsewhere; the question shows in the row with its two answers
  // and no panel; Cancel opens nothing and says nothing was changed; "It was finished elsewhere"
  // asks for the plan again saying so, and the panel opens with the plan's line saying it. Positive
  // twin first: an ordinary plan opens its panel at once.
  async elsewhere() {
    page([['D:\\Mail\\Gone', 'other'], ['E:\\Cur', 'current']], 'E:\\Cur');
    boot(); await wait(30);
    const [gone, cur] = doc.querySelectorAll('.archive-row');
    replies['api/archive/delete/plan'] = x => plan(x.path, 'Cur', 'TOK0');
    cur.querySelector('[data-delete-archive]').click(); await wait(40);
    check(!!cur.querySelector('.delete-panel'), 'an ordinary plan opens its panel');
    const q = 'This archive\'s folder is not there. If its delete was finished from another computer or user account, MailArchive can finish it here too: it stops this computer\'s schedules for it and removes its row. If the folder is on a drive that is not plugged in, plug it in instead.';
    const said = 'The folder was not found here; you said this delete was finished elsewhere.';
    replies['api/archive/delete/plan'] = x => (x.elsewhere ? Object.assign(plan(x.path, 'Gone', 'TOK1'), { finishesLine: 'This finishes the delete begun D.', finishesNotes: [said] })
      : { ok: false, elsewhere: true, error: q, yes: 'It was finished elsewhere', no: 'Cancel' });
    gone.querySelector('[data-delete-archive]').click(); await wait(40);
    const ask = gone.querySelector('.elsewhere-ask');
    check(!!ask && ask.querySelector('p').textContent === q && !!byText(ask, 'It was finished elsewhere') && !!byText(ask, 'Cancel') && !gone.querySelector('.delete-panel'),
      'the question shows in the row with its two answers and no panel');
    byText(ask, 'Cancel').click(); await wait(20);
    const n = doc.getElementById('settingsNote');
    check(!gone.querySelector('.elsewhere-ask') && !gone.querySelector('.delete-panel') && !!n && n.textContent === 'Nothing was changed.', 'Cancel opens nothing and says nothing was changed');
    gone.querySelector('[data-delete-archive]').click(); await wait(40);
    byText(gone.querySelector('.elsewhere-ask'), 'It was finished elsewhere').click(); await wait(60);
    const asked = posts.filter(p => p.path === 'api/archive/delete/plan' && p.body.path === 'D:\\Mail\\Gone');
    const ps = gone.querySelector('.delete-panel') ? gone.querySelector('.delete-panel').all().filter(e => e.tagName === 'P').map(e => e.textContent) : [];
    check(asked.length === 3 && asked[2].body.elsewhere === true && !asked[0].body.elsewhere && ps.indexOf(said) === ps.indexOf('This finishes the delete begun D.') + 1 && !gone.querySelector('.elsewhere-ask'),
      'the answer asks for the plan again saying so, and the panel says it', JSON.stringify(ps));
  },
  // The Uninstall panel with a delete not finished (MA-1030 amended; the uninstall-docs group's
  // adversarial pass, finding 2): the plan is shown, then why Remove waits, never "removed
  // nothing", and Remove is disabled and sends nothing; a plan with nothing waiting gives it back.
  // Positive twin first: a plan with nothing waiting leaves Remove enabled and no note.
  async uninstallWaits() {
    page([['E:\\Cur', 'current']], 'E:\\Cur');
    doc.getElementById('sec-uninstall').append(el('button', { class: 'btn secondary', type: 'button', id: 'uninstallBtn' }, 'Uninstall MailArchive…'),
      el('div', { class: 'device-box', id: 'uninstallPanel', hidden: '' }, el('pre', { class: 'mono', id: 'uninstallPlan' }),
        el('div', {}, el('button', { class: 'btn', id: 'uninstallGo', type: 'button' }, 'Remove'), el('button', { class: 'btn secondary', id: 'uninstallCancel', type: 'button' }, 'Cancel'))),
      el('div', { class: 'note', id: 'uninstallNote', hidden: '' }));
    boot(); await wait(30);
    const until = 'Uninstall will refuse until the delete of E:\\Old is finished (it is running or did not finish): …';
    const planReply = extra => Object.assign({ ok: true, done: false, schedules: [], configFiles: ['C:\\cfg\\desktop-settings.json', 'C:\\cfg\\deletions.log'],
      deletionsLog: 'C:\\cfg\\deletions.log', deletionsLogNote: 'Copy deletions.log first.', keptArchives: ['E:\\Cur'], clearedSignIn: false, binaryNote: '' }, extra);
    const go = doc.getElementById('uninstallGo'), note = doc.getElementById('uninstallNote'), open = doc.getElementById('uninstallBtn');
    replies['api/uninstall'] = planReply({});
    open.click(); await wait(40);
    check(shown('uninstallPanel') && !go.disabled && note.hidden, 'a plan with nothing waiting leaves Remove enabled and no note');
    replies['api/uninstall'] = planReply({ refuseUntil: until });
    open.click(); await wait(40);
    check(shown('uninstallPanel') && doc.getElementById('uninstallPlan').textContent.includes('- the deletion log: C:\\cfg\\deletions.log'), 'the plan is shown');
    check(!note.hidden && note.textContent === until, 'then why Remove waits, never "removed nothing"', note.textContent);
    check(go.disabled, 'Remove is disabled');
    const before = posts.filter(p => p.path === 'api/uninstall').length;
    go.click(); await wait(20);
    check(posts.filter(p => p.path === 'api/uninstall').length === before, 'a click on Remove sends nothing');
    replies['api/uninstall'] = planReply({});
    open.click(); await wait(40);
    check(!go.disabled && note.hidden, 'a plan with nothing waiting gives Remove back');
  },
  // Restore to Thunderbird on a plain archive (MA-1131; design-restore-thunderbird §7.3): each step
  // brought into view with showStep, the keyboard on its first control, its status line saying what
  // is needed: the profiles (the pre-selected one ticked, one that cannot be used shown with why and
  // not offered); what to restore, and the tree asked for by the list ID and position, never a path;
  // a refusal said in place; the plan's lines in the engine's order; the run with W-WAIT for the
  // whole of it, its progress and "Checking what was written…"; the result in the engine's words
  // and Save this record. Positive twin first: a page with no restore running opens nothing.
  async restoreFlow() {
    page([['E:\\Cur', 'current']], 'E:\\Cur'); restoreCard();
    replies['api/activity'] = { running: false, job: '0' };
    boot(); await wait(30);
    check(scrolled.length === 0 && doc.getElementById('rtbPanel').hidden, 'a page with no restore running opens nothing');
    replies['api/restore-tb/profiles'] = { ok: true, listID: 'L1', default: 1, profiles: [
      { position: 0, name: 'work', dir: 'C:\\TB\\work', usable: true, unusable: '', opensByDefault: true, backedUp: true },
      { position: 1, name: 'home', dir: 'C:\\TB\\home', usable: true, unusable: '', opensByDefault: false, backedUp: false },
      { position: 2, name: 'odd', dir: 'C:\\TB\\odd', usable: false, unusable: 'This profile cannot be used.', opensByDefault: false, backedUp: false }] };
    doc.getElementById('rtbBtn').click(); await wait(40);
    const ps = doc.getElementById('rtbProfileStep'), p1 = doc.getElementById('rtbProfile1');
    check(!ps.hidden && lastScroll().el === ps && doc.activeElement === p1 && p1.checked && doc.getElementById('rtbProfile2').disabled && statusOf('rtbProfileStep') === rtbWords.profile,
      'step 1: the profiles, the pre-selected one ticked with the keyboard on it, one that cannot be used not offered', JSON.stringify({ a: doc.activeElement && doc.activeElement.id, s: statusOf('rtbProfileStep') }));
    check(ps.textContent.includes('This profile cannot be used.') && ps.textContent.includes('work — C:\\TB\\work (Thunderbird opens it by default) (this archive backs it up)'),
      'why one cannot be used, and which one the archive backs up, are said', ps.textContent);
    doc.getElementById('rtbNext').click(); await wait(10);
    const ws = doc.getElementById('rtbWhatStep');
    check(!ws.hidden && ps.hidden && lastScroll().el === ws && doc.activeElement === doc.getElementById('rtbWhole') && statusOf('rtbWhatStep') === rtbWords.what,
      'step 2: what to restore, the keyboard on The whole archive', statusOf('rtbWhatStep'));
    replies['api/restore-tb/folders'] = { ok: true, total: 4, folders: [{ id: 'm', parent: '', name: 'Mail', store: true, count: 0, total: 4 },
      { id: 'm/Z', parent: 'm', name: 'Zebrafolder', store: false, count: 2, total: 3 }, { id: 'm/Z/Q', parent: 'm/Z', name: 'Quokkasub', store: false, count: 1, total: 1 },
      { id: 'm/S', parent: 'm', name: 'Sentbox', store: false, count: 1, total: 1 }] };
    const ch = doc.getElementById('rtbChoose'); ch.checked = true; ch.fire('change'); await wait(40);
    const boxes = doc.querySelectorAll('[data-rtb-folder]'), fp = posts.filter(p => p.path === 'api/restore-tb/folders');
    check(boxes.length === 4 && doc.activeElement === boxes[0] && fp.length === 1 && fp[0].body.listID === 'L1' && fp[0].body.position === 1 &&
      doc.getElementById('rtbTree').textContent.includes('Zebrafolder (3)') && statusOf('rtbWhatStep') === rtbWords.tree,
      "choose folders: the tree with each folder's count, asked by the list ID and position, the keyboard on its first box", JSON.stringify(fp.map(p => p.body)));
    boxes[1].checked = true;
    const refusal = 'Thunderbird is open, so nothing was restored. Close Thunderbird (on Windows: File → Exit; on a Mac: Thunderbird → Quit Thunderbird), then click Check again.';
    replies['api/restore-tb/plan'] = { ok: false, error: refusal };
    const ck = doc.getElementById('rtbCheck'); ck.click(); await wait(40);
    check(doc.activeElement === ck && statusOf('rtbWhatStep') === refusal && doc.getElementById('rtbPlanStep').hidden, 'a refusal is said in place, the keyboard on Check');
    replies['api/restore-tb/plan'] = { ok: true, token: 'T1', encrypted: false, askTwice: false, lines: [
      { code: 'W-RECAPTURE', words: 'This archive backs up this Thunderbird profile.' }, { code: 'DEST', words: "Restore into the Thunderbird profile 'home'." }, { code: 'W-WAIT', words: rtbWords.wait }] };
    ck.click(); await wait(40);
    const pp = posts.filter(p => p.path === 'api/restore-tb/plan').pop(), lis = doc.getElementById('rtbPlan').querySelectorAll('li');
    check(pp.body.whole === false && pp.body.folders.join() === 'm/Z' && pp.body.position === 1 && lis.length === 3 && lis[0].getAttribute('data-code') === 'W-RECAPTURE' && lis[2].textContent === rtbWords.wait &&
      lastScroll().el === doc.getElementById('rtbPlanStep') && doc.activeElement === doc.getElementById('rtbGo') && doc.getElementById('rtbTickBox').hidden && statusOf('rtbPlanStep') === rtbWords.plan,
      'step 3: the plan, every line in its order (W-RECAPTURE first, W-WAIT last), the keyboard on Restore, no tick for a plain archive', JSON.stringify(pp.body));
    let n = 0;
    const record = 'MailArchive — Restore to Thunderbird record\nThe plan:\n…';
    replies['api/restore-tb/start'] = { ok: true, job: '9', wait: rtbWords.wait, encrypted: false };
    replies['api/activity'] = () => {
      n++;
      if (n < 4) return { running: true, job: '9', restore: { running: true, wait: rtbWords.wait, line: 'Restoring: 1 of 3 messages ("Zebrafolder")', checking: false } };
      if (n < 8) return { running: true, job: '9', restore: { running: true, wait: rtbWords.wait, line: 'Restoring: 3 of 3 messages ("Zebrafolder")', checking: true, checkingLine: rtbWords.checking } };
      return { running: false, job: '9', restore: { running: false, result: { words: "Restored 3 messages into Thunderbird as the folder 'Restored — Work mail' under Local Folders.", done: true, stopped: false, record, recordName: 'MailArchive restore record - Work mail - 2026-11-07.txt' } } };
    };
    doc.getElementById('rtbGo').click(); await wait(10);
    const rs = doc.getElementById('rtbRunStep'), sp = posts.filter(p => p.path === 'api/restore-tb/start').pop();
    check(!rs.hidden && sp.body.token === 'T1' && sp.body.notEncryptedOK === false && sp.body.confirmAgain === false && doc.getElementById('rtbWait').textContent === rtbWords.wait &&
      doc.activeElement === doc.getElementById('rtbStop') && statusOf('rtbRunStep') === rtbWords.run, 'step 4: running, W-WAIT shown, the keyboard on Cancel the restore', JSON.stringify(sp.body));
    const progressed = await untilTrue(() => doc.getElementById('rtbProgress').textContent === 'Restoring: 1 of 3 messages ("Zebrafolder")');
    const checking = await untilTrue(() => doc.getElementById('rtbProgress').textContent === rtbWords.checking);
    const waitWhole = doc.getElementById('rtbWait').textContent === rtbWords.wait && !rs.hidden;
    const ended = await untilTrue(() => !doc.getElementById('rtbResultStep').hidden, 2000);
    check(progressed && checking && waitWhole && ended, 'the progress follows the task, then the check, W-WAIT shown for the whole run', JSON.stringify({ progressed, checking, waitWhole, ended }));
    const sv = doc.getElementById('rtbSave');
    check(doc.getElementById('rtbResult').textContent.startsWith('Restored 3 messages') && !sv.hidden && doc.activeElement === sv && doc.getElementById('rtbRecordNote').hidden &&
      statusOf('rtbResultStep') === rtbWords.done && !doc.getElementById('rtbBtn').disabled, "step 5: the result in the engine's words, the keyboard on Save this record", statusOf('rtbResultStep'));
    const b0 = blobs.length, d0 = downloads.length;
    sv.click(); await wait(5);
    check(blobs.length === b0 + 1 && blobs[b0].size === Buffer.byteLength(record) && downloads[d0] === 'MailArchive restore record - Work mail - 2026-11-07.txt',
      'Save this record saves the record, as text, under its name', JSON.stringify(downloads));
  },
  // Restore to Thunderbird on an encrypted archive (MA-1131, MA-1128; §6.2): one profile, so its step
  // is skipped; the plan with the "will not be encrypted" tick and, when it asks twice, the second
  // question with its reasons; a Restore the server refuses for a missing tick or second yes brings
  // the keyboard back to that box, saying why, nothing started; with both, the run's progress is the
  // counts the server sends; Save this record says beside it that the record names the folders.
  async restoreEncrypted() {
    page([['E:\\Enc', 'current']], 'E:\\Enc'); restoreCard();
    replies['api/activity'] = { running: false, job: '0' };
    boot(); await wait(30);
    replies['api/restore-tb/profiles'] = { ok: true, listID: 'L2', default: 0, profiles: [{ position: 0, name: 'default', dir: 'C:\\TB\\d', usable: true, unusable: '', opensByDefault: true, backedUp: false }] };
    doc.getElementById('rtbBtn').click(); await wait(40);
    check(doc.getElementById('rtbProfileStep').hidden && !doc.getElementById('rtbWhatStep').hidden && doc.activeElement === doc.getElementById('rtbWhole'), 'one profile: its step is skipped');
    const again = 'Asked a second time: C:\\Users\\a\\Dropbox\\TB is synced to Dropbox: the restored mail will be uploaded there.';
    replies['api/restore-tb/plan'] = { ok: true, token: 'T2', encrypted: true, askTwice: true, againLine: again, tickLine: 'I understand the restored mail will not be encrypted',
      lines: [{ code: 'DEST', words: 'Restore into…' }, { code: 'NOT-ENCRYPTED', words: 'The restored mail in Thunderbird is NOT encrypted.' }, { code: 'W-WAIT', words: rtbWords.wait }] };
    doc.getElementById('rtbCheck').click(); await wait(40);
    const tk = doc.getElementById('rtbTick'), ag = doc.getElementById('rtbAgain');
    check(!doc.getElementById('rtbTickBox').hidden && !doc.getElementById('rtbAgainBox').hidden && doc.getElementById('rtbAgainWhy').textContent === again && doc.activeElement === tk && statusOf('rtbPlanStep') === rtbWords.planEnc,
      'the plan asks for the tick, and a second yes with its reasons, the keyboard on the tick', statusOf('rtbPlanStep'));
    const tickWords = 'The restored mail will not be encrypted, and the box saying you understand was not ticked, so nothing was restored. Tick it and click Restore again.';
    replies['api/restore-tb/start'] = b => (!b.notEncryptedOK ? { ok: false, need: 'tick', error: tickWords } : (!b.confirmAgain ? { ok: false, need: 'again', error: 'That was not confirmed a second time, so nothing was restored.' } : { ok: true, job: '5', wait: rtbWords.wait, encrypted: true }));
    const go = doc.getElementById('rtbGo');
    go.click(); await wait(30);
    const s1 = posts.filter(p => p.path === 'api/restore-tb/start').pop();
    check(s1.body.notEncryptedOK === false && doc.activeElement === tk && statusOf('rtbPlanStep') === tickWords && doc.getElementById('rtbRunStep').hidden, 'Restore without the tick: the server refuses, said at the tick, nothing started');
    tk.checked = true; go.click(); await wait(30);
    check(doc.activeElement === ag && statusOf('rtbPlanStep') === 'That was not confirmed a second time, so nothing was restored.' && doc.getElementById('rtbRunStep').hidden, 'without the second yes: said at that box, nothing started');
    let n = 0;
    replies['api/activity'] = () => (++n < 4 ? { running: true, job: '5', restore: { running: true, encrypted: true, wait: rtbWords.wait, line: 'Restoring: 1 of 2 messages' } }
      : { running: false, job: '5', restore: { running: false, result: { words: 'Restored 2 messages into Thunderbird.', done: true, stopped: false, record: 'r', recordName: 'rec.txt', recordNote: 'The record names the restored folders and is not encrypted: it is saved only where you choose.' } } });
    ag.checked = true; go.click(); await wait(10);
    const s3 = posts.filter(p => p.path === 'api/restore-tb/start').pop();
    const counts = await untilTrue(() => doc.getElementById('rtbProgress').textContent === 'Restoring: 1 of 2 messages');
    check(s3.body.notEncryptedOK === true && s3.body.confirmAgain === true && counts, 'with both, it runs; its progress is the counts the server sends', JSON.stringify(s3.body));
    await untilTrue(() => !doc.getElementById('rtbResultStep').hidden, 2000);
    const note = doc.getElementById('rtbRecordNote');
    check(!note.hidden && note.textContent === 'The record names the restored folders and is not encrypted: it is saved only where you choose.' && !doc.getElementById('rtbSave').hidden,
      'Save this record says beside it that the record names the restored folders');
  },
  // Cancel, Close, a spent plan, a refusal before the steps, and a page drawn while a restore runs
  // (MA-1131): a page drawn while one runs follows it in its panel; Cancel the restore asks the job
  // slot to stop and stays disabled, and the stopped result is said; Close shuts the panel with the
  // keyboard back on Restore to Thunderbird…; a plan the server no longer holds goes back to Check;
  // a refusal before any step (no profile) is said beside the button, no panel opened.
  async restoreCancel() {
    page([['E:\\Cur', 'current']], 'E:\\Cur'); restoreCard();
    // The page's own activity poll asks too, so the run ends when the test says, not by a count.
    let over = false;
    replies['api/activity'] = () => (!over ? { running: true, job: '4', restore: { running: true, wait: rtbWords.wait, line: 'Restoring: 2 of 9 messages ("Inbox")' } }
      : { running: false, job: '4', restore: { running: false, result: { words: 'The restore was stopped, so nothing was added to Thunderbird. The part already written was removed.', done: false, stopped: true, record: 'r', recordName: 'rec.txt' } } });
    boot(); await wait(5);
    const rs = doc.getElementById('rtbRunStep');
    const followed = await untilTrue(() => !rs.hidden && doc.getElementById('rtbProgress').textContent === 'Restoring: 2 of 9 messages ("Inbox")' && doc.getElementById('rtbWait').textContent === rtbWords.wait);
    check(followed && doc.getElementById('rtbBtn').disabled, 'a page drawn while a restore runs follows it in its panel');
    const stop = doc.getElementById('rtbStop');
    replies['api/job/cancel'] = { ok: true };
    stop.click(); await wait(10);
    check(posts.some(p => p.path === 'api/job/cancel') && stop.disabled, 'Cancel the restore asks the job slot to stop, and stays disabled');
    over = true;
    const stopped = await untilTrue(() => !doc.getElementById('rtbResultStep').hidden, 2000);
    check(stopped && doc.getElementById('rtbResult').textContent.startsWith('The restore was stopped') && statusOf('rtbResultStep') === rtbWords.stopped, 'the stopped result is said', statusOf('rtbResultStep'));
    doc.getElementById('rtbDone').click(); await wait(5);
    const btn = doc.getElementById('rtbBtn');
    check(doc.getElementById('rtbPanel').hidden && doc.activeElement === btn && !btn.disabled, 'Close shuts the panel, the keyboard back on Restore to Thunderbird…');
    replies['api/activity'] = { running: false, job: '4' };
    replies['api/restore-tb/profiles'] = { ok: true, listID: 'L3', default: 0, profiles: [{ position: 0, name: 'default', dir: 'C:\\TB\\d', usable: true, unusable: '', opensByDefault: true, backedUp: false }] };
    replies['api/restore-tb/plan'] = { ok: true, token: 'T3', encrypted: false, askTwice: false, lines: [{ code: 'W-WAIT', words: rtbWords.wait }] };
    replies['api/restore-tb/start'] = { ok: false, planUsed: true, error: 'That restore is no longer planned, so nothing was restored. Click Check again.' };
    btn.click(); await wait(40); doc.getElementById('rtbCheck').click(); await wait(40); doc.getElementById('rtbGo').click(); await wait(40);
    check(!doc.getElementById('rtbWhatStep').hidden && doc.getElementById('rtbPlanStep').hidden && doc.activeElement === doc.getElementById('rtbCheck') &&
      statusOf('rtbWhatStep') === 'That restore is no longer planned, so nothing was restored. Click Check again.', 'a plan the server no longer holds goes back to Check');
    doc.getElementById('rtbClose').click(); await wait(5);
    const nop = 'No Thunderbird profile was found on this computer, so there is nowhere to restore to. Open Thunderbird once to make one, close it, then click Restore to Thunderbird again.';
    replies['api/restore-tb/profiles'] = { ok: false, error: nop };
    btn.click(); await wait(40);
    check(doc.getElementById('rtbPanel').hidden && doc.getElementById('rtbNote').textContent === nop && !doc.getElementById('rtbNote').hidden && !btn.disabled, 'a refusal before any step is said beside the button, no panel opened');
  },
  // The R7 friction review's proposals, settled 2026-11-07 (MA-1131; the lead's calls and words):
  // P1 Change profile from what to restore back to the list, the profile chosen still ticked and
  // the keyboard on it, offered only when there was a choice; P6 a ticked folder ticks its
  // subfolders and they cannot be unticked alone, and only the folder is sent; F13 the tree groups
  // its counts; P3 Shut down's confirmation names a running restore (and not when none runs), and
  // a restore that ends while the keyboard is on Confirm shut down leaves it there; P2 when
  // MailArchive Desktop stops answering, the run step says so instead of a frozen counter, and
  // says the progress again when it answers.
  async restoreFriction() {
    page([['E:\\Cur', 'current']], 'E:\\Cur'); restoreCard();
    doc.body.append(el('button', { class: 'btn secondary', id: 'shutdownBtn', type: 'button' }, 'Shut down'), el('div', { class: 'note', id: 'shutdownNote', hidden: '' }));
    replies['api/activity'] = { running: false, job: '0' };
    boot(); await wait(30);
    const sd = doc.getElementById('shutdownBtn'), sn = doc.getElementById('shutdownNote');
    sd.click(); await wait(20);
    const plainShut = sn.textContent;
    check(plainShut.startsWith('Click again to stop MailArchive Desktop.'), 'twin: with no restore running, Shut down says nothing of one', plainShut);
    sd.dataset.armed = ''; sd.textContent = 'Shut down'; sn.hidden = true;

    replies['api/restore-tb/profiles'] = { ok: true, listID: 'L9', default: 1, profiles: [
      { position: 0, name: 'work', dir: 'C:\\TB\\work', usable: false, unusable: 'This profile has no Local Folders yet, so mail cannot go back into it.', opensByDefault: true, backedUp: false },
      { position: 1, name: 'home', dir: 'C:\\TB\\home', usable: true, unusable: '', opensByDefault: false, backedUp: false }] };
    doc.getElementById('rtbBtn').click(); await wait(40);
    doc.getElementById('rtbNext').click(); await wait(10);
    const back = doc.getElementById('rtbBack');
    check(!back.hidden && doc.getElementById('rtbProfile0').disabled, 'P1: Change profile is offered on what to restore when there was a choice');
    back.click(); await wait(10);
    const p1 = doc.getElementById('rtbProfile1');
    check(!doc.getElementById('rtbProfileStep').hidden && doc.getElementById('rtbWhatStep').hidden && doc.activeElement === p1 && p1.checked &&
      statusOf('rtbProfileStep') === 'Choose the Thunderbird profile to restore into, then click Next.', 'P1: Change profile goes back to the list, the profile chosen ticked with the keyboard on it', statusOf('rtbProfileStep'));
    doc.getElementById('rtbNext').click(); await wait(10);

    replies['api/restore-tb/folders'] = { ok: true, total: 15001, folders: [{ id: 'm', parent: '', name: 'Mail', store: true, count: 0, total: 15001 },
      { id: 'm/P', parent: 'm', name: 'Projects', store: false, count: 15000, total: 15001 }, { id: 'm/P/Q', parent: 'm/P', name: 'Plans', store: false, count: 1, total: 1 }] };
    const ch = doc.getElementById('rtbChoose'); ch.checked = true; ch.fire('change'); await wait(40);
    const boxes = doc.querySelectorAll('[data-rtb-folder]');
    check(doc.getElementById('rtbTree').textContent.includes('Projects (15,001)') && doc.getElementById('rtbTree').textContent.includes('Mail (15,001)'), 'F13: the tree groups its counts', doc.getElementById('rtbTree').textContent);
    boxes[1].checked = true; boxes[1].fire('change');
    const ticked = boxes[2].checked && boxes[2].disabled;
    replies['api/restore-tb/plan'] = { ok: false, error: 'stop here' };
    doc.getElementById('rtbCheck').click(); await wait(30);
    const sent = posts.filter(p => p.path === 'api/restore-tb/plan').pop();
    boxes[1].checked = false; boxes[1].fire('change');
    check(ticked && sent.body.folders.join() === 'm/P' && !boxes[2].checked && !boxes[2].disabled,
      'P6: a ticked folder ticks its subfolder, which cannot be unticked alone; only the folder is sent; unticking gives it back', JSON.stringify(sent.body));
    boxes[1].checked = true; boxes[1].fire('change');

    let phase = 'run';
    replies['api/restore-tb/plan'] = { ok: true, token: 'T9', encrypted: false, askTwice: false, lines: [{ code: 'W-WAIT', words: rtbWords.wait }] };
    replies['api/restore-tb/start'] = { ok: true, job: '7', wait: rtbWords.wait, encrypted: false };
    replies['api/activity'] = () => phase === 'down' ? NOANSWER : (phase === 'run' ? { running: true, job: '7', restore: { running: true, wait: rtbWords.wait, line: 'Restoring: 1 of 15,001 messages ("Projects")' } }
      : { running: false, job: '7', restore: { running: false, result: { words: 'Restored 15,001 messages into Thunderbird.', done: true, stopped: false, record: 'r', recordName: 'rec.txt' } } });
    doc.getElementById('rtbCheck').click(); await wait(40);
    doc.getElementById('rtbGo').click(); await wait(10);
    await untilTrue(() => doc.getElementById('rtbProgress').textContent === 'Restoring: 1 of 15,001 messages ("Projects")');
    sd.click(); await wait(20);
    check(sn.textContent.startsWith('A restore into Thunderbird is running: shutting down stops it, and nothing is added to Thunderbird. Click again to stop MailArchive Desktop.'),
      'P3: Shut down\'s confirmation names the running restore and what Shut down does to it', sn.textContent);

    const notAnswering = 'MailArchive Desktop is not answering — the restore may have stopped. Restart MailArchive Desktop to see what happened.';
    phase = 'down';
    const said = await untilTrue(() => doc.getElementById('rtbProgress').textContent === 'Last seen: Restoring: 1 of 15,001 messages ("Projects")' && statusOf('rtbRunStep') === notAnswering, 2000);
    check(said && !doc.getElementById('rtbRunStep').hidden, 'P2: when MailArchive Desktop stops answering, the run step says so instead of a frozen counter', doc.getElementById('rtbProgress').textContent);
    phase = 'run';
    const back2 = await untilTrue(() => doc.getElementById('rtbProgress').textContent === 'Restoring: 1 of 15,001 messages ("Projects")' && statusOf('rtbRunStep') === rtbWords.run, 2000);
    check(back2, 'P2: when it answers again, the progress is said again');

    sd.focus(); phase = 'done';
    const ended = await untilTrue(() => !doc.getElementById('rtbResultStep').hidden, 2000);
    check(ended && doc.activeElement === sd && statusOf('rtbResultStep') === rtbWords.done, 'P3: a restore that ends while the keyboard is on Confirm shut down leaves it there, its result said', JSON.stringify({ a: doc.activeElement && doc.activeElement.id }));

    reset(); page([['E:\\Cur', 'current']], 'E:\\Cur'); restoreCard();
    replies['api/activity'] = { running: false, job: '0' };
    boot(); await wait(30);
    replies['api/restore-tb/profiles'] = { ok: true, listID: 'L10', default: 0, profiles: [{ position: 0, name: 'only', dir: 'C:\\TB\\o', usable: true, unusable: '', opensByDefault: true, backedUp: false }] };
    doc.getElementById('rtbBtn').click(); await wait(40);
    check(!doc.getElementById('rtbWhatStep').hidden && doc.getElementById('rtbBack').hidden, 'P1 twin: one profile, no Change profile');
  },
};

(async () => {
  if (!scenarios[scenario]) throw new Error('no scenario ' + scenario);
  await scenarios[scenario]();
  console.log('done');
  process.exit(failed ? 1 : 0);
})().catch(e => { console.log('FAIL ' + scenario + ': ' + e.stack); process.exit(1); });
