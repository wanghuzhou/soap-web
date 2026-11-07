// Application entry point: wires the feature modules together.

import { $ } from './dom.js';
import { state, initCollapsed, findService, findOperation, findCall } from './state.js';
import { apiGet, apiHealth, apiJSON, apiUpload } from './api.js';
import { toast } from './toast.js';
import { initTheme } from './theme.js';
import { initTree, renderTree, syncSelection } from './tree.js';
import { initEditor, loadNode, updateEditButtons, isDirty, resetEditor, cancelSend } from './editor.js';
import { initResponse } from './response.js';
import { initProxy } from './proxy.js';
import { initLayout } from './layout.js';
import { initWsdlUrl } from './wsdlurl.js';
import { initWorkspace, loadWorkspaceInfo, handleServerWorkspace } from './workspace.js';

let pendingFileMode = null;

// The badge reflects the last /api/health probe. Without this heartbeat it
// would keep saying "就绪" forever once the page had loaded, even after the
// backend was stopped.
const HEALTH_INTERVAL_MS = 10000;

function setServerState(ok, text) {
  const badge = $('serverState');
  if (!badge) return;
  badge.classList.toggle('is-error', !ok);
  const label = badge.querySelector('span:last-child');
  if (label) label.textContent = text;
  const now = new Date().toLocaleTimeString('zh-CN', { hour12: false });
  badge.title = '本地服务状态：' + text + '（最近检查 ' + now + '，点击立即重试）';
}

async function checkHealth() {
  if (document.hidden) return;
  const health = await apiHealth();
  const ok = !!health;
  setServerState(ok, ok ? '就绪' : '已断开');
  // The heartbeat doubles as the workspace-change detector for other tabs.
  if (ok) handleServerWorkspace(health.workspace);
}

function startHealthMonitor() {
  checkHealth();
  setInterval(checkHealth, HEALTH_INTERVAL_MS);
  // Re-check as soon as the tab becomes visible again instead of waiting for
  // the next tick, and never claim "就绪" while the machine is offline.
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) checkHealth();
  });
  window.addEventListener('focus', checkHealth);
  window.addEventListener('online', checkHealth);
  window.addEventListener('offline', () => setServerState(false, '已断开'));
  $('serverState').addEventListener('click', checkHealth);
}

async function loadProjects(selectFirst) {
  try {
    state.projects = await apiGet('/api/projects');
    setServerState(true, '就绪');
    renderTree();
    if (selectFirst && state.projects.length) {
      selectFirstInProject(state.projects[0]);
    }
  } catch (error) {
    // A workspace mismatch is not an outage: workspace.js marks the tab stale
    // and offers a sync, and its own toast explains the situation.
    if (error.code === 'workspace_mismatch') return;
    setServerState(false, '连接失败');
    toast('加载工程失败: ' + error.message, true);
  }
}

// reload re-fetches projects and re-syncs the UI.
//
// It serves two distinct situations:
//
//   - structural change (a node was added, deleted or renamed): the tree must be
//     rebuilt, otherwise a newly created request never appears until the page is
//     refreshed;
//   - pure refresh (proxy saved, selection moved): the tree only needs its
//     selection marker updated.
//
// The tree rebuild is therefore unconditional here, while loadNode() stays
// selection-only. Splitting them the other way round (rebuild inside loadNode)
// would reintroduce the full-tree re-parse on every click, and skipping the
// rebuild entirely breaks node creation — both mistakes are easy to make, so
// the responsibility is stated explicitly at each call site.
async function reload() {
  const selected = state.selected;
  try {
    state.projects = await apiGet('/api/projects');
    setServerState(true, '就绪');
  } catch (error) {
    if (error.code === 'workspace_mismatch') return;
    setServerState(false, '连接失败');
    toast(error.message, true);
    return;
  }

  // The project list changed: rebuild the tree first so the rows the user just
  // created exist, then re-apply selection onto the fresh DOM.
  renderTree();

  if (!selected) {
    updateEditButtons();
    return;
  }

  if (selected.kind === 'call') {
    const found = findCall(selected.ids);
    if (!found) {
      state.selected = null;
      updateEditButtons();
      return;
    }
    // Do not overwrite in-progress edits with the server copy: reload() runs
    // after unrelated actions too (adding a service, deleting an operation,
    // saving proxy settings).
    if (isDirty()) {
      state.selected = {
        kind: 'call',
        node: found.call,
        ids: selected.ids,
        parentOp: found.operation,
      };
      updateEditButtons();
      syncSelection();
      return;
    }
    loadNode(found.call, 'call', { ids: selected.ids, parentOp: found.operation });
    return;
  }

  if (selected.kind === 'op') {
    const found = findOperation(selected.ids.pid, selected.ids.sid, selected.ids.oid);
    if (found) {
      loadNode(found.operation, 'op', { ids: selected.ids });
      return;
    }
  } else if (selected.kind === 'svc') {
    const found = findService(selected.ids.pid, selected.ids.sid);
    if (found) {
      loadNode(found.service, 'svc', { ids: selected.ids });
      return;
    }
  }

  // The selected node is gone (it was deleted, or its parent was).
  state.selected = null;
  updateEditButtons();
  syncSelection();
}

function selectFirstInProject(project) {
  const service = (project.services || [])[0];
  const operation = service && (service.operations || [])[0];
  if (operation && operation.calls && operation.calls.length) {
    loadNode(operation.calls[0], 'call', {
      ids: { pid: project.id, sid: service.id, oid: operation.id, cid: operation.calls[0].id },
      parentOp: operation,
    });
  } else if (operation) {
    loadNode(operation, 'op', { ids: { pid: project.id, sid: service.id, oid: operation.id } });
  } else {
    renderTree();
  }
}

// selectProject focuses a freshly imported project by id. The caller must have
// reloaded state.projects first, otherwise the project is not in the lookup.
function selectProject(project) {
  const found = (state.projects || []).find((item) => item.id === (project && project.id));
  if (found) selectFirstInProject(found);
}

async function createProject() {
  const name = prompt('新工程名称：', '新工程');
  if (name == null) return;
  try {
    const project = await apiJSON('POST', '/api/projects', { name: name || '新工程', services: [] });
    await loadProjects(false);
    toast('已创建工程：' + project.name);
  } catch (error) {
    toast(error.message, true);
  }
}

function pickFile(mode) {
  pendingFileMode = mode;
  $('fileInput').value = '';
  $('fileInput').click();
}

// workspaceName resolves an id to its display name for import notices.
function workspaceName(id) {
  const found = (state.workspaces || []).find((item) => item.id === id);
  return found ? found.name : '其他工作空间';
}

async function importSoapUI(file) {
  const startedIn = state.workspaceId;
  try {
    const result = await apiUpload('/api/import/soapui', file);
    const saved = (result && result.project) || result;
    await loadProjects(false);
    // A slow upload can finish after another tab switched workspaces. Compare
    // with the workspace the import STARTED in: loadProjects() may already have
    // synced state.workspaceId to the new one.
    if (result && result.workspaceId && result.workspaceId !== startedIn) {
      toast('已导入到工作空间「' + workspaceName(result.workspaceId) + '」，当前工作空间已切换', true);
      return;
    }
    toast('已导入并保存工程：' + (saved.name || file.name));
    const project = (state.projects || []).find((item) => item.id === saved.id);
    if (project) selectFirstInProject(project);
  } catch (error) {
    toast(error.message, true);
  }
}

async function parseWSDL(file) {
  const startedIn = state.workspaceId;
  try {
    const result = await apiUpload('/api/wsdl/parse', file);
    await loadProjects(false);
    if (result && result.workspaceId && result.workspaceId !== startedIn) {
      toast('WSDL 已保存到工作空间「' + workspaceName(result.workspaceId) + '」，当前工作空间已切换', true);
      return;
    }
    toast('WSDL 解析完成并已保存工程');
    const project = result.project && (state.projects || []).find((item) => item.id === result.project.id);
    if (project) selectFirstInProject(project);
  } catch (error) {
    toast(error.message, true);
  }
}

async function boot() {
  initTheme();
  initResponse();

  initTree({ onSelect: loadNode, reload });
  initEditor({ reload });
  initProxy({ reload });
  initLayout();
  initWsdlUrl({ reload, onImported: selectProject });
  initWorkspace({
    // Called after a switch/create/delete/external sync: the previous
    // selection, response and collapse state belong to another workspace.
    onEnter: async () => {
      cancelSend();
      resetEditor();
      initCollapsed(state.workspaceId);
      await loadProjects(false);
    },
  });

  $('btnNewProject').addEventListener('click', createProject);
  $('btnNewProjectSide').addEventListener('click', createProject);
  $('btnImport').addEventListener('click', () => pickFile('soapui'));
  $('btnWsdl').addEventListener('click', () => pickFile('wsdl'));

  $('fileInput').addEventListener('change', (event) => {
    const file = event.target.files[0];
    if (!file) return;
    if (pendingFileMode === 'soapui') importSoapUI(file);
    else if (pendingFileMode === 'wsdl') parseWSDL(file);
  });

  updateEditButtons();

  // The workspace must be known before the first project fetch: its id is sent
  // as a header so the server can reject a stale tab, and the collapse map is
  // keyed by it.
  try {
    await loadWorkspaceInfo();
    initCollapsed(state.workspaceId);
  } catch (error) {
    setServerState(false, '连接失败');
    toast('加载工作空间失败: ' + error.message, true);
  }

  startHealthMonitor();
  await loadProjects(true);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot);
} else {
  boot();
}
