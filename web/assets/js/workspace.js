// Workspace switcher: the top-bar control plus the create/rename/delete
// actions. The backend only exposes names and ids; file paths never reach the
// browser.
//
// Multi-tab safety lives here too: when the server reports that another tab
// switched workspaces (409 from any API call, or the 10-second health poll),
// this module marks the tab stale. It resyncs automatically only when nothing
// is unsaved and no request is in flight; otherwise the user has to confirm,
// because a switch discards the current editor contents.

import { $, escapeHtml, escapeAttr } from './dom.js';
import { state } from './state.js';
import { apiGet, apiJSON, setWorkspaceID, setWorkspaceMismatchHandler } from './api.js';
import { isDirty, isSending } from './editor.js';
import { toast } from './toast.js';

let onEnter = async () => {};
let menuOpen = false;
let cleanupMenu = null;

// initWorkspace wires the top-bar control. onEnter is called after the active
// workspace changes; the app uses it to reset the editor and reload projects.
export function initWorkspace(options = {}) {
  onEnter = options.onEnter || onEnter;
  setWorkspaceMismatchHandler(markStale);

  const button = $('workspaceBtn');
  const menu = $('workspaceMenu');
  if (button) {
    button.addEventListener('click', () => {
      if (state.workspaceStale) {
        syncNow(false);
        return;
      }
      if (menuOpen) closeMenu();
      else openMenu();
    });
  }
  if (menu) menu.addEventListener('click', handleMenuClick);
  renderButton();
}

// loadWorkspaceInfo fetches the workspace list and applies it.
export async function loadWorkspaceInfo() {
  const payload = await apiGet('/api/workspaces');
  applyWorkspace(payload);
  return payload;
}

// handleServerWorkspace is fed by the health heartbeat. It detects a switch
// made in another tab (or by editing settings.json and restarting).
export function handleServerWorkspace(info) {
  if (!info || !info.id) return;
  if (!state.workspaceId) {
    // Boot never learned its workspace (the backend was down). Adopt the
    // server's answer now that it is answering again.
    recoverWorkspace();
    return;
  }
  if (info.id === state.workspaceId) return;
  markStale(info);
}

// recoverWorkspace finishes a boot that could not reach the backend.
async function recoverWorkspace() {
  try {
    const payload = await apiGet('/api/workspaces');
    applyWorkspace(payload);
    await enterWorkspace();
    toast('已连接，当前工作空间：' + state.workspaceName);
  } catch (error) {
    // Stay offline; the next heartbeat retries.
  }
}

// applyWorkspace updates local state, the request header, and the top bar.
export function applyWorkspace(payload) {
  if (!payload || !payload.current || !payload.current.id) return;
  state.workspaces = payload.workspaces || [];
  state.workspaceId = payload.current.id;
  state.workspaceName = payload.current.name || '工作空间';
  state.workspaceStale = false;
  setWorkspaceID(state.workspaceId);
  renderButton();
  document.title = 'soap-web · ' + state.workspaceName;
}

// markStale records that the server moved on without this tab.
function markStale(info) {
  if (state.workspaceStale) return;
  state.workspaceStale = true;
  renderButton();
  const name = (info && info.name) || '其他工作空间';
  if (!isDirty() && !isSending()) {
    // Nothing to lose: catch up immediately.
    syncNow(true, name);
    return;
  }
  toast('工作空间已在其他窗口切换为「' + name + '」，点击顶部工作空间按钮同步', true);
}

// syncNow re-reads the workspace list and reloads the page state.
async function syncNow(auto, expectedName) {
  if (!auto && isDirty()
    && !confirm('当前请求有未保存的修改，同步工作空间会丢失这些修改。确定继续？')) {
    return;
  }
  try {
    const payload = await apiGet('/api/workspaces');
    applyWorkspace(payload);
    await enterWorkspace();
    const name = state.workspaceName || expectedName || '';
    toast(auto ? '工作空间已在其他窗口切换，已同步为：' + name : '已同步工作空间：' + name);
  } catch (error) {
    toast('同步工作空间失败: ' + error.message, true);
  }
}

async function enterWorkspace() {
  closeMenu();
  await onEnter();
}

async function switchTo(id) {
  if (!id || id === state.workspaceId) return;
  if (isDirty() && !confirm('当前请求有未保存的修改，切换工作空间会丢失这些修改。确定继续？')) {
    return;
  }
  try {
    const payload = await apiJSON('POST', '/api/workspaces/switch', { id });
    applyWorkspace(payload);
    await enterWorkspace();
    toast('已切换到工作空间：' + state.workspaceName);
  } catch (error) {
    toast('切换工作空间失败: ' + error.message, true);
  }
}

async function createWorkspace() {
  if (isDirty() && !confirm('当前请求有未保存的修改，新建工作空间会切换并丢失这些修改。确定继续？')) {
    return;
  }
  const suggested = '工作空间 ' + ((state.workspaces || []).length + 1);
  const name = prompt('新工作空间名称：', suggested);
  if (name == null) return;
  try {
    const payload = await apiJSON('POST', '/api/workspaces', { name });
    applyWorkspace(payload);
    await enterWorkspace();
    toast('已创建工作空间：' + state.workspaceName);
  } catch (error) {
    toast('新建工作空间失败: ' + error.message, true);
  }
}

async function renameWorkspace() {
  const current = state.workspaceName;
  const name = prompt('重命名工作空间：', current);
  if (name == null || name.trim() === current) return;
  try {
    const payload = await apiJSON('PATCH', '/api/workspaces/' + encodeURIComponent(state.workspaceId), { name });
    applyWorkspace(payload);
    toast('已重命名为：' + state.workspaceName);
  } catch (error) {
    toast('重命名失败: ' + error.message, true);
  }
}

async function deleteWorkspace() {
  if ((state.workspaces || []).length <= 1) {
    toast('至少需要保留一个工作空间', true);
    return;
  }
  const name = state.workspaceName;
  const dirtyNote = isDirty() ? '\n当前未保存的修改也会丢失。' : '';
  if (!confirm('确定删除工作空间「' + name + '」？\n其中的工程会连同文件一起移到 data/trash 备份。' + dirtyNote)) {
    return;
  }
  try {
    const payload = await apiJSON('DELETE', '/api/workspaces/' + encodeURIComponent(state.workspaceId));
    applyWorkspace(payload);
    await enterWorkspace();
    toast('已删除工作空间：' + name);
  } catch (error) {
    toast('删除工作空间失败: ' + error.message, true);
  }
}

// ---- popover menu ----

async function handleMenuClick(event) {
  const button = event.target.closest('button');
  if (!button || button.disabled) return;
  const wsID = button.dataset.ws;
  const action = button.dataset.act;
  closeMenu();
  if (wsID) {
    await switchTo(wsID);
    return;
  }
  if (action === 'create') await createWorkspace();
  else if (action === 'rename') await renameWorkspace();
  else if (action === 'delete') await deleteWorkspace();
}

function renderButton() {
  const button = $('workspaceBtn');
  const label = $('workspaceName');
  if (label) label.textContent = state.workspaceName || '工作空间';
  if (!button) return;
  button.classList.toggle('is-stale', !!state.workspaceStale);
  if (state.workspaceStale) {
    button.title = '工作空间已在其他窗口切换，点击同步';
  } else if (state.workspaceName) {
    button.title = '切换工作空间（' + state.workspaceName + '）';
  } else {
    button.title = '切换工作空间';
  }
}

function renderMenu() {
  const menu = $('workspaceMenu');
  if (!menu) return;
  const list = state.workspaces || [];
  const items = list.map((ws) => {
    const current = ws.id === state.workspaceId;
    return `<button type="button" class="row-menu-item ws-item" role="menuitemradio" aria-checked="${current}" data-ws="${escapeAttr(ws.id)}">`
      + `<span class="ws-check" aria-hidden="true">${current ? '✓' : ''}</span>`
      + `<span class="ws-name${current ? ' ws-current' : ''}">${escapeHtml(ws.name)}</span>`
      + '</button>';
  }).join('');
  menu.innerHTML = items
    + '<div class="row-menu-sep" role="separator"></div>'
    + '<button type="button" class="row-menu-item" role="menuitem" data-act="create">新建工作空间…</button>'
    + '<button type="button" class="row-menu-item" role="menuitem" data-act="rename">重命名当前…</button>'
    + `<button type="button" class="row-menu-item danger" role="menuitem" data-act="delete"${list.length <= 1 ? ' disabled' : ''}>删除当前…</button>`;
}

function place(menu, anchor) {
  // Measure at the origin so the size is trustworthy before positioning.
  menu.style.left = '0px';
  menu.style.top = '0px';
  const rect = anchor.getBoundingClientRect();
  const size = menu.getBoundingClientRect();
  const margin = 8;

  let left = rect.left;
  if (left + size.width > window.innerWidth - margin) left = window.innerWidth - margin - size.width;
  if (left < margin) left = margin;

  let top = rect.bottom + 4;
  if (top + size.height > window.innerHeight - margin) {
    const above = rect.top - size.height - 4;
    top = above >= margin ? above : window.innerHeight - margin - size.height;
  }
  if (top < margin) top = margin;

  menu.style.left = Math.round(left) + 'px';
  menu.style.top = Math.round(top) + 'px';
}

function openMenu() {
  const menu = $('workspaceMenu');
  const button = $('workspaceBtn');
  if (!menu || !button) return;
  renderMenu();
  menu.classList.remove('hidden');
  button.setAttribute('aria-expanded', 'true');
  place(menu, button);
  menuOpen = true;

  const onPointerDown = (event) => {
    if (menu.contains(event.target) || button.contains(event.target)) return;
    closeMenu();
  };
  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeMenu();
      button.focus();
      return;
    }
    const items = Array.from(menu.querySelectorAll('.row-menu-item:not(:disabled)'));
    if (!items.length) return;
    const index = items.indexOf(document.activeElement);
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      items[(index + 1) % items.length].focus();
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      items[(index - 1 + items.length) % items.length].focus();
    } else if (event.key === 'Tab') {
      closeMenu();
    }
  };
  const onViewportChange = () => closeMenu();

  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown, true);
  window.addEventListener('resize', onViewportChange);
  window.addEventListener('blur', onViewportChange);
  window.addEventListener('scroll', onViewportChange, true);

  cleanupMenu = () => {
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown, true);
    window.removeEventListener('resize', onViewportChange);
    window.removeEventListener('blur', onViewportChange);
    window.removeEventListener('scroll', onViewportChange, true);
  };

  const first = menu.querySelector('.row-menu-item');
  if (first) first.focus();
}

function closeMenu() {
  if (!menuOpen) return;
  menuOpen = false;
  if (cleanupMenu) {
    cleanupMenu();
    cleanupMenu = null;
  }
  const menu = $('workspaceMenu');
  const button = $('workspaceBtn');
  if (menu) {
    menu.classList.add('hidden');
    menu.innerHTML = '';
  }
  if (button) button.setAttribute('aria-expanded', 'false');
}
