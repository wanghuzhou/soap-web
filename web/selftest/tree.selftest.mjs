// Tree rendering contract: structural changes call renderTree(), selection
// changes call syncSelection(). Runs the real tree.js/state.js under a DOM stub.
//     node web/selftest/tree.selftest.mjs

// ---------- minimal DOM stub ----------
class El {
  constructor(tag = 'div') {
    this.tagName = tag.toUpperCase();
    this.children = [];
    this.parent = null;
    this.attrs = {};
    this.style = {
      props: {},
      setProperty(k, v) { this.props[k] = v; },
      removeProperty(k) { delete this.props[k]; },
      getPropertyValue(k) { return this.props[k] || ''; },
    };
    this.dataset = {};
    this._html = '';
    this._text = '';
    this.classes = new Set();
    this.tabIndex = -1;
    this.listeners = {};
    this.value = '';
    this.checked = false;
  }
  set className(v) { this.classes = new Set(String(v).split(/\s+/).filter(Boolean)); }
  get className() { return [...this.classes].join(' '); }
  get classList() {
    const self = this;
    return {
      add: (...c) => c.forEach((x) => self.classes.add(x)),
      remove: (...c) => c.forEach((x) => self.classes.delete(x)),
      toggle: (c, on) => {
        if (on === undefined) {
          if (self.classes.has(c)) self.classes.delete(c); else self.classes.add(c);
        } else if (on) self.classes.add(c);
        else self.classes.delete(c);
      },
      contains: (c) => self.classes.has(c),
    };
  }
  set innerHTML(v) { this._html = String(v); this.children = parseRows(this, String(v)); }
  get innerHTML() { return this._html; }
  set textContent(v) { this._text = String(v); }
  get textContent() { return this._text; }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  addEventListener(t, fn) { (this.listeners[t] = this.listeners[t] || []).push(fn); }
  removeEventListener(t, fn) { this.listeners[t] = (this.listeners[t] || []).filter((f) => f !== fn); }
  focus() { globalThis.__focused = this; }
  querySelector() { return null; }
  querySelectorAll(sel) {
    const all = this.children.filter((c) => c.classes.has('tree-node'));
    if (sel === '.tree-node') return all;
    if (sel === '.tree-node.selected') return all.filter((c) => c.classes.has('selected'));
    if (sel === '.rename-input') return [];
    return [];
  }
  closest() { return null; }
}

function parseRows(parent, html) {
  const rows = [];
  const re = /<div class="tree-node ([^"]*)"([^>]*)>/g;
  let m;
  while ((m = re.exec(html)) !== null) {
    const el = new El('div');
    el.parent = parent;
    el.className = 'tree-node ' + m[1];
    const dataRe = /data-([\w-]+)="([^"]*)"/g;
    let d;
    while ((d = dataRe.exec(m[2])) !== null) el.dataset[d[1]] = d[2];
    const exp = /aria-expanded="([^"]*)"/.exec(m[2]);
    if (exp) el.attrs['aria-expanded'] = exp[1];
    rows.push(el);
  }
  return rows;
}

const els = {};
const getEl = (id) => {
  if (!els[id]) els[id] = new El();
  return els[id];
};

globalThis.document = {
  getElementById: getEl,
  querySelector: () => null,
  querySelectorAll: () => [],
  createElement: (t) => new El(t),
  addEventListener: () => {},
  body: new El('body'),
  hidden: false,
  activeElement: null,
  readyState: 'complete',
};
globalThis.window = { addEventListener: () => {}, matchMedia: () => ({ matches: true }) };
globalThis.localStorage = {
  store: {},
  getItem(k) { return this.store[k] ?? null; },
  setItem(k, v) { this.store[k] = String(v); },
  removeItem(k) { delete this.store[k]; },
};
globalThis.requestAnimationFrame = (fn) => setTimeout(fn, 0);
globalThis.cancelAnimationFrame = (id) => clearTimeout(id);
globalThis.confirm = () => true;
globalThis.prompt = (msg, def) => def;

// ---------- run the real modules ----------
const base = new URL('../assets/js/', import.meta.url);
const { state, initCollapsed, saveCollapsed } = await import(new URL('state.js', base).href);
const { renderTree, syncSelection, toggleNode } = await import(new URL('tree.js', base).href);

let failures = 0;
let checks = 0;
function check(label, cond, detail) {
  checks++;
  if (!cond) {
    failures++;
    console.error(`FAIL ${label}${detail ? ' — ' + detail : ''}`);
  }
}

// Two projects-worth of fixture data.
function fixture(callCount) {
  return [{
    id: 'p1',
    name: 'Project',
    source: 'native',
    services: [{
      id: 's1',
      name: 'Service',
      operations: [{
        id: 'o1',
        name: 'Operation',
        calls: Array.from({ length: callCount }, (_, i) => ({
          id: `c${i + 1}`,
          title: `call ${i + 1}`,
          request: '<x/>',
        })),
      }],
    }],
  }];
}

// ---- structural change must appear without a page refresh ----
state.projects = fixture(0);
renderTree();
let rows = getEl('tree').children;
check('初始树包含 proj/svc/op', rows.length === 3, `实际 ${rows.length} 行`);

// Simulate the server having accepted a new call and reload() re-fetching.
state.projects = fixture(1);
renderTree();
rows = getEl('tree').children;
check('新建请求后树包含该行', rows.length === 4, `实际 ${rows.length} 行`);
check('新建行可被定位',
  rows.some((r) => r.dataset.kind === 'call' && r.dataset.cid === 'c1'),
  JSON.stringify(rows.map((r) => r.dataset.kind)));

// ---- selection change must NOT rebuild, but must move the marker ----
const before = getEl('tree').innerHTML;
state.selected = { kind: 'call', node: {}, ids: { pid: 'p1', sid: 's1', oid: 'o1', cid: 'c1' } };
syncSelection();
check('syncSelection 不重建树', getEl('tree').innerHTML === before,
  'syncSelection 改变了树的 HTML，说明它在重建而非迁移选中态');
rows = getEl('tree').children;
const selectedRow = rows.find((r) => r.dataset.cid === 'c1');
check('选中标记已迁移', !!selectedRow && selectedRow.classes.has('selected'));
check('aria-selected 同步', !!selectedRow && selectedRow.attrs['aria-selected'] === 'true');
check('未选中行 aria-selected=false',
  rows.filter((r) => r.dataset.cid !== 'c1').every((r) => r.attrs['aria-selected'] === 'false'));

// ---- roving tabindex: exactly one row in the tab order ----
check('仅一行可 Tab 聚焦', rows.filter((r) => r.tabIndex === 0).length === 1,
  `实际 ${rows.filter((r) => r.tabIndex === 0).length} 行`);

// ---- ARIA tree structure ----
const html = getEl('tree').innerHTML;
check('子层有 role=group', /role="group"/.test(html));
check('行有 aria-level', /aria-level="1"/.test(html) && /aria-level="2"/.test(html));
check('非叶子行有 aria-expanded', /aria-expanded="true"/.test(html));

// ---- collapse state must not orphan keys forever ----
state.collapsed = { gone: true };
renderTree();
check('孤儿折叠键被回收', !state.collapsed.gone,
  `collapsed = ${JSON.stringify(state.collapsed)}`);

// ---- deleting a call removes its row ----
state.projects = fixture(0);
renderTree();
rows = getEl('tree').children;
check('删除后行消失', !rows.some((r) => r.dataset.kind === 'call'));

// ---- collapse state is namespaced per workspace ----
// Regression: the map used to be global, so rendering workspace B pruned the
// keys of workspace A and switching back lost the user's expanded/collapsed
// layout. The legacy global key must be adopted by the first workspace only.
localStorage.store['soap-web-collapsed:w1'] = JSON.stringify({ p1: true });
initCollapsed('w1');
check('按工作空间读取折叠状态', state.collapsed.p1 === true,
  JSON.stringify(state.collapsed));

localStorage.store['soap-web-collapsed:w2'] = JSON.stringify({ p2: true });
initCollapsed('w2');
check('切换工作空间不串用折叠状态', state.collapsed.p2 === true && !state.collapsed.p1,
  JSON.stringify(state.collapsed));

// A debounced save must be flushed before the map is replaced, otherwise the
// last caret click in the old workspace is lost to the 300ms window.
state.workspaceId = 'w1';
state.collapsed = { a: true };
saveCollapsed();
initCollapsed('w2');
const flushed = JSON.parse(localStorage.store['soap-web-collapsed:w1'] || '{}');
check('切换前落盘待保存的折叠状态', flushed.a === true, JSON.stringify(flushed));

// Legacy global key is migrated once and removed.
delete localStorage.store['soap-web-collapsed:w3'];
localStorage.store['soap-web-collapsed'] = JSON.stringify({ legacy: true });
initCollapsed('w3');
check('旧版全局折叠状态被迁移', state.collapsed.legacy === true
  && localStorage.store['soap-web-collapsed'] === undefined,
  JSON.stringify(state.collapsed));

// ---- collapsing changes visibility without rebuilding the tree ----
state.collapsed = {};
state.projects = fixture(1);
renderTree();
const beforeCollapse = getEl('tree').innerHTML;
toggleNode('p1');
rows = getEl('tree').children;
check('折叠后后代行隐藏',
  rows.filter((r) => r.dataset.kind !== 'proj').every((r) => r.classes.has('is-hidden')),
  JSON.stringify(rows.map((r) => [r.dataset.kind, r.classes.has('is-hidden')])));
check('折叠不重建树', getEl('tree').innerHTML === beforeCollapse);
toggleNode('p1');
check('展开后后代行恢复',
  getEl('tree').children
    .filter((r) => r.dataset.kind !== 'proj')
    .every((r) => !r.classes.has('is-hidden')));

// ---- report ----
if (failures === 0) {
  console.log(`OK ${checks} tree checks passed`);
} else {
  console.error(`${failures}/${checks} tree checks FAILED`);
  process.exitCode = 1;
}
