// Project tree rendering and all tree-level CRUD actions.

import { $, escapeHtml, escapeAttr } from './dom.js';
import { apiJSON } from './api.js';
import { toast } from './toast.js';
import { state, findProject, findService, findOperation, findCall, toggleCollapsed, pruneCollapsed } from './state.js';
import { openProxyDialog, proxyLabel } from './proxy.js';
import { openRowMenu } from './rowmenu.js';
import { startInlineRename, cancelActiveRename } from './inlineedit.js';

let handlers = {
  onSelect: () => {},
  reload: async () => {},
};

export function initTree(options) {
  handlers = { ...handlers, ...options };
  const tree = $('tree');
  tree.addEventListener('click', handleTreeClick);
  tree.addEventListener('keydown', handleTreeKeydown);
  tree.addEventListener('focusin', handleTreeFocusIn);
}

// handleTreeFocusIn makes the focused row the roving tab stop, so Tab-ing away
// and back returns to where the user was.
function handleTreeFocusIn(event) {
  const row = event.target.closest('.tree-node');
  if (!row) return;
  const tree = $('tree');
  tree.querySelectorAll('.tree-node').forEach((r) => { r.tabIndex = -1; });
  row.tabIndex = 0;
}

// handleTreeKeydown implements the standard ARIA tree keyboard model:
//   Up/Down   move between visible rows
//   Right     expand, or move into the first child
//   Left      collapse, or move to the parent
//   Enter/Space select the row
//   F2        rename (calls only)
function handleTreeKeydown(event) {
  const row = event.target.closest('.tree-node');
  if (!row) return;
  const tree = $('tree');
  const rows = Array.from(tree.querySelectorAll('.tree-node'));
  const index = rows.indexOf(row);
  if (index < 0) return;

  const move = (target) => {
    if (!target) return;
    event.preventDefault();
    rows.forEach((r) => { r.tabIndex = -1; });
    target.tabIndex = 0;
    target.focus();
  };

  switch (event.key) {
    case 'ArrowDown':
      move(rows[index + 1]);
      return;
    case 'ArrowUp':
      move(rows[index - 1]);
      return;
    case 'Home':
      move(rows[0]);
      return;
    case 'End':
      move(rows[rows.length - 1]);
      return;
    case 'ArrowRight': {
      event.preventDefault();
      const key = toggleKeyOf(row);
      if (key && isCollapsed(key)) {
        toggleCollapsed(key);
        renderTree();
        refocusRow(row);
        return;
      }
      const next = rows[index + 1];
      // Only descend when the next row is actually a child (deeper).
      if (next && depthOf(next) > depthOf(row)) move(next);
      return;
    }
    case 'ArrowLeft': {
      event.preventDefault();
      const key = toggleKeyOf(row);
      if (key && !isCollapsed(key)) {
        toggleCollapsed(key);
        renderTree();
        refocusRow(row);
        return;
      }
      // Move to the nearest preceding row with a shallower depth (its parent).
      for (let i = index - 1; i >= 0; i--) {
        if (depthOf(rows[i]) < depthOf(row)) { move(rows[i]); return; }
      }
      return;
    }
    case 'Enter':
    case ' ':
      event.preventDefault();
      selectRow(row);
      return;
    case 'F2':
      if (row.dataset.kind === 'call') {
        event.preventDefault();
        handleTreeAction({ act: 'rename-call', ...row.dataset });
      }
      return;
    default:
  }
}

// toggleKeyOf returns the collapse key of a row, or '' when it is a leaf.
function toggleKeyOf(row) {
  const caret = row.querySelector('.caret[data-toggle]');
  return caret ? caret.dataset.toggle : '';
}

function depthOf(row) {
  return Number(row.style.getPropertyValue('--depth')) || 0;
}

// refocusRow re-establishes focus after renderTree() rebuilt the rows.
function refocusRow(staleRow) {
  const tree = $('tree');
  const kind = staleRow.dataset.kind;
  const keys = ['pid', 'sid', 'oid', 'cid'].filter((k) => staleRow.dataset[k]);
  let selector = '.tree-node';
  keys.forEach((k) => { selector += `[data-${k}="${staleRow.dataset[k]}"]`; });
  const fresh = tree.querySelector(selector) || tree.querySelector(`.tree-node[data-kind="${kind}"]`);
  if (fresh) {
    tree.querySelectorAll('.tree-node').forEach((r) => { r.tabIndex = -1; });
    fresh.tabIndex = 0;
    fresh.focus();
  }
}

function isCollapsed(key) {
  return !!state.collapsed[key];
}

function caretHtml(key, leaf) {
  if (leaf) return '<span class="caret leaf" aria-hidden="true"></span>';
  const glyph = isCollapsed(key) ? '▸' : '▾';
  return `<span class="caret" data-toggle="${escapeAttr(key)}" title="展开/收起">${glyph}</span>`;
}

function rowHtml({ kind, depth, attrs, label, tags = '', actions = '', toggleKey, leaf = false, selected = false }) {
  const attrHtml = Object.entries(attrs)
    .map(([name, value]) => ` data-${name}="${escapeAttr(value)}"`)
    .join('');
  // Roving tabindex: exactly one row is in the tab order (the selected one,
  // or the first row when nothing is selected). Without this, role="treeitem"
  // rows were not focusable at all, so keyboard users could never select a
  // request sample — only click it.
  const expanded = leaf ? '' : ` aria-expanded="${isCollapsed(toggleKey) ? 'false' : 'true'}"`;
  return `<div class="tree-node ${kind}${selected ? ' selected' : ''}" style="--depth:${depth}" data-kind="${kind}" role="treeitem" aria-level="${depth + 1}" aria-selected="${selected ? 'true' : 'false'}" tabindex="-1"${expanded}${attrHtml}>
    ${caretHtml(toggleKey, leaf)}
    <span class="lbl" title="${escapeAttr(label)}">${escapeHtml(label)}</span>
    ${tags}
    <span class="node-actions">${actions}</span>
  </div>`;
}

export function renderTree() {
  const tree = $('tree');
  // A rebuild would detach an in-place rename input; cancel it first so the
  // module-level editor state never points at a removed element.
  cancelActiveRename();
  const projects = state.projects || [];
  const count = $('projectCount');
  if (count) count.textContent = String(projects.length);

  if (!projects.length) {
    tree.innerHTML = `<div class="empty-state">
      <div class="empty-icon" aria-hidden="true">⌁</div>
      <div class="empty-title">还没有工程</div>
      <div class="empty-desc">点击顶部「导入工程」或「解析 WSDL」开始</div>
    </div>`;
    return;
  }

  let html = '';
  projects.forEach((project) => {
    // The project row only keeps its primary action; everything else lives in
    // the "⋯" menu so the row stays readable at a glance.
    const projectActions = `
      <button class="mini" title="新建服务" data-act="add-svc" data-pid="${escapeAttr(project.id)}">+服务</button>
      <button class="mini more" title="更多操作" aria-haspopup="menu" aria-expanded="false"
              data-more="proj" data-pid="${escapeAttr(project.id)}">⋯</button>`;

    html += '<div class="tree-group" role="group">';
    html += rowHtml({
      kind: 'proj',
      depth: 0,
      attrs: { pid: project.id },
      label: project.name || '工程',
      tags: `${project.source && project.source !== 'native' ? `<span class="tag">${escapeHtml(project.source)}</span>` : ''}<span class="tag proxy-tag" title="工程代理">${escapeHtml(proxyLabel(project.proxy))}</span>`,
      actions: projectActions,
      toggleKey: project.id,
      leaf: false,
    });

    if (!isCollapsed(project.id)) {
      (project.services || []).forEach((service) => {
        const serviceActions = `
          <button class="mini" title="新建操作" data-act="add-op" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}">+操作</button>
          <button class="mini danger" title="删除服务" data-act="del-svc" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}">✕</button>`;
        const serviceSelected = !!state.selected && state.selected.kind === 'svc'
          && state.selected.ids.pid === project.id && state.selected.ids.sid === service.id;
        html += rowHtml({
          kind: 'svc',
          depth: 1,
          attrs: { pid: project.id, sid: service.id },
          label: service.name || 'service',
          tags: '<span class="tag">service</span>',
          actions: serviceActions,
          toggleKey: service.id,
          leaf: !(service.operations && service.operations.length),
          selected: serviceSelected,
        });

        if (!isCollapsed(service.id)) {
          (service.operations || []).forEach((operation) => {
            const callCount = (operation.calls || []).length;
            const operationActions = `
              <button class="mini" title="新建请求样例" data-act="add-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}">+请求</button>
              <button class="mini danger" title="删除操作" data-act="del-op" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}">✕</button>`;
            const operationSelected = !!state.selected && state.selected.kind === 'op'
              && state.selected.ids.pid === project.id
              && state.selected.ids.sid === service.id
              && state.selected.ids.oid === operation.id;
            html += rowHtml({
              kind: 'op',
              depth: 2,
              attrs: { pid: project.id, sid: service.id, oid: operation.id },
              label: operation.name || 'operation',
              tags: `<span class="tag">${callCount ? callCount + ' 请求' : 'op'}</span>`,
              actions: operationActions,
              toggleKey: operation.id,
              leaf: callCount === 0,
              selected: operationSelected,
            });

            if (!isCollapsed(operation.id)) {
              (operation.calls || []).forEach((call) => {
                const callActions = `
                  <button class="mini" title="重命名请求（就地编辑）" data-act="rename-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}" data-cid="${escapeAttr(call.id)}">✎</button>
                  <button class="mini danger" title="删除请求" data-act="del-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}" data-cid="${escapeAttr(call.id)}">✕</button>`;
                const callSelected = !!state.selected && state.selected.kind === 'call'
                  && state.selected.ids.pid === project.id
                  && state.selected.ids.sid === service.id
                  && state.selected.ids.oid === operation.id
                  && state.selected.ids.cid === call.id;
                html += rowHtml({
                  kind: 'call',
                  depth: 3,
                  attrs: { pid: project.id, sid: service.id, oid: operation.id, cid: call.id },
                  label: call.title || '请求',
                  actions: callActions,
                  toggleKey: call.id,
                  leaf: true,
                  selected: callSelected,
                });
              });
            }
          });
        }
      });
    }
    html += '</div>';
  });

  tree.innerHTML = html;
  pruneCollapsed(collectKeys(projects));
  syncTabStop();
}

// collectKeys returns every id that may still be collapsed, so orphaned keys
// (from deleted nodes or a re-import that assigned new ids) can be reclaimed.
function collectKeys(projects) {
  const keys = new Set();
  projects.forEach((project) => {
    keys.add(project.id);
    (project.services || []).forEach((service) => {
      keys.add(service.id);
      (service.operations || []).forEach((operation) => keys.add(operation.id));
    });
  });
  return keys;
}

// syncTabStop keeps exactly one row reachable with Tab (roving tabindex).
function syncTabStop() {
  const tree = $('tree');
  if (!tree) return;
  const rows = tree.querySelectorAll('.tree-node');
  if (!rows.length) return;
  const active = tree.querySelector('.tree-node.selected') || rows[0];
  rows.forEach((row) => { row.tabIndex = -1; });
  active.tabIndex = 0;
}

function handleTreeClick(event) {
  // Never let a click inside the rename input select / collapse the row.
  if (event.target.closest('.rename-input')) return;

  const moreButton = event.target.closest('[data-more]');
  if (moreButton) {
    event.stopPropagation();
    openProjectMenu(moreButton);
    return;
  }

  const actionButton = event.target.closest('[data-act]');
  if (actionButton) {
    event.stopPropagation();
    handleTreeAction(actionButton.dataset);
    return;
  }

  const caret = event.target.closest('.caret[data-toggle]');
  if (caret) {
    event.stopPropagation();
    toggleCollapsed(caret.dataset.toggle);
    renderTree();
    return;
  }

  const row = event.target.closest('.tree-node');
  if (!row) return;
  selectRow(row);
}

// selectRow dispatches selection for a tree row. Shared by mouse and keyboard.
function selectRow(row) {
  const data = row.dataset;

  if (data.kind === 'proj') {
    toggleCollapsed(data.pid);
    renderTree();
    return;
  }

  if (data.kind === 'svc') {
    const found = findService(data.pid, data.sid);
    if (found) handlers.onSelect(found.service, 'svc', { ids: { pid: data.pid, sid: data.sid } });
  } else if (data.kind === 'op') {
    const found = findOperation(data.pid, data.sid, data.oid);
    if (found) handlers.onSelect(found.operation, 'op', { ids: { pid: data.pid, sid: data.sid, oid: data.oid } });
  } else if (data.kind === 'call') {
    const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid };
    const found = findCall(ids);
    if (found) handlers.onSelect(found.call, 'call', { ids, parentOp: found.operation });
  }
}

// syncSelection moves the .selected marker without rebuilding the tree.
//
// renderTree() used to be called at the end of every loadNode(), which re-parsed
// the whole tree (roughly 80 KB of HTML for 200 nodes) just to move one class.
// It also destroyed the button the user had just clicked, dropping focus to
// <body> and breaking :focus-within, so the row's inline actions vanished.
export function syncSelection() {
  const tree = $('tree');
  if (!tree) return;
  const selected = state.selected;
  const ids = (selected && selected.ids) || {};
  const kind = selected ? selected.kind : '';

  const rows = tree.querySelectorAll('.tree-node');
  rows.forEach((row) => {
    const match = kind && row.dataset.kind === kind
      && (!ids.pid || row.dataset.pid === ids.pid)
      && (!ids.sid || row.dataset.sid === ids.sid)
      && (!ids.oid || row.dataset.oid === ids.oid)
      && (!ids.cid || row.dataset.cid === ids.cid);
    row.classList.toggle('selected', !!match);
    row.setAttribute('aria-selected', match ? 'true' : 'false');
  });
  syncTabStop();
}

// Project row "⋯" menu. Items carry the same dataset shape as the old inline
// buttons, so handleTreeAction needs no changes.
function openProjectMenu(anchor) {
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

function projectURL(projectID, suffix = '') {
  return '/api/projects/' + encodeURIComponent(projectID) + suffix;
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

function beginRename(kind, ids) {
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
      await apiJSON('PUT', projectURL(ids.pid), { ...project, name: next });
      project.name = next;
      renderTree();
    } else {
      const found = findCall(ids);
      if (!found) return;
      const url = projectURL(ids.pid,
        `/services/${encodeURIComponent(ids.sid)}/operations/${encodeURIComponent(ids.oid)}/calls/${encodeURIComponent(ids.cid)}`);
      await apiJSON('PUT', url, { ...found.call, title: next });
      found.call.title = next;
      renderTree();
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
    toast('重命名失败: ' + error.message, true);
  }
}

async function handleTreeAction(data) {
  const action = data.act;
  try {
    if (action === 'add-svc') {
      const name = prompt('新服务名称：', 'NewService');
      if (name == null) return;
      await apiJSON('POST', projectURL(data.pid, '/services'), { name: name || 'Service' });
      await handlers.reload();
    } else if (action === 'del-svc') {
      if (!confirm('确定删除该服务及其下所有操作？')) return;
      await apiJSON('DELETE', projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}`));
      clearSelection(data.pid, data.sid);
      await handlers.reload();
    } else if (action === 'add-op') {
      const name = prompt('新操作名称：', 'NewOperation');
      if (name == null) return;
      await apiJSON('POST', projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations`), { name: name || 'Operation' });
      await handlers.reload();
    } else if (action === 'del-op') {
      if (!confirm('确定删除该操作及其下所有请求？')) return;
      await apiJSON('DELETE', projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations/${encodeURIComponent(data.oid)}`));
      clearSelection(data.pid, data.sid, data.oid);
      await handlers.reload();
    } else if (action === 'add-call') {
      // Create immediately with the backend's default title, then start an
      // in-place rename so the user can name it without an extra dialog.
      const url = projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations/${encodeURIComponent(data.oid)}/calls`);
      const created = await apiJSON('POST', url, { title: '' });
      // The new call must be visible for the inline rename input to exist; make
      // sure a previously collapsed operation is expanded before re-rendering.
      if (isCollapsed(data.oid)) toggleCollapsed(data.oid);
      await handlers.reload();
      const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: created.id };
      const found = findCall(ids);
      if (found) handlers.onSelect(found.call, 'call', { ids, parentOp: found.operation });
      beginRename('call', ids);
    } else if (action === 'del-call') {
      if (!confirm('确定删除该请求样例？')) return;
      await apiJSON('DELETE', projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations/${encodeURIComponent(data.oid)}/calls/${encodeURIComponent(data.cid)}`));
      clearSelection(data.pid, data.sid, data.oid);
      await handlers.reload();
    } else if (action === 'rename-call') {
      beginRename('call', { pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid });
    } else if (action === 'proxy-proj') {
      openProxyDialog(data.pid);
    } else if (action === 'del-proj') {
      const project = findProject(data.pid);
      if (!confirm('确定删除整个工程「' + ((project && project.name) || '') + '」？')) return;
      await apiJSON('DELETE', projectURL(data.pid));
      clearSelection(data.pid);
      await handlers.reload();
    } else if (action === 'rename-proj') {
      beginRename('proj', { pid: data.pid });
    } else if (action === 'export-proj') {
      const link = document.createElement('a');
      link.href = projectURL(data.pid, '/export');
      link.download = '';
      document.body.appendChild(link);
      link.click();
      link.remove();
      toast('已导出 SoapUI XML');
    }
  } catch (error) {
    toast(error.message, true);
  }
}
