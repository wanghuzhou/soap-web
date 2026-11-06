// Request editor: transport fields, highlighted textarea, send/save/delete.

import { $ } from './dom.js';
import { state } from './state.js';
import { apiJSON, apiSend } from './api.js';
import { toast } from './toast.js';
import { prettyXML, minifyXML, normalizeNewlines, highlightXML } from './xml.js';
import { renderTree } from './tree.js';
import { renderResponse } from './response.js';

let reloadHandler = async () => {};

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
    if (modalOpen || document.querySelector('.rename-input')) return;
    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
      event.preventDefault();
      send();
    }
    if ((event.ctrlKey || event.metaKey) && event.shiftKey && (event.key === 'F' || event.key === 'f')) {
      event.preventDefault();
      formatBody();
    }
  });

  updateEditButtons();
  renderHighlight();
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
  renderHighlight();
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
    renderTree();
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
  const autoFormat = $('autoFormat') ? $('autoFormat').checked : true;
  setRequestBody(node.request || '', autoFormat);
  updateReqBadge();
  updateEditButtons();
  renderTree();
}

export function updateEditButtons() {
  const selected = state.selected;
  const isCall = !!selected && selected.kind === 'call';
  const canDelete = !!selected && (selected.kind === 'call' || selected.kind === 'op' || selected.kind === 'svc');
  $('btnSaveCall').disabled = !isCall;
  $('btnDeleteNode').disabled = !canDelete;
  $('btnSend').disabled = !isCall;
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

async function send() {
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
    timeoutMs: 60000,
  };

  const button = $('btnSend');
  const info = $('sendInfo');
  button.disabled = true;
  info.textContent = '发送中…';

  try {
    const data = await apiSend(payload);
    state.response = data;
    renderResponse();
    info.textContent = data.error ? '失败' : (data.status + ' · ' + (data.durationMs != null ? data.durationMs : 0) + 'ms');
    if (data.error) toast(data.error, true);
  } catch (error) {
    state.response = { error: error.message };
    renderResponse();
    info.textContent = '失败';
    toast('请求出错: ' + error.message, true);
  } finally {
    updateEditButtons();
  }
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
