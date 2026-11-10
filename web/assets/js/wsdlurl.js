// Remote WSDL import dialog.
//
// Lets the user import a WSDL by URL instead of downloading it first, which
// matters for intranet services that are only reachable from the machine
// running soap-web. The fetch happens server-side so the selected proxy
// settings apply (the browser would bypass them).

import { $ } from './dom.js';
import { apiJSON, path } from './api.js';
import { toast } from './toast.js';
import { state, workspaceLandedIn } from './state.js';
import { proxyLabel } from './proxy.js';
import { openModal } from './modal.js';

let reloadHandler = async () => {};
let onImported = () => {};
let closeDialog = null;
let busy = false;

export function initWsdlUrl({ reload, onImported: imported } = {}) {
  reloadHandler = reload || reloadHandler;
  onImported = imported || onImported;

  $('btnWsdlUrl').addEventListener('click', openWsdlUrlDialog);
  $('wsdlUrlClose').addEventListener('click', closeWsdlUrlDialog);
  $('wsdlUrlCancel').addEventListener('click', closeWsdlUrlDialog);
  $('wsdlUrlImport').addEventListener('click', submitWsdlUrl);
  $('wsdlUrlProxy').addEventListener('change', updateProxyCustomVisibility);

  // Enter submits from the URL field, matching how people expect a one-field
  // dialog to behave.
  $('wsdlUrlInput').addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      submitWsdlUrl();
    }
  });
}

function openWsdlUrlDialog() {
  clearError();
  $('wsdlUrlInput').value = '';
  populateProxyOptions();
  closeDialog = openModal('wsdlUrlOverlay', { fallbackFocus: 'btnWsdlUrl' });
  $('wsdlUrlInput').focus();
}

function closeWsdlUrlDialog() {
  if (closeDialog) closeDialog();
}

// populateProxyOptions fills the proxy dropdown. Direct connection is the
// default; system/custom cover the other local cases, and existing projects are
// offered as a shortcut to reuse their already-configured proxy.
function populateProxyOptions() {
  const select = $('wsdlUrlProxy');
  select.innerHTML = [
    '<option value="direct">直连（默认）</option>',
    '<option value="system">系统代理</option>',
    '<option value="custom">自定义代理</option>',
  ].join('');

  const projects = state.projects || [];
  if (projects.length) {
    const group = document.createElement('optgroup');
    group.label = '使用已有工程代理';
    projects.forEach((project) => {
      const option = document.createElement('option');
      option.value = 'project:' + project.id;
      option.textContent = '「' + (project.name || '未命名工程') + '」（' + proxyLabel(project.proxy) + '）';
      group.appendChild(option);
    });
    select.appendChild(group);
  }

  select.value = 'direct';
  $('wsdlUrlProxyType').value = 'http';
  $('wsdlUrlProxyHost').value = '';
  $('wsdlUrlProxyPort').value = '';
  $('wsdlUrlProxyUser').value = '';
  $('wsdlUrlProxyPass').value = '';
  updateProxyCustomVisibility();
}

function updateProxyCustomVisibility() {
  $('wsdlUrlCustom').classList.toggle('hidden', $('wsdlUrlProxy').value !== 'custom');
}

// readProxySelection maps the dropdown selection to the JSON fields the API
// expects: direct/system/custom send a proxy object, project:<id> sends the
// project id so the server reuses that project's settings.
function readProxySelection() {
  const value = $('wsdlUrlProxy').value;
  if (value === 'direct' || value === 'system') {
    return { proxy: { mode: value } };
  }
  if (value === 'custom') {
    const host = $('wsdlUrlProxyHost').value.trim();
    const port = parseInt($('wsdlUrlProxyPort').value, 10) || 0;
    if (!host) return { error: '请填写代理主机地址', focus: 'wsdlUrlProxyHost' };
    if (!port) return { error: '请填写代理端口', focus: 'wsdlUrlProxyPort' };
    return {
      proxy: {
        mode: 'custom',
        type: $('wsdlUrlProxyType').value,
        host,
        port,
        username: $('wsdlUrlProxyUser').value,
        password: $('wsdlUrlProxyPass').value,
      },
    };
  }
  if (value.startsWith('project:')) {
    return { projectId: value.slice('project:'.length) };
  }
  return { proxy: { mode: 'direct' } };
}

function showError(message) {
  const box = $('wsdlUrlError');
  box.textContent = message;
  box.classList.remove('hidden');
}

function clearError() {
  const box = $('wsdlUrlError');
  box.textContent = '';
  box.classList.add('hidden');
}

// normalizeURL trims the input and supplies http:// when the user pasted a
// bare host:port (a common way to copy an intranet WSDL address).
function normalizeURL(raw) {
  let value = raw.trim();
  if (!value) return { error: '请填写 WSDL 地址' };
  if (!/^https?:\/\//i.test(value)) {
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
      return { error: '仅支持 http:// 或 https:// 地址' };
    }
    value = 'http://' + value;
  }
  let parsed;
  try {
    parsed = new URL(value);
  } catch (error) {
    return { error: '地址格式无效，请检查后重试' };
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    return { error: '仅支持 http:// 或 https:// 地址' };
  }
  if (!parsed.hostname) return { error: '地址缺少主机名' };
  return { url: value };
}

async function submitWsdlUrl() {
  if (busy) return;

  const checked = normalizeURL($('wsdlUrlInput').value);
  if (checked.error) {
    showError(checked.error);
    $('wsdlUrlInput').focus();
    return;
  }

  clearError();
  const button = $('wsdlUrlImport');
  busy = true;
  button.disabled = true;
  button.textContent = '获取中…';
  $('wsdlUrlOverlay').setAttribute('aria-busy', 'true');

  try {
    const selection = readProxySelection();
    if (selection.error) {
      showError(selection.error);
      if (selection.focus) $(selection.focus).focus();
      return;
    }

    const payload = { url: checked.url };
    if (selection.proxy) payload.proxy = selection.proxy;
    if (selection.projectId) payload.projectId = selection.projectId;

    // The server allows itself 30s for the download; the client waits a little
    // longer so a slow-but-successful import is not reported as a timeout.
    const startedIn = state.workspaceId;
    const result = await apiJSON('POST', path('wsdl', 'fetch'), payload, { timeoutMs: 45_000 });
    const project = result && result.project;
    closeWsdlUrlDialog();
    await reloadHandler();
    // A slow fetch can land after another tab switched workspaces; the server
    // tells us which workspace actually received the project.
    const other = workspaceLandedIn(result, startedIn);
    if (other) {
      toast('WSDL 已导入到工作空间「' + other + '」，当前工作空间已切换', true);
      return;
    }
    toast('WSDL 已导入：' + ((project && project.name) || checked.url));
    if (project) onImported(project);
  } catch (error) {
    // Keep the dialog open so the user can correct the URL.
    showError(error.message);
    $('wsdlUrlInput').focus();
  } finally {
    busy = false;
    button.disabled = false;
    button.textContent = '获取并导入';
    $('wsdlUrlOverlay').removeAttribute('aria-busy');
  }
}
