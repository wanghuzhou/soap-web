// Workspace JSON import/export. Export offers an option to keep per-project
// proxy settings (off by default); import creates a new workspace from a
// previously exported array.

import { $ } from './dom.js';
import { state } from './state.js';
import { apiJSON, apiUpload, path } from './api.js';
import { isDirty } from './editor.js';
import { applyWorkspace, enterWorkspace } from './workspace.js';
import { toast } from './toast.js';
import { openModal } from './modal.js';

let busy = false;
let closeDialog = null;

export function initWorkspaceIO() {
  $('exportClose').addEventListener('click', closeExportDialog);
  $('exportCancel').addEventListener('click', closeExportDialog);
  $('exportConfirm').addEventListener('click', confirmExport);
  $('workspaceFileInput').addEventListener('change', handleImportFile);
}

// openExportDialog is wired to the workspace menu. The proxy checkbox always
// starts unchecked so credentials are not exported by accident.
export function openExportDialog() {
  if (busy) return;
  $('exportKeepProxy').checked = false;
  closeDialog = openModal('exportOverlay', { fallbackFocus: 'workspaceBtn' });
  $('exportConfirm').focus();
}

function closeExportDialog() {
  if (closeDialog) closeDialog();
}

async function confirmExport() {
  if (busy) return;
  busy = true;
  const button = $('exportConfirm');
  button.disabled = true;
  const keepProxy = $('exportKeepProxy').checked;
  try {
    const projects = await apiJSON('POST', path('workspaces', 'export'), { keepProxy });
    downloadJSON(projects, state.workspaceName);
    closeExportDialog();
    toast('已导出工作空间：' + state.workspaceName);
  } catch (error) {
    toast('导出失败: ' + error.message, true);
  } finally {
    busy = false;
    button.disabled = false;
  }
}

// downloadJSON turns the exported array into a Blob download. The backend
// returns a bare array so the file can be imported back directly.
function downloadJSON(projects, name) {
  const blob = new Blob([JSON.stringify(projects, null, 2) + '\n'], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = fileSafeName(name) + '.json';
  document.body.appendChild(link);
  link.click();
  link.remove();
  // Revoking immediately can cancel the download in some browsers.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function fileSafeName(name) {
  return String(name || 'workspace').replace(/[\\/:*?"<>|]/g, '_').trim() || 'workspace';
}

// pickWorkspaceFile is wired to the workspace menu.
export function pickWorkspaceFile() {
  if (busy) return;
  $('workspaceFileInput').value = '';
  $('workspaceFileInput').click();
}

async function handleImportFile(event) {
  const file = event.target.files && event.target.files[0];
  if (!file || busy) return;
  if (isDirty() && !confirm('当前请求有未保存的修改，导入工作空间会切换并丢失这些修改。确定继续？')) {
    return;
  }

  const suggested = file.name.replace(/\.json$/i, '') || '导入的工作空间';
  const name = prompt('导入为工作空间，名称：', suggested);
  if (name == null) return;

  busy = true;
  try {
    const payload = await apiUpload(
      path('workspaces', 'import') + '?name=' + encodeURIComponent(name),
      file,
    );
    applyWorkspace(payload);
    await enterWorkspace();
    toast('已导入工作空间：' + state.workspaceName);
  } catch (error) {
    toast('导入工作空间失败: ' + error.message, true);
  } finally {
    busy = false;
  }
}
