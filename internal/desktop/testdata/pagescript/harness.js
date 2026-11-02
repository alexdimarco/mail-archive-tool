// The Overview's page script run against a small stand-in for the browser's document (MA-986):
// the Desktop's Go tests run no browser, so ma986_page_behaviour_test.go runs the served
// dashboard.js here, under Node, and reads what it did. The stand-in has only what the script
// uses: elements, attributes, children, the selectors the script queries, click and input events,
// fetch (answered per route by the scenario, each after its own delay), location.reload and
// sessionStorage. It never touches a real network, file or browser.
//
// Usage: node harness.js DASHBOARD_JS SCENARIO. Each check prints "ok NAME" or "FAIL NAME: why";
// the scenario ends with "done".
'use strict';
const fs = require('fs');
const vm = require('vm');

const code = fs.readFileSync(process.argv[2], 'utf8');
const scenario = process.argv[3];
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
  click() { if (this.disabled) return; (this.listeners.click || []).forEach(f => f({ preventDefault() {} })); if (this.onclick) this.onclick(); }
  fire(ev) { (this.listeners[ev] || []).forEach(f => f({})); }
  focus() { doc.activeElement = this; }
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
  doc.body.append(el('nav', { class: 'nav' }, el('a', { href: '#sec-archive', id: 'navArchive' }, 'Archive now'), el('a', { href: '#sec-backup', id: 'navRunNow' }, 'Run now')));
  doc.body.append(el('div', { id: 'heldDeletes', 'data-held-deletes': JSON.stringify(held || []), hidden: '' }));
  const card = el('div', { class: 'card', id: 'sec-archives' }, el('h2', {}, 'Your archives'));
  const ul = el('ul', { class: 'archive-list' });
  rows.forEach(([p, s]) => ul.append(row(p, s)));
  card.append(ul); doc.body.append(card);
  doc.body.append(el('div', { class: 'card', id: 'sec-archive' }, el('button', { class: 'btn', id: 'archiveBtn' }, 'Archive now'), el('div', { class: 'note', id: 'captureResult', hidden: '' })));
  doc.body.append(el('div', { class: 'card', id: 'sec-backup' }, el('button', { class: 'btn', id: 'runBackupBtn', type: 'button' }, 'Run now'), el('div', { class: 'note', id: 'runBackupNote', hidden: '' })));
  const outDir = el('input', { id: 'outDir' }); outDir.value = drawnFor;
  doc.body.append(el('div', { class: 'card', id: 'sec-settings' }, outDir, el('div', { class: 'note', id: 'settingsNote', hidden: '' }),
    el('div', { class: 'note warnbox', id: 'settingsConfirm', hidden: '' }, el('p', { id: 'settingsConfirmText' }))));
}

// ---- the browser around it -------------------------------------------------------------
const posts = [], gets = [];
let reloads = 0;
const replies = {}, delays = {};
const store = {};
function fetchStub(path, opts) {
  const body = opts && opts.body ? JSON.parse(opts.body) : null;
  if (opts && opts.method === 'POST') posts.push({ path, body }); else gets.push(path);
  const r = typeof replies[path] === 'function' ? replies[path](body) : (replies[path] || { ok: true });
  const ms = typeof delays[path] === 'function' ? delays[path](body) : (delays[path] || 0);
  return new Promise(res => setTimeout(() => res({ json: () => Promise.resolve(r), status: 200 }), ms));
}
// boot runs the page script on the page as drawn, as a tab loading it would (a reload is a new
// boot: the same tab's sessionStorage, a fresh script).
let ctx = null;
function boot() {
  ctx = {
    document: doc, fetch: fetchStub, atob: s => Buffer.from(s, 'base64').toString('latin1'),
    Uint8Array, Blob, JSON, Array, Promise, Object, String, Number, Error, Proxy,
    URL: { createObjectURL: () => 'blob:1', revokeObjectURL() {} },
    location: { reload() { reloads++; }, hash: '' },
    sessionStorage: { getItem: k => (k in store ? store[k] : null), setItem: (k, v) => { store[k] = String(v); }, removeItem: k => { delete store[k]; } },
    setTimeout: (f, ms) => setTimeout(f, Math.min(ms, 20)), setInterval: () => 0, clearTimeout, console,
    confirm: () => { throw new Error('the page asked through confirm()'); },
    prompt: () => { throw new Error('the page asked through prompt()'); },
  };
  ctx.window = ctx; ctx.addEventListener = () => {};
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
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

const scenarios = {
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
};

(async () => {
  if (!scenarios[scenario]) throw new Error('no scenario ' + scenario);
  await scenarios[scenario]();
  console.log('done');
  process.exit(failed ? 1 : 0);
})().catch(e => { console.log('FAIL ' + scenario + ': ' + e.stack); process.exit(1); });
