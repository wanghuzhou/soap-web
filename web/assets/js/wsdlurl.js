// Remote WSDL import dialog.
//
// Lets the user import a WSDL by URL instead of downloading it first, which
// matters for intranet services that are only reachable from the machine
// running soap-web. The fetch happens server-side so the selected project's
// proxy settings apply (the browser would bypass them).

import { $ } from './dom.js';
import { apiJSON } from './api.js';
import { toast } from './toast.js';
import { state } from './state.js';
import { proxyLabel } from './proxy.js';

let reloadHandler = async () => {};
let onImported = () => {};
let lastFocus = null;
let busy = false;

export function initWsdlUrl({ reload, onImported: imported } = {}) {
  reloadHandler = reload || reloadHandler;
  onImported = imported || onImported;

  $('btnWsdlUrl').addEventListener('click', openWsdlUrlDialog);
  $('wsdlUrlClose').addEventListener('click', closeWsdlUrlDialog);
  $('wsdlUrlCancel').addEventListener('click', closeWsdlUrlDialog);
  $('wsdlUrlImport').addEventListener('click', submitWsdlUrl);
  $('wsdlUrlOverlay').addEventListener('click', (event) => {
    if (event.target === $('wsdlUrlOverlay')) closeWsdlUrlDialog();
  });

  // Enter submits from the URL field, matching how people expect a one-field
  // dialog to behave.
  $('wsdlUrlInput').addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      submitWsdlUrl();
    }
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !$('wsdlUrlOverlay').classList.contains('hidden')) {
      closeWsdlUrlDialog();
    }
  });
}

function openWsdlUrlDialog() {
  lastFocus = document.activeElement;
  clearError();
  $('wsdlUrlInput').value = '';
  populateProxyOptions();
  $('wsdlUrlOverlay').classList.remove('hidden');
  $('wsdlUrlInput').focus();
}

function closeWsdlUrlDialog() {
  $('wsdlUrlOverlay').classList.add('hidden');
  const back = lastFocus && lastFocus.isConnected ? lastFocus : $('btnWsdlUrl');
  lastFocus = null;
  if (back && back.focus) back.focus();
}

// populateProxyOptions lets the user borrow an existing project's proxy for
// the download. Empty value means "system proxy".
function populateProxyOptions() {
  const select = $('wsdlUrlProxy');
  const current = select.value;
  select.innerHTML = '<option value="">系统代理</option>';
  (state.projects || []).forEach((project) => {
    const option = document.createElement('option');
    option.value = project.id;
    option.textContent = '使用「' + (project.name || '未命名工程') + '」的代理（' + proxyLabel(project.proxy) + '）';
    select.appendChild(option);
  });
  if (current) select.value = current;
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
    const payload = { url: checked.url };
    const proxyId = $('wsdlUrlProxy').value;
    if (proxyId) payload.projectId = proxyId;

    // The server allows itself 30s for the download; the client waits a little
    // longer so a slow-but-successful import is not reported as a timeout.
    const result = await apiJSON('POST', '/api/wsdl/fetch', payload, { timeoutMs: 45_000 });
    const project = result && result.project;
    closeWsdlUrlDialog();
    await reloadHandler();
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
