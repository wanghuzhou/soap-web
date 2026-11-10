// Contract checks for the frontend helpers added with headers / search / curl:
//   - header merge (project defaults overlaid by call overrides)
//   - curl command generation and shell quoting
//   - tree filter visibility (match + ancestors keep, everything else hidden)
//     node web/selftest/features.selftest.mjs

// ---------- minimal DOM stub (mirrors tree.selftest.mjs) ----------
class El {
  constructor(tag = 'div') {
    this.tagName = tag.toUpperCase();
    this.children = [];
    this.attrs = {};
    this.style = {
      props: {},
      setProperty(k, v) { this.props[k] = v; },
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
    this.type = 'text';
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
  removeEventListener() {}
  focus() {}
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
  querySelectorAll(sel) {
    const all = this.children.filter((c) => c.classes.has('tree-node'));
    if (sel === '.tree-node') return all;
    if (sel === '.lbl') return this.lbl ? [this.lbl] : [];
    if (sel.startsWith('.tree-node[')) {
      const m = /data-kind="([^"]+)"/.exec(sel);
      const idm = /data-(pid|sid|oid)="([^"]*)"/.exec(sel);
      return all.filter((r) => (!m || r.dataset.kind === m[1])
        && (!idm || r.dataset[idm[1]] === idm[2]));
    }
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
    el.className = 'tree-node ' + m[1];
    const dataRe = /data-([\w-]+)="([^"]*)"/g;
    let d;
    while ((d = dataRe.exec(m[2])) !== null) el.dataset[d[1]] = d[2];
    const lbl = /<span class="lbl"[^>]*>([\s\S]*?)<\/span>/.exec(html.slice(re.lastIndex));
    if (lbl) {
      const span = new El('span');
      span.className = 'lbl';
      span._text = lbl[1];
      el.lbl = span;
    }
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
globalThis.confirm = () => true;

// ---------- run the real modules ----------
const base = new URL('../assets/js/', import.meta.url);
const { state } = await import(new URL('state.js', base).href);
const { renderTree } = await import(new URL('tree.js', base).href);
const { buildCurl, sensitiveNames } = await import(new URL('curl.js', base).href);
const { isSensitiveName, TIMEOUT } = await import(new URL('constants.js', base).href);
const { initTreeSearch, applyTreeFilter, getQuery } = await import(new URL('tree/search.js', base).href);

let failures = 0;
let checks = 0;
function check(label, cond, detail) {
  checks++;
  if (!cond) {
    failures++;
    console.error(`FAIL ${label}${detail ? ' — ' + detail : ''}`);
  }
}

// ---------- curl generation ----------
const simple = buildCurl({ url: 'http://h/x', method: 'POST', headers: {}, body: '<a/>', timeoutMs: 60000 });
check('使用 --location --request 形式', simple.startsWith(`curl --location --request POST 'http://h/x'`), simple);
check('URL 紧跟 --request 而不是放在最后',
  /^curl --location --request POST 'http:\/\/h\/x'\s/.test(simple) && !simple.trimEnd().endsWith(`'http://h/x'`),
  simple);
check('报文用 --data-raw 传递', simple.includes(`--data-raw '<a/>'`), simple);
check('原文换行续行', simple.includes(' \\\n'), JSON.stringify(simple));
check('不使用 -X / -H 短选项', !/\s-X\s/.test(simple) && !/\s-H\s/.test(simple), simple);

check('GET 也显式写明方法',
  buildCurl({ url: 'http://h/x', method: 'GET', headers: {}, body: '', timeoutMs: 0 })
    .startsWith(`curl --location --request GET 'http://h/x'`));

// A single quote in the payload must not break out of the quoting.
const quoted = buildCurl({ url: 'http://h/x', method: 'POST', headers: {}, body: "it's", timeoutMs: 0 });
check('转义单引号', quoted.includes(`'it'\\''s'`), quoted);

// Header values keep their own quoting rules.
const withHeader = buildCurl({
  url: 'http://h/x', method: 'POST', headers: { SOAPAction: '"urn:op"' }, body: '', timeoutMs: 0,
});
check('请求头用 --header', withHeader.includes(`--header 'SOAPAction: "urn:op"'`), withHeader);

check('超时不写进命令（与参考格式一致）',
  !buildCurl({ url: 'http://h/x', method: 'POST', headers: {}, body: '', timeoutMs: 1500 }).includes('--max-time'));

// The multi-line body must survive verbatim inside the quoted argument.
const multiline = buildCurl({
  url: 'http://h/x', method: 'POST', headers: {}, body: '<a>\n  <b/>\n</a>', timeoutMs: 0,
});
check('多行报文原样保留', multiline.includes(`--data-raw '<a>\n  <b/>\n</a>'`), JSON.stringify(multiline));

let oversized = '';
try {
  buildCurl({ url: 'http://h/x', method: 'POST', headers: {}, body: 'x'.repeat(600 * 1024), timeoutMs: 0 });
} catch (error) {
  oversized = error.message;
}
check('超大报文拒绝生成', oversized.includes('512 KB'), oversized);

let missingUrl = '';
try {
  buildCurl({ url: '', method: 'POST', headers: {}, body: '', timeoutMs: 0 });
} catch (error) {
  missingUrl = error.message;
}
check('缺 Endpoint 时报错', missingUrl.includes('Endpoint'), missingUrl);

check('敏感头识别', sensitiveNames({ Authorization: 'x', 'X-Trace': '1' }, isSensitiveName).join() === 'Authorization');
check('普通头不算敏感', !isSensitiveName('Content-Type') && !isSensitiveName('SOAPAction'));
check('token 类头算敏感', isSensitiveName('X-Api-Key') && isSensitiveName('app-token'));

// ---------- tree filter ----------
const fixture = [{
  id: 'p1',
  name: '订单工程',
  source: 'native',
  services: [{
    id: 's1',
    name: 'OrderService',
    operations: [{
      id: 'o1',
      name: 'createOrder',
      calls: [{ id: 'c1', title: '新建订单', request: '<x/>' }],
    }, {
      id: 'o2',
      name: 'cancelOrder',
      calls: [{ id: 'c2', title: '取消订单', request: '<x/>' }],
    }],
  }],
}];

state.projects = fixture;
state.collapsed = {};
renderTree();
const treeEl = getEl('tree');
const rowBy = (attr, value) => treeEl.children.find((r) => r.dataset[attr] === value);
const visibleRows = () => treeEl.children.filter((r) => !r.classes.has('is-hidden'));

check('无搜索时全部可见', visibleRows().length === 6, `实际 ${visibleRows().length} 行`);

initTreeSearch();
getEl('treeSearch').value = 'cancel';
// Drive the same path the input event uses.
getEl('treeSearch').listeners.input.forEach((fn) => fn({ target: getEl('treeSearch') }));
check('搜索命中的行标记为匹配', !!rowBy('oid', 'o2') && rowBy('oid', 'o2').classes.has('is-match'),
  JSON.stringify(treeEl.children.map((r) => [r.dataset.kind, r.classes.has('is-match')])));
check('命中行的祖先仍可见',
  !rowBy('pid', 'p1').classes.has('is-hidden') && !rowBy('sid', 's1').classes.has('is-hidden'));
check('不命中的兄弟行隐藏', rowBy('oid', 'o1').classes.has('is-hidden') && rowBy('cid', 'c2').classes.has('is-hidden'));
check('过滤生效后可检索查询词', getQuery() === 'cancel', getQuery());

// Escape clears the filter and restores every row.
getEl('treeSearch').listeners.keydown.forEach((fn) => fn({ key: 'Escape', preventDefault() {} }));
check('清空搜索后恢复全部行', visibleRows().length === 6, `实际 ${visibleRows().length} 行`);
check('清空后无匹配标记', treeEl.children.every((r) => !r.classes.has('is-match')));

// A filter outranks the collapse state: filtering must reveal a collapsed
// subtree, otherwise a match would be unreachable.
state.collapsed = { p1: true };
renderTree();
check('折叠后子行隐藏', rowBy('oid', 'o1').classes.has('is-hidden'));
getEl('treeSearch').listeners.input.forEach((fn) => fn({ target: getEl('treeSearch') }));
getEl('treeSearch').value = 'create';
getEl('treeSearch').listeners.input.forEach((fn) => fn({ target: getEl('treeSearch') }));
check('搜索穿透折叠状态', !rowBy('oid', 'o1').classes.has('is-hidden'),
  '折叠的子树里命中的行仍然隐藏，用户无法看到结果');

// Re-rendering while a filter is active must keep the filter applied.
state.projects = fixture;
renderTree();
applyTreeFilter();
check('重渲染后过滤仍然生效', rowBy('oid', 'o2').classes.has('is-hidden'));

// A search with no hits hides everything.
getEl('treeSearch').value = 'zzzz';
getEl('treeSearch').listeners.input.forEach((fn) => fn({ target: getEl('treeSearch') }));
check('无命中时全部隐藏', visibleRows().length === 0, `实际 ${visibleRows().length} 行`);

// ---------- timeout constants stay in step with the backend ----------
check('超时上下限', TIMEOUT.MIN === 1000 && TIMEOUT.MAX === 600000 && TIMEOUT.DEFAULT === 60000);

// ---------- report ----------
if (failures === 0) {
  console.log(`OK ${checks} feature checks passed`);
} else {
  console.error(`${failures}/${checks} feature checks FAILED`);
  process.exitCode = 1;
}
