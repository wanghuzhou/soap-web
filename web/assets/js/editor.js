// Request editor: transport fields, highlighted textarea, send/save/delete.

import { $ } from './dom.js';
import { state } from './state.js';
import { apiJSON, apiSend } from './api.js';
import { toast } from './toast.js';
import { prettyXML, minifyXML, normalizeNewlines, highlightXML } from './xml.js';
import { syncSelection } from './tree.js';
import { STORAGE_KEY } from './constants.js';
import { isRenaming } from './inlineedit.js';
import { renderResponse } from './response.js';

let reloadHandler = async () => {};

// Send state.
//
// The previous implementation guarded concurrency with button.disabled alone,
// which three paths could bypass: loadNode() calls updateEditButtons() and
// re-enables the button while a request is in flight; the global Ctrl+Enter
// shortcut calls send() directly without checking anything; and there was no
// AbortController. Combined with state.response being a global singleton that
// loadNode() never cleared, a response for request A could be rendered onto
// the panel of request B, which is the worst possible failure for a debugging
// tool.
//
// Now: `sending` is the real gate, `sendSeq` identifies the current request so
// late responses are discarded, and `abortController` lets the user cancel.
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

  const textarea = $('requestBody');
  textarea.addEventListener('input', renderHighlight);
  textarea.addEventListener('scroll', syncScroll, { passive: true });
  textarea.addEventListener('keydown', handleEditorKeydown);

  document.addEventListener('keydown', (event) => {
    const modalOpen = !$('proxyOverlay').classList.contains('hidden');
    // While a tree row is being renamed inline, global shortcuts must not fire
    // (the editor also stops its own keydown from bubbling, this is a guard).
    if (modalOpen || isRenaming()) return;
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault();
      send();
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

function renderHighlight() {
  const textarea = $('requestBody');
  const highlight = $('requestHighlight');
  if (!textarea || !highlight) return;
  highlight.innerHTML = highlightXML(textarea.value) + '\n';
  syncScroll();
}

function syncScroll() {
  const textarea = $('requestBody');
  const highlight = $('requestHighlight');
  if (!textarea || !highlight) return;
  highlight.scrollTop = textarea.scrollTop;
  highlight.scrollLeft = textarea.scrollLeft;
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
    $('endpoint').value = '';
    $('soapAction').value = '';
    setRequestBody('', false);
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

// INDENT is the editor's indent unit (two spaces). Kept in sync with the
// indentation prettyXML emits.
const INDENT = '  ';

// handleEditorKeydown implements Tab / Shift+Tab indentation.
//
// The previous implementation assigned textarea.value wholesale, which had two
// defects: it wiped the native undo stack (Ctrl+Z could not restore the
// indentation) and it replaced the entire selection with a single indent
// (selecting N lines and pressing Tab deleted N lines of XML).
//
// Now: a collapsed caret inserts one indent via execCommand (the only path that
// preserves the undo stack, with setRangeText as fallback), and a multi-line
// selection indents or outdents every line it touches while restoring the
// selection.
function handleEditorKeydown(event) {
  if (event.key !== 'Tab') return;
  event.preventDefault();
  const textarea = event.target;
  const value = textarea.value;
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;
  const outdent = event.shiftKey;

  // Single caret: insert / remove one indent level.
  if (start === end) {
    if (!outdent) {
      insertText(textarea, INDENT);
      return;
    }
    // Shift+Tab with no selection removes up to INDENT.length leading spaces.
    const lineStart = value.lastIndexOf('\n', start - 1) + 1;
    let remove = 0;
    while (remove < INDENT.length && value[lineStart + remove] === ' ') remove++;
    if (remove === 0) return;
    textarea.setSelectionRange(lineStart, lineStart + remove);
    insertText(textarea, '');
    textarea.setSelectionRange(Math.max(lineStart, start - remove), Math.max(lineStart, start - remove));
    renderHighlight();
    return;
  }

  // Multi-line selection: operate per line.
  const blockStart = value.lastIndexOf('\n', start - 1) + 1;
  // Include the trailing line when the selection ends exactly at its start.
  let blockEnd = end;
  if (blockEnd > 0 && value[blockEnd - 1] === '\n') blockEnd -= 1;
  const blockEndIdx = value.indexOf('\n', blockEnd);
  const sliceEnd = blockEndIdx === -1 ? value.length : blockEndIdx;

  const lines = value.slice(blockStart, sliceEnd).split('\n');
  let newStart = start;
  let newEnd = end;
  const next = lines.map((line, i) => {
    if (!outdent) {
      // Every line in the block gains an indent, so the selection grows by one
      // indent per line (the first line's gain is absorbed by the offset).
      newEnd += INDENT.length;
      if (i === 0) newStart += INDENT.length;
      return INDENT + line;
    }
    const remove = Math.min(INDENT.length, line.length - line.replace(/^ +/, '').length);
    newEnd -= remove;
    if (i === 0) newStart = Math.max(blockStart, newStart - remove);
    return line.slice(remove);
  }).join('\n');

  // setRangeText keeps the operation a single undo step.
  textarea.setSelectionRange(blockStart, sliceEnd);
  textarea.setRangeText(next, blockStart, sliceEnd, 'preserve');
  textarea.setSelectionRange(newStart, newEnd);
  renderHighlight();
}

// insertText replaces the current selection. execCommand keeps the browser's
// native undo history intact; setRangeText is the fallback when it is
// unavailable (e.g. not focused, or unsupported).
function insertText(textarea, text) {
  if (text === '' || document.execCommand) {
    try {
      textarea.focus();
      if (document.execCommand('insertText', false, text)) return;
    } catch (e) { /* fall through */ }
  }
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;
  textarea.setRangeText(text, start, end, 'end');
}

function callURL(ids) {
  return '/api/projects/' + encodeURIComponent(ids.pid)
    + '/services/' + encodeURIComponent(ids.sid)
    + '/operations/' + encodeURIComponent(ids.oid)
    + '/calls/' + encodeURIComponent(ids.cid);
}

// send executes the current request, or cancels the in-flight one.
//
// Concurrency rules:
//   - `sending` is the single source of truth; Ctrl+Enter cannot bypass it.
//   - Each request captures the ids it was started for. When the response
//     arrives, it is discarded if the selection has moved on or a newer send
//     has superseded it, so a response never renders onto another sample's
//     panel.
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
  const body = {
    id: selected.ids.cid,
    title: selected.node.title || '请求',
    endpoint: $('endpoint').value.trim(),
    action: $('soapAction').value.trim(),
    request: $('requestBody').value,
  };
  try {
    await apiJSON('PUT', callURL(selected.ids), body);
    await reloadHandler();
    toast('已保存');
  } catch (error) {
    toast('保存失败: ' + error.message, true);
  }
}

async function deleteCurrentNode() {
  const selected = state.selected;
  if (!selected) return;
  const ids = selected.ids;
  try {
    if (selected.kind === 'call') {
      if (!confirm('确定删除该请求样例？')) return;
      await apiJSON('DELETE', callURL(ids));
    } else if (selected.kind === 'op') {
      if (!confirm('确定删除该操作及其下所有请求？')) return;
      await apiJSON('DELETE', '/api/projects/' + encodeURIComponent(ids.pid)
        + '/services/' + encodeURIComponent(ids.sid)
        + '/operations/' + encodeURIComponent(ids.oid));
    } else if (selected.kind === 'svc') {
      if (!confirm('确定删除该服务？')) return;
      await apiJSON('DELETE', '/api/projects/' + encodeURIComponent(ids.pid)
        + '/services/' + encodeURIComponent(ids.sid));
    } else {
      return;
    }
    state.selected = null;
    await reloadHandler();
    toast('已删除');
  } catch (error) {
    toast(error.message, true);
  }
}
