// Project tree rendering and all tree-level CRUD actions.

import { $, escapeHtml, escapeAttr } from './dom.js';
import { apiJSON } from './api.js';
import { toast } from './toast.js';
import { state, findProject, findService, findOperation, findCall, toggleCollapsed } from './state.js';
import { openProxyDialog, proxyLabel } from './proxy.js';

let handlers = {
  onSelect: () => {},
  reload: async () => {},
};

export function initTree(options) {
  handlers = { ...handlers, ...options };
  $('tree').addEventListener('click', handleTreeClick);
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
  return `<div class="tree-node ${kind}${selected ? ' selected' : ''}" style="--depth:${depth}" data-kind="${kind}" role="treeitem" aria-selected="${selected ? 'true' : 'false'}"${attrHtml}>
    ${caretHtml(toggleKey, leaf)}
    <span class="lbl" title="${escapeAttr(label)}">${escapeHtml(label)}</span>
    ${tags}
    <span class="node-actions">${actions}</span>
  </div>`;
}

export function renderTree() {
  const tree = $('tree');
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
    const projectActions = `
      <button class="mini" title="代理设置" data-act="proxy-proj" data-pid="${escapeAttr(project.id)}">代理</button>
      <button class="mini" title="重命名工程" data-act="rename-proj" data-pid="${escapeAttr(project.id)}">✎</button>
      <button class="mini" title="导出 SoapUI XML" data-act="export-proj" data-pid="${escapeAttr(project.id)}">导出</button>
      <button class="mini" title="新建服务" data-act="add-svc" data-pid="${escapeAttr(project.id)}">+服务</button>
      <button class="mini danger" title="删除工程" data-act="del-proj" data-pid="${escapeAttr(project.id)}">✕</button>`;

    html += '<div class="tree-group">';
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
                  <button class="mini" title="重命名" data-act="rename-call" data-pid="${escapeAttr(project.id)}" data-sid="${escapeAttr(service.id)}" data-oid="${escapeAttr(operation.id)}" data-cid="${escapeAttr(call.id)}">✎</button>
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
}

function handleTreeClick(event) {
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
      const title = prompt('新请求样例标题：', '新请求');
      if (title == null) return;
      const url = projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations/${encodeURIComponent(data.oid)}/calls`);
      const created = await apiJSON('POST', url, { title: title || '新请求' });
      await handlers.reload();
      const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: created.id };
      const found = findCall(ids);
      if (found) handlers.onSelect(found.call, 'call', { ids, parentOp: found.operation });
    } else if (action === 'del-call') {
      if (!confirm('确定删除该请求样例？')) return;
      await apiJSON('DELETE', projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations/${encodeURIComponent(data.oid)}/calls/${encodeURIComponent(data.cid)}`));
      clearSelection(data.pid, data.sid, data.oid);
      await handlers.reload();
    } else if (action === 'rename-call') {
      const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid };
      const found = findCall(ids);
      if (!found) return;
      const current = found.call.title || '';
      const title = prompt('重命名请求样例：', current);
      if (title == null || title === current) return;
      const url = projectURL(data.pid, `/services/${encodeURIComponent(data.sid)}/operations/${encodeURIComponent(data.oid)}/calls/${encodeURIComponent(data.cid)}`);
      await apiJSON('PUT', url, { ...found.call, title });
      await handlers.reload();
    } else if (action === 'proxy-proj') {
      openProxyDialog(data.pid);
    } else if (action === 'del-proj') {
      const project = findProject(data.pid);
      if (!confirm('确定删除整个工程「' + ((project && project.name) || '') + '」？')) return;
      await apiJSON('DELETE', projectURL(data.pid));
      clearSelection(data.pid);
      await handlers.reload();
    } else if (action === 'rename-proj') {
      const project = findProject(data.pid);
      if (!project) return;
      const name = prompt('重命名工程：', project.name || '');
      if (name == null || name === project.name) return;
      await apiJSON('PUT', projectURL(data.pid), { ...project, name });
      await handlers.reload();
      toast('工程已重命名');
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
