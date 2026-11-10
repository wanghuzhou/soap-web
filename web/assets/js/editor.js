// Request editor: transport fields, highlighted textarea, send/save/delete.

import { $ } from './dom.js';
import { state } from './state.js';
import { apiJSON, apiSend, callPath } from './api.js';
import { toast } from './toast.js';
import { prettyXML, minifyXML, normalizeNewlines } from './xml.js';
import { syncSelection, deleteNode } from './tree.js';
import { STORAGE_KEY } from './constants.js';
import { isRenaming } from './inlineedit.js';
import { renderResponse } from './response.js';
import { initTextarea, renderHighlight } from './editor/textarea.js';

let reloadHandler = async () => {};

// Send state: `sending` gates the button and Ctrl+Enter, `sendSeq` discards
// late responses, and `abortController` supports cancel. All three are needed
// so a response for request A can never render onto request B's panel.
let sending = false;
let sendSeq = 0;
let abortController = null;
let loadedSnapshot = '';

// saving guards the write paths so a double-click cannot fire two PUTs.
let saving = false;

// SEND_TIMEOUT_MS is the client-side request timeout. The server enforces its
// own copy; this bound just keeps the UI from waiting forever.
const SEND_TIMEOUT_MS = 60_000;

export function initEditor({ reload }) {
  reloadHandler = reload || reloadHandler;

  $('btnFormat').addEventListener('click', formatBody);
  $('btnMinify').addEventListener('click', minifyBody);
  $('btnSend').addEventListener('click', send);
  $('btnSaveCall').addEventListener('click', saveCurrentCall);
  $('btnDeleteNode').addEventListener('click', deleteCurrentNode);
  $('contentType').addEventListener('change', updateReqBadge);

  initTextarea();

  document.addEventListener('keydown', (event) => {
    // Any open modal captures global shortcuts: Ctrl+Enter inside the WSDL URL
    // dialog must not also send the request currently loaded in the editor.
    const modalOpen = !!document.querySelector('.modal-overlay:not(.hidden)');
    // While a tree row is being renamed inline, global shortcuts must not fire
    // (the editor also stops its own keydown from bubbling, this is a guard).
    if (modalOpen || isRenaming()) return;
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault();
      send();
    }
    // Ctrl/Cmd+S saves the current request sample, mirroring the 保存请求
    // button. preventDefault stops the browser's "save page" dialog.
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      saveCurrentCall();
    }
    if ((event.ctrlKey || event.metaKey) && event.shiftKey && (event.key === 'F' || event.key === 'f')) {
      event.preventDefault();
      formatBody();
    }
  });

  initAutoFormat();
  updateEditButtons();
  renderHighlight();
}

// initAutoFormat restores the persisted preference and keeps it in sync.
// Defaulting to off matters: auto-formatting rewrites the payload as it loads,
// so the user should opt in rather than discover their XML was reformatted.
function initAutoFormat() {
  const box = $('autoFormat');
  if (!box) return;
  try {
    const saved = localStorage.getItem(STORAGE_KEY.AUTO_FORMAT);
    box.checked = saved === '1';
  } catch (e) { /* storage may be unavailable */ }
  box.addEventListener('change', () => {
    try { localStorage.setItem(STORAGE_KEY.AUTO_FORMAT, box.checked ? '1' : '0'); } catch (e) { /* ignore */ }
  });
}

function setRequestBody(value, doFormat) {
  const textarea = $('requestBody');
  let content = normalizeNewlines(value || '');
  if (doFormat) content = prettyXML(content);
  textarea.value = content;
  // Snapshot what we loaded so isDirty() can tell user edits from our own.
  loadedSnapshot = content;
  renderHighlight();
}

// isDirty reports whether the editor holds changes not yet saved.
export function isDirty() {
  const textarea = $('requestBody');
  return !!textarea && textarea.value !== loadedSnapshot;
}

// markSaved makes the current editor content the saved baseline. Without it,
// isDirty() stays true after a successful save and the workspace switcher keeps
// warning about "unsaved changes".
function markSaved() {
  const textarea = $('requestBody');
  loadedSnapshot = textarea ? textarea.value : '';
}

// isSending reports whether a request is currently in flight.
export function isSending() {
  return sending;
}

// clearResponseIfOther drops the response pane when the user selects a
// different request sample, so a stale response is never mistaken for the
// current one. Re-selecting the same sample keeps its response.
function clearResponseIfOther(ids) {
  const current = state.response;
  if (!current) return;
  const prev = current._ids;
  if (!prev || !sameIds(prev, ids)) {
    state.response = null;
    renderResponse();
  }
}

function updateReqBadge() {
  const badge = $('reqBadge');
  if (!badge) return;
  if (!state.selected || state.selected.kind !== 'call') {
    badge.classList.add('hidden');
    return;
  }
  const contentType = $('contentType').value;
  badge.textContent = contentType.startsWith('application/soap+xml') ? 'SOAP 1.2' : 'SOAP 1.1';
  badge.classList.remove('hidden');
}

export function loadNode(node, kind, path = {}) {
  // The response pane belongs to the sample that produced it. Leaving a stale
  // response visible after switching samples made it look like the response
  // came from the newly selected one.
  clearResponseIfOther(path.ids);

  state.selected = {
    kind,
    node,
    ids: path.ids || {},
    parentOp: path.parentOp || null,
  };
  const parentOperation = path.parentOp || null;

  if (kind === 'svc' || kind === 'op') {
    let title;
    if (kind === 'svc') {
      title = '服务：' + (node.name || '');
    } else {
      const count = (node.calls && node.calls.length) ? node.calls.length : 0;
      title = '操作：' + (node.name || '') + (count ? '  ·  ' + count + ' 个请求，请选择请求样例' : '  ·  暂无请求样例');
    }
    $('reqTitle').textContent = title;
    // Show the operation's generated template as a preview. Without this a
    // freshly imported WSDL operation looked completely empty until the user
    // discovered the "+请求" button; the data was there all along.
    $('endpoint').value = kind === 'op' ? (node.endpoint || '') : '';
    $('soapAction').value = kind === 'op' ? (node.soapAction || '') : '';
    setRequestBody(kind === 'op' ? (node.template || node.request || '') : '', false);
    updateReqBadge();
    updateEditButtons();
    syncSelection();
    return;
  }

  let title = '';
  if (kind === 'call') {
    title = (node.title || '请求') + (parentOperation ? '  ·  ' + (parentOperation.name || '') : '');
  } else {
    title = (node.name || '') + '  · 测试步骤';
  }
  $('reqTitle').textContent = title;
  $('endpoint').value = node.endpoint || (parentOperation && parentOperation.endpoint) || '';
  $('soapAction').value = node.action || node.soapAction || (parentOperation && parentOperation.soapAction) || '';
  const autoFormat = $('autoFormat') ? $('autoFormat').checked : false;
  setRequestBody(node.request || '', autoFormat);
  updateReqBadge();
  updateEditButtons();
  syncSelection();
}

export function updateEditButtons() {
  const selected = state.selected;
  const isCall = !!selected && selected.kind === 'call';
  const canDelete = !!selected && (selected.kind === 'call' || selected.kind === 'op' || selected.kind === 'svc');
  $('btnSaveCall').disabled = !isCall || saving;
  $('btnDeleteNode').disabled = !canDelete || saving;
  // While a request is in flight the button becomes a Cancel action, so it
  // stays enabled but its label and handler change (see send()).
  const btnSend = $('btnSend');
  btnSend.disabled = sending ? false : !isCall;
  btnSend.textContent = sending ? '取消' : '发送';
  btnSend.classList.toggle('is-cancel', sending);
}

// cancelSend aborts an in-flight request and invalidates its completion.
//
// Bumping sendSeq makes the aborted request's catch/finally no-ops, so it
// cannot repaint the "已取消" state after the caller has already reset the
// editor (which is what happens on a workspace switch).
export function cancelSend() {
  if (abortController) abortController.abort();
  sendSeq++;
  sending = false;
  abortController = null;
  updateEditButtons();
}

// resetEditor clears the editor and response pane without touching the tree.
// Called when the active workspace changes: the old selection and its response
// belong to projects that are no longer loaded.
export function resetEditor() {
  state.selected = null;
  state.response = null;
  loadedSnapshot = '';
  const title = $('reqTitle');
  if (title) title.textContent = '未选择请求';
  const endpoint = $('endpoint');
  if (endpoint) endpoint.value = '';
  const action = $('soapAction');
  if (action) action.value = '';
  const body = $('requestBody');
  if (body) body.value = '';
  const info = $('sendInfo');
  if (info) info.textContent = '';
  renderHighlight();
  updateReqBadge();
  updateEditButtons();
  renderResponse();
}

function formatBody() {
  const textarea = $('requestBody');
  textarea.value = prettyXML(textarea.value);
  renderHighlight();
}

function minifyBody() {
  const textarea = $('requestBody');
  textarea.value = minifyXML(textarea.value);
  renderHighlight();
}

// send executes the current request, or cancels the in-flight one. Each request
// captures its ids; a late response is dropped if the selection moved on.
async function send() {
  // Clicking while a request is in flight cancels it.
  if (sending) {
    if (abortController) abortController.abort();
    return;
  }

  if (!state.selected || state.selected.kind !== 'call') {
    toast('请先在工程树中选择一个请求样例', true);
    return;
  }

  const url = $('endpoint').value.trim();
  if (!url) {
    toast('请先填写 Endpoint', true);
    return;
  }

  const contentType = $('contentType').value;
  const headers = {};
  const action = $('soapAction').value.trim();
  if (action) {
    if (contentType.startsWith('application/soap+xml')) {
      headers['Content-Type'] = contentType + '; action="' + action.replace(/"/g, '') + '"';
    } else {
      headers['SOAPAction'] = '"' + action.replace(/"/g, '') + '"';
    }
  }

  const payload = {
    projectId: (state.selected.ids && state.selected.ids.pid) || '',
    url,
    method: 'POST',
    contentType,
    headers,
    body: $('requestBody').value,
    timeoutMs: SEND_TIMEOUT_MS,
  };

  const info = $('sendInfo');
  const targetIds = Object.assign({}, state.selected.ids);
  const seq = ++sendSeq;
  sending = true;
  abortController = new AbortController();
  info.textContent = '发送中…（点击「取消」中止）';
  updateEditButtons();

  try {
    const data = await apiSend(payload, { signal: abortController.signal });
    // Drop responses that no longer belong to the current selection.
    if (seq !== sendSeq || !sameIds(state.selected && state.selected.ids, targetIds)) {
      return;
    }
    state.response = Object.assign({}, data, { _ids: targetIds });
    renderResponse();
    info.textContent = data.error ? '失败' : (data.status + ' · ' + (data.durationMs != null ? data.durationMs : 0) + 'ms');
    if (data.truncated) toast('响应超过 16 MB，已截断显示', true);
    if (data.error) toast(data.error, true);
  } catch (error) {
    if (error.name === 'AbortError') {
      if (seq === sendSeq) {
        info.textContent = '已取消';
        toast('已取消请求');
      }
      return;
    }
    if (seq !== sendSeq) return;
    state.response = { error: error.message, _ids: targetIds };
    renderResponse();
    info.textContent = '失败';
    toast('请求出错: ' + error.message, true);
  } finally {
    if (seq === sendSeq) {
      sending = false;
      abortController = null;
      updateEditButtons();
    }
  }
}

// sameIds reports whether two id bundles identify the same request sample.
function sameIds(a, b) {
  if (!a || !b) return false;
  return a.pid === b.pid && a.sid === b.sid && a.oid === b.oid && a.cid === b.cid;
}

async function saveCurrentCall() {
  const selected = state.selected;
  if (!selected || selected.kind !== 'call') {
    toast('请先选择一个请求样例', true);
    return;
  }
  if (saving) return;
  const body = {
    id: selected.ids.cid,
    title: selected.node.title || '请求',
    endpoint: $('endpoint').value.trim(),
    action: $('soapAction').value.trim(),
    request: $('requestBody').value,
  };
  saving = true;
  updateEditButtons();
  try {
    await apiJSON('PUT', callPath(selected.ids), body);
    // The write succeeded: the editor content is now the saved baseline.
    markSaved();
    await reloadHandler();
    toast('已保存');
  } catch (error) {
    toast('保存失败: ' + error.message, true);
  } finally {
    saving = false;
    updateEditButtons();
  }
}

async function deleteCurrentNode() {
  const selected = state.selected;
  if (!selected || saving) return;
  const ids = selected.ids;
  saving = true;
  updateEditButtons();
  try {
    if (!(await deleteNode(selected.kind, ids))) return;
    state.selected = null;
    await reloadHandler();
    toast('已删除');
  } catch (error) {
    toast(error.message, true);
  } finally {
    saving = false;
    updateEditButtons();
  }
}
