// Per-project proxy settings dialog.

import { $ } from './dom.js';
import { apiJSON } from './api.js';
import { toast } from './toast.js';
import { findProject } from './state.js';

let editingProjectID = null;
let reloadHandler = async () => {};
let lastFocus = null;
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
  $('proxyOverlay').addEventListener('click', (event) => {
    if (event.target === $('proxyOverlay')) closeProxyDialog();
  });
  document.querySelectorAll('input[name="proxyMode"]').forEach((radio) => {
    radio.addEventListener('change', updateProxyCustomVisibility);
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !$('proxyOverlay').classList.contains('hidden')) {
      closeProxyDialog();
    }
  });
}

export function openProxyDialog(projectID) {
  const project = findProject(projectID);
  if (!project) return;

  // Remember where focus came from so closing the dialog can restore it. The
  // tree is re-rendered on save, which destroys the original button, so the
  // restore is best-effort and falls back to a stable element.
  lastFocus = document.activeElement;

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
  $('proxyOverlay').classList.remove('hidden');

  // Focus something that is actually visible. #proxyHost lives inside
  // #proxyCustom, which is display:none unless mode is "custom"; focusing a
  // hidden element is a no-op and would leave focus outside an aria-modal.
  const target = mode === 'custom'
    ? $('proxyHost')
    : (document.querySelector('input[name="proxyMode"]:checked') || $('proxySave'));
  if (target) target.focus();
}

function closeProxyDialog() {
  $('proxyOverlay').classList.add('hidden');
  editingProjectID = null;
  // Restore focus if the originating element still exists; otherwise move it
  // somewhere sane instead of leaving it on <body>.
  const back = lastFocus && lastFocus.isConnected ? lastFocus : $('btnNewProject');
  lastFocus = null;
  if (back && back.focus) back.focus();
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
    await apiJSON('PUT', '/api/projects/' + encodeURIComponent(project.id) + '/proxy', proxy);
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
