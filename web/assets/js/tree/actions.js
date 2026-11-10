// Tree CRUD actions, the project menu and in-place renaming.

import { $ } from '../dom.js';
import { apiJSON, callPath, projectPath } from '../api.js';
import { toast } from '../toast.js';
import { state, findProject, findCall, toggleCollapsed } from '../state.js';
import { openProxyDialog } from '../proxy.js';
import { openRowMenu } from '../rowmenu.js';
import { startInlineRename } from '../inlineedit.js';
import { isCollapsed, updateRowLabel } from './render.js';

let hooks = {
  onSelect: () => {},
  reload: async () => {},
};

export function initTreeActions(options) {
  hooks = { ...hooks, ...options };
}

// Project row "⋯" menu. Items carry the same dataset shape as the old inline
// buttons, so handleTreeAction needs no changes.
export function openProjectMenu(anchor) {
  const project = findProject(anchor.dataset.pid);
  if (!project) return;

  const pid = project.id;
  const items = [
    { label: '代理设置…', dataset: { act: 'proxy-proj', pid } },
    { label: '重命名工程', dataset: { act: 'rename-proj', pid } },
    { label: '导出 SoapUI XML', dataset: { act: 'export-proj', pid } },
    { separator: true },
    { label: '删除工程', danger: true, dataset: { act: 'del-proj', pid } },
  ];

  openRowMenu({
    anchor,
    items,
    onSelect: (item) => handleTreeAction(item.dataset),
  });
}

function clearSelection(pid, sid, oid) {
  const selected = state.selected;
  if (!selected) return;
  const ids = selected.ids || {};
  if (ids.pid !== pid) return;
  if (sid && ids.sid !== sid) return;
  if (oid && ids.oid !== oid) return;
  state.selected = null;
}

// ---- In-place rename (no prompt dialogs) ----

function findRow(kind, ids) {
  for (const row of $('tree').querySelectorAll('.tree-node')) {
    const d = row.dataset;
    if (d.kind !== kind || d.pid !== ids.pid) continue;
    if (ids.sid && d.sid !== ids.sid) continue;
    if (ids.oid && d.oid !== ids.oid) continue;
    if (ids.cid && d.cid !== ids.cid) continue;
    return row;
  }
  return null;
}

export function beginRename(kind, ids) {
  let current = '';
  if (kind === 'proj') {
    const project = findProject(ids.pid);
    if (!project) return;
    current = project.name || '';
  } else if (kind === 'call') {
    const found = findCall(ids);
    if (!found) return;
    current = found.call.title || '';
  } else {
    return;
  }

  const row = findRow(kind, ids);
  if (!row) return;
  startInlineRename(row, {
    value: current,
    onCommit: (next) => commitRename(kind, ids, next, current),
  });
}

async function commitRename(kind, ids, next, current) {
  if (!next || next === current) return;
  try {
    if (kind === 'proj') {
      const project = findProject(ids.pid);
      if (!project) return;
      // Narrow write: renaming must not overwrite concurrent edits to the rest
      // of the project made in another tab.
      await apiJSON('PUT', projectPath(ids.pid, 'name'), { name: next });
      project.name = next;
      updateRowLabel('proj', ids, next);
    } else {
      const found = findCall(ids);
      if (!found) return;
      await apiJSON('PUT', callPath(ids), { ...found.call, title: next });
      found.call.title = next;
      updateRowLabel('call', ids, next);
      // Keep the request pane title in sync without reloading the editor
      // (which would discard unsaved XML edits).
      const selected = state.selected;
      if (selected && selected.kind === 'call' && selected.ids.cid === ids.cid) {
        const parent = selected.parentOp;
        $('reqTitle').textContent = next + (parent ? '  ·  ' + (parent.name || '') : '');
      }
    }
    toast('已重命名');
  } catch (error) {
    // The API call failed, so the row still shows the old name; nothing to undo.
    toast('重命名失败: ' + error.message, true);
  }
}

const DELETE_PROMPT = {
  call: '确定删除该请求样例？',
  op: '确定删除该操作及其下所有请求？',
  svc: '确定删除该服务及其下所有操作？',
};

// deleteNode confirms and deletes one node. It returns true when the delete was
// sent, so callers own selection cleanup and reloading. Sharing it keeps the
// tree buttons and the editor's delete button on one prompt and one URL.
export async function deleteNode(kind, ids) {
  let promptText = DELETE_PROMPT[kind];
  if (kind === 'proj') {
    const project = findProject(ids.pid);
    promptText = '确定删除整个工程「' + ((project && project.name) || '') + '」？';
  }
  if (!promptText || !confirm(promptText)) return false;

  if (kind === 'call') {
    await apiJSON('DELETE', callPath(ids));
  } else if (kind === 'op') {
    await apiJSON('DELETE', projectPath(ids.pid, 'services', ids.sid, 'operations', ids.oid));
  } else if (kind === 'svc') {
    await apiJSON('DELETE', projectPath(ids.pid, 'services', ids.sid));
  } else if (kind === 'proj') {
    await apiJSON('DELETE', projectPath(ids.pid));
  } else {
    return false;
  }
  return true;
}

// Each entry handles one data-act value. Keeping them in one table makes the
// set of row actions discoverable and leaves the dispatcher free of branching.
const TREE_ACTIONS = {
  'add-svc': async (data) => {
    const name = prompt('新服务名称：', 'NewService');
    if (name == null) return;
    await apiJSON('POST', projectPath(data.pid, 'services'), { name: name || 'Service' });
    await hooks.reload();
  },

  'del-svc': async (data) => {
    if (!(await deleteNode('svc', { pid: data.pid, sid: data.sid }))) return;
    clearSelection(data.pid, data.sid);
    await hooks.reload();
  },

  'add-op': async (data) => {
    const name = prompt('新操作名称：', 'NewOperation');
    if (name == null) return;
    await apiJSON('POST', projectPath(data.pid, 'services', data.sid, 'operations'), { name: name || 'Operation' });
    await hooks.reload();
  },

  'del-op': async (data) => {
    if (!(await deleteNode('op', { pid: data.pid, sid: data.sid, oid: data.oid }))) return;
    clearSelection(data.pid, data.sid, data.oid);
    await hooks.reload();
  },

  'add-call': async (data) => {
    // Create immediately with the backend's default title, then start an
    // in-place rename so the user can name it without an extra dialog.
    const url = projectPath(data.pid, 'services', data.sid, 'operations', data.oid, 'calls');
    const created = await apiJSON('POST', url, { title: '' });
    // The new call must be visible for the inline rename input to exist; make
    // sure a previously collapsed operation is expanded before re-rendering.
    if (isCollapsed(data.oid)) toggleCollapsed(data.oid);
    await hooks.reload();
    const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: created.id };
    const found = findCall(ids);
    if (found) hooks.onSelect(found.call, 'call', { ids, parentOp: found.operation });
    beginRename('call', ids);
  },

  'del-call': async (data) => {
    if (!(await deleteNode('call', {
      pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid,
    }))) return;
    clearSelection(data.pid, data.sid, data.oid);
    await hooks.reload();
  },

  'rename-call': (data) => {
    beginRename('call', { pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid });
  },

  // Duplicate keeps the payload, headers and timeout, so a variant only needs
  // the fields that differ edited.
  'duplicate-call': async (data) => {
    const url = projectPath(data.pid, 'services', data.sid, 'operations', data.oid, 'calls', data.cid, 'duplicate');
    const copied = await apiJSON('POST', url);
    if (isCollapsed(data.oid)) toggleCollapsed(data.oid);
    await hooks.reload();
    const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: copied.id };
    const found = findCall(ids);
    if (found) hooks.onSelect(found.call, 'call', { ids, parentOp: found.operation });
    toast('已复制请求样例');
  },

  'proxy-proj': (data) => {
    openProxyDialog(data.pid);
  },

  'del-proj': async (data) => {
    if (!(await deleteNode('proj', { pid: data.pid }))) return;
    clearSelection(data.pid);
    await hooks.reload();
  },

  'rename-proj': (data) => {
    beginRename('proj', { pid: data.pid });
  },

  'export-proj': (data) => {
    const link = document.createElement('a');
    link.href = projectPath(data.pid, 'export');
    link.download = '';
    document.body.appendChild(link);
    link.click();
    link.remove();
    toast('已导出 SoapUI XML');
  },
};

export async function handleTreeAction(data) {
  const run = TREE_ACTIONS[data.act];
  if (!run) return;
  try {
    await run(data);
  } catch (error) {
    toast(error.message, true);
  }
}
