// Per-project proxy settings dialog.

import { $ } from './dom.js';
import { apiJSON, projectPath } from './api.js';
import { toast } from './toast.js';
import { findProject } from './state.js';
import { openModal } from './modal.js';

let editingProjectID = null;
let reloadHandler = async () => {};
let closeDialog = null;
let saving = false;

export function proxyLabel(proxy) {
  if (!proxy || !proxy.mode || proxy.mode === 'system') return '系统';
  if (proxy.mode === 'direct') return '直连';
  return proxy.type === 'socks5' ? 'SOCKS5' : 'HTTP';
}

export function initProxy({ reload }) {
  reloadHandler = reload || reloadHandler;

  $('proxyClose').addEventListener('click', closeProxyDialog);
  $('proxyCancel').addEventListener('click', closeProxyDialog);
  $('proxySave').addEventListener('click', saveProxy);
  document.querySelectorAll('input[name="proxyMode"]').forEach((radio) => {
    radio.addEventListener('change', updateProxyCustomVisibility);
  });
}

export function openProxyDialog(projectID) {
  const project = findProject(projectID);
  if (!project) return;

  editingProjectID = projectID;
  $('proxyProjectName').textContent = '工程：' + (project.name || '');
  const proxy = project.proxy || { mode: 'system' };
  const mode = proxy.mode || 'system';
  document.querySelectorAll('input[name="proxyMode"]').forEach((radio) => {
    radio.checked = radio.value === mode;
  });
  $('proxyType').value = proxy.type || 'http';
  $('proxyHost').value = proxy.host || '';
  $('proxyPort').value = proxy.port || '';
  $('proxyUser').value = proxy.username || '';
  $('proxyPass').value = proxy.password || '';
  updateProxyCustomVisibility();
  closeDialog = openModal('proxyOverlay', { fallbackFocus: 'btnNewProject' });

  // Focus something that is actually visible. #proxyHost lives inside
  // #proxyCustom, which is display:none unless mode is "custom"; focusing a
  // hidden element is a no-op and would leave focus outside an aria-modal.
  const target = mode === 'custom'
    ? $('proxyHost')
    : (document.querySelector('input[name="proxyMode"]:checked') || $('proxySave'));
  if (target) target.focus();
}

function closeProxyDialog() {
  editingProjectID = null;
  if (closeDialog) closeDialog();
}

function updateProxyCustomVisibility() {
  const checked = document.querySelector('input[name="proxyMode"]:checked');
  const mode = checked ? checked.value : 'system';
  $('proxyCustom').classList.toggle('hidden', mode !== 'custom');
}

function readProxyForm() {
  const checked = document.querySelector('input[name="proxyMode"]:checked');
  const mode = checked ? checked.value : 'system';
  if (mode !== 'custom') return { mode };
  return {
    mode: 'custom',
    type: $('proxyType').value,
    host: $('proxyHost').value.trim(),
    port: parseInt($('proxyPort').value, 10) || 0,
    username: $('proxyUser').value,
    password: $('proxyPass').value,
  };
}

async function saveProxy() {
  if (!editingProjectID || saving) return;
  const project = findProject(editingProjectID);
  if (!project) return;

  const proxy = readProxyForm();
  if (proxy.mode === 'custom') {
    if (!proxy.host) { toast('请填写代理主机地址', true); return; }
    if (!proxy.port) { toast('请填写代理端口', true); return; }
  }

  const saveBtn = $('proxySave');
  saving = true;
  saveBtn.disabled = true;
  try {
    // Narrow endpoint: only the proxy field is written. Sending the whole
    // project would overwrite concurrent changes made in another tab.
    await apiJSON('PUT', projectPath(project.id, 'proxy'), proxy);
    // Close the dialog before reloading: reload re-renders the tree, which
    // destroys the button that opened the dialog and breaks focus restore.
    closeProxyDialog();
    await reloadHandler();
    toast('代理设置已保存');
  } catch (error) {
    toast('保存失败: ' + error.message, true);
  } finally {
    saving = false;
    saveBtn.disabled = false;
  }
}
