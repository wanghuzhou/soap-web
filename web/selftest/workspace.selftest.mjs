// Workspace switcher regression checks.
//
// Runs the real workspace.js under a minimal DOM stub. Run with:
//
//     node web/selftest/workspace.selftest.mjs
//
// Regression it guards:
//   - applyWorkspace() must update state, the top-bar label, the document
//     title and the request header source of truth together. A tab whose
//     workspaceId lags behind the server is exactly how a write lands in the
//     wrong workspace.
//   - an external switch must not auto-sync over unsaved editor contents: it
//     has to mark the tab stale instead, so the user decides.

// ---------- minimal DOM stub ----------
class El {
  constructor(id = '') {
    this.id = id;
    this._text = '';
    this._className = '';
    this.classes = new Set();
    this.attrs = {};
    this.listeners = {};
    this.value = '';
    this.title = '';
  }
  set textContent(v) { this._text = String(v); }
  get textContent() { return this._text; }
  set className(v) {
    this._className = String(v);
    this.classes = new Set(this._className.split(/\s+/).filter(Boolean));
  }
  get className() { return this._className; }
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
  addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); }
  removeEventListener() {}
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  getBoundingClientRect() { return { left: 0, top: 0, right: 120, bottom: 30, width: 120, height: 30 }; }
  contains() { return false; }
  focus() {}
  querySelector() { return null; }
  querySelectorAll() { return []; }
}

const els = {};
const getEl = (id) => {
  if (!els[id]) els[id] = new El(id);
  return els[id];
};

globalThis.document = {
  getElementById: getEl,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener: () => {},
  body: new El('body'),
  hidden: false,
  activeElement: null,
  readyState: 'complete',
  title: '',
};
globalThis.window = {
  addEventListener: () => {},
  innerWidth: 1280,
  innerHeight: 800,
  matchMedia: () => ({ matches: true }),
};
globalThis.localStorage = {
  store: {},
  getItem(k) { return this.store[k] ?? null; },
  setItem(k, v) { this.store[k] = String(v); },
  removeItem(k) { delete this.store[k]; },
};

// ---------- run the real modules ----------
const base = new URL('../assets/js/', import.meta.url);
const { state } = await import(new URL('state.js', base).href);
const { initWorkspace, applyWorkspace, handleServerWorkspace } = await import(new URL('workspace.js', base).href);

let failures = 0;
let checks = 0;
function check(label, cond, detail) {
  checks++;
  if (!cond) {
    failures++;
    console.error(`FAIL ${label}${detail ? ' — ' + detail : ''}`);
  }
}

initWorkspace({ onEnter: async () => {} });

const payload = {
  current: { id: 'w1', name: '测试空间' },
  workspaces: [
    { id: 'w1', name: '测试空间' },
    { id: 'w2', name: '另一个' },
  ],
};
applyWorkspace(payload);

check('state 记录当前工作空间', state.workspaceId === 'w1' && state.workspaceName === '测试空间');
check('state 记录工作空间列表', state.workspaces.length === 2 && state.workspaces[1].name === '另一个');
check('顶栏显示名字而非文件', getEl('workspaceName').textContent === '测试空间',
  getEl('workspaceName').textContent);
check('窗口标题跟随工作空间', document.title === 'soap-web · 测试空间', document.title);
check('切换后清除过期标记', state.workspaceStale === false);

// Same workspace from the heartbeat: no stale state, no resync.
handleServerWorkspace({ id: 'w1', name: '测试空间' });
check('同一工作空间心跳不触发同步', state.workspaceStale === false);

// External switch with unsaved edits: mark stale, never silently overwrite.
getEl('requestBody').value = '<dirty/>';
handleServerWorkspace({ id: 'w2', name: '另一个' });
check('外部切换且编辑器有修改时标记过期', state.workspaceStale === true);
check('过期状态反映在顶栏按钮', getEl('workspaceBtn').classList.contains('is-stale'));
check('未保存修改时不清空当前工作空间', state.workspaceId === 'w1');

// Report and exit: the toast leaves a 3.2s timer that would keep Node alive.
if (failures === 0) {
  console.log(`OK ${checks} workspace checks passed`);
} else {
  console.error(`${failures}/${checks} workspace checks FAILED`);
}
process.exit(failures === 0 ? 0 : 1);
