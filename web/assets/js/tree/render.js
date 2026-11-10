// Tree rendering: row HTML, collapse visibility and selection markers.

import { $, escapeHtml, escapeAttr } from '../dom.js';
import { state, toggleCollapsed, pruneCollapsed } from '../state.js';
import { proxyLabel } from '../proxy.js';
import { cancelActiveRename } from '../inlineedit.js';
import { getQuery, applyTreeFilter } from './search.js';

function isCollapsed(key) {
  return !!state.collapsed[key];
}

function caretHtml(key, leaf) {
  if (leaf) return '<span class="caret leaf" aria-hidden="true"></span>';
  const glyph = isCollapsed(key) ? '▸' : '▾';
  return `<span class="caret" data-toggle="${escapeAttr(key)}" title="展开/收起">${glyph}</span>`;
}

// isSelected reports whether a node matches the current selection.
function isSelected(kind, ids) {
  const selected = state.selected;
  if (!selected || selected.kind !== kind) return false;
  const cur = selected.ids || {};
  return (!ids.pid || cur.pid === ids.pid)
    && (!ids.sid || cur.sid === ids.sid)
    && (!ids.oid || cur.oid === ids.oid)
    && (!ids.cid || cur.cid === ids.cid);
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

    (project.services || []).forEach((service) => {
      const serviceActions = `
        <button class="mini" title="新建操作" data-act="add-op" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}">+操作</button>
        <button class="mini danger" title="删除服务" data-act="del-svc" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}">✕</button>`;
      html += rowHtml({
        kind: 'svc',
        depth: 1,
        attrs: { pid: project.id, sid: service.id },
        label: service.name || 'service',
        tags: '<span class="tag">service</span>',
        actions: serviceActions,
        toggleKey: service.id,
        leaf: !(service.operations && service.operations.length),
        selected: isSelected('svc', { pid: project.id, sid: service.id }),
      });

      (service.operations || []).forEach((operation) => {
        const callCount = (operation.calls || []).length;
        const operationActions = `
          <button class="mini" title="新建请求样例" data-act="add-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}">+请求</button>
          <button class="mini danger" title="删除操作" data-act="del-op" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}">✕</button>`;
        html += rowHtml({
          kind: 'op',
          depth: 2,
          attrs: { pid: project.id, sid: service.id, oid: operation.id },
          label: operation.name || 'operation',
          tags: `<span class="tag">${callCount ? callCount + ' 请求' : 'op'}</span>`,
          actions: operationActions,
          toggleKey: operation.id,
          leaf: callCount === 0,
          selected: isSelected('op', { pid: project.id, sid: service.id, oid: operation.id }),
        });

        (operation.calls || []).forEach((call) => {
          const callActions = `
            <button class="mini" title="复制该请求样例" data-act="duplicate-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}" data-cid="${escapeAttr(call.id)}">⧉</button>
            <button class="mini" title="重命名请求（就地编辑）" data-act="rename-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}" data-cid="${escapeAttr(call.id)}">✎</button>
            <button class="mini danger" title="删除请求" data-act="del-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}" data-cid="${escapeAttr(call.id)}">✕</button>`;
          html += rowHtml({
            kind: 'call',
            depth: 3,
            attrs: { pid: project.id, sid: service.id, oid: operation.id, cid: call.id },
            label: call.title || '请求',
            actions: callActions,
            toggleKey: call.id,
            leaf: true,
            selected: isSelected('call', {
              pid: project.id, sid: service.id, oid: operation.id, cid: call.id,
            }),
          });
        });
      });
    });
    html += '</div>';
  });

  tree.innerHTML = html;
  pruneCollapsed(collectKeys(projects));
  // A filter outranks the collapse state, so a re-render has to re-apply it
  // rather than fall back to the plain collapsed view.
  if (getQuery()) applyTreeFilter();
  else applyCollapseVisibility();
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

// applyCollapseVisibility hides rows whose ancestor is collapsed, using only
// class changes. Toggling no longer rebuilds the tree, so focus, the inline
// rename input and scroll position all survive.
export function applyCollapseVisibility() {
  const tree = $('tree');
  if (!tree) return;
  tree.querySelectorAll('.tree-node').forEach((row) => {
    const d = row.dataset;
    let hidden = false;
    if (d.kind === 'svc') {
      hidden = !!state.collapsed[d.pid];
    } else if (d.kind === 'op') {
      hidden = !!state.collapsed[d.pid] || !!state.collapsed[d.sid];
    } else if (d.kind === 'call') {
      hidden = !!state.collapsed[d.pid] || !!state.collapsed[d.sid] || !!state.collapsed[d.oid];
    }
    row.classList.toggle('is-hidden', hidden);
    if (hidden) row.tabIndex = -1;
  });
  syncTabStop();
}

// updateCaret refreshes one node's arrow and aria-expanded state in place.
export function updateCaret(key) {
  const tree = $('tree');
  if (!tree) return;
  const collapsed = isCollapsed(key);
  tree.querySelectorAll('.caret[data-toggle]').forEach((caret) => {
    if (caret.dataset.toggle !== key) return;
    caret.textContent = collapsed ? '▸' : '▾';
    const row = caret.closest('.tree-node');
    if (row) row.setAttribute('aria-expanded', String(!collapsed));
  });
}

// toggleNode collapses or expands one node without rebuilding the tree.
// While a filter is active the filter decides visibility, so toggling only
// updates the caret: hiding a result's parent would make it unreachable.
export function toggleNode(key) {
  toggleCollapsed(key);
  updateCaret(key);
  if (getQuery()) applyTreeFilter();
  else applyCollapseVisibility();
}

// updateRowLabel rewrites one row's visible text and tooltip in place. A rename
// changes no structure, so rebuilding the whole tree would only cost the
// user's focus, scroll position and in-place edit state.
export function updateRowLabel(kind, ids, label) {
  const tree = $('tree');
  if (!tree) return;
  for (const row of tree.querySelectorAll('.tree-node')) {
    const d = row.dataset;
    if (d.kind !== kind || d.pid !== ids.pid) continue;
    if (ids.sid && d.sid !== ids.sid) continue;
    if (ids.oid && d.oid !== ids.oid) continue;
    if (ids.cid && d.cid !== ids.cid) continue;
    const span = row.querySelector('.lbl');
    if (span) {
      span.textContent = label;
      span.title = label;
    }
    return;
  }
}

// syncTabStop keeps exactly one visible row reachable with Tab.
function syncTabStop() {
  const tree = $('tree');
  if (!tree) return;
  const rows = Array.from(tree.querySelectorAll('.tree-node'))
    .filter((row) => !row.classList.contains('is-hidden'));
  if (!rows.length) return;
  const active = rows.find((row) => row.classList.contains('selected')) || rows[0];
  tree.querySelectorAll('.tree-node').forEach((row) => { row.tabIndex = -1; });
  active.tabIndex = 0;
}

// syncSelection moves the .selected marker without rebuilding the tree, which
// would re-parse the whole tree and drop focus from the clicked row.
export function syncSelection() {
  const tree = $('tree');
  if (!tree) return;
  const selected = state.selected;

  tree.querySelectorAll('.tree-node').forEach((row) => {
    const match = selected && isSelected(row.dataset.kind, {
      pid: row.dataset.pid,
      sid: row.dataset.sid,
      oid: row.dataset.oid,
      cid: row.dataset.cid,
    });
    row.classList.toggle('selected', !!match);
    row.setAttribute('aria-selected', match ? 'true' : 'false');
  });
  syncTabStop();
}

export { isCollapsed };
