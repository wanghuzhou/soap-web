// Application entry point: wires the feature modules together.

import { $ } from './dom.js';
import { state, initCollapsed, findService, findOperation, findCall } from './state.js';
import { apiGet, apiJSON, apiUpload } from './api.js';
import { toast } from './toast.js';
import { initTheme } from './theme.js';
import { initTree, renderTree } from './tree.js';
import { initEditor, loadNode, updateEditButtons } from './editor.js';
import { initResponse } from './response.js';
import { initProxy } from './proxy.js';
import { initLayout } from './layout.js';

let pendingFileMode = null;

function setServerState(ok, text) {
  const badge = $('serverState');
  if (!badge) return;
  badge.classList.toggle('is-error', !ok);
  const label = badge.querySelector('span:last-child');
  if (label) label.textContent = text;
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
    setServerState(false, '连接失败');
    toast('加载工程失败: ' + error.message, true);
  }
}

async function reload() {
  const selected = state.selected;
  try {
    state.projects = await apiGet('/api/projects');
    setServerState(true, '就绪');
  } catch (error) {
    setServerState(false, '连接失败');
    toast(error.message, true);
    return;
  }

  if (selected) {
    if (selected.kind === 'call') {
      const found = findCall(selected.ids);
      if (found) {
        loadNode(found.call, 'call', { ids: selected.ids, parentOp: found.operation });
        return;
      }
    } else if (selected.kind === 'op') {
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
    state.selected = null;
  }
  updateEditButtons();
  renderTree();
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

async function importSoapUI(file) {
  try {
    const saved = await apiUpload('/api/import/soapui', file);
    await loadProjects(false);
    toast('已导入并保存工程：' + (saved.name || file.name));
    const project = (state.projects || []).find((item) => item.id === saved.id);
    if (project) selectFirstInProject(project);
  } catch (error) {
    toast(error.message, true);
  }
}

async function parseWSDL(file) {
  try {
    const result = await apiUpload('/api/wsdl/parse', file);
    await loadProjects(false);
    toast('WSDL 解析完成并已保存工程');
    const project = result.project && (state.projects || []).find((item) => item.id === result.project.id);
    if (project) selectFirstInProject(project);
  } catch (error) {
    toast(error.message, true);
  }
}

function boot() {
  initCollapsed();
  initTheme();
  initResponse();

  initTree({ onSelect: loadNode, reload });
  initEditor({ reload });
  initProxy({ reload });
  initLayout();

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
  loadProjects(true);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot);
} else {
  boot();
}
