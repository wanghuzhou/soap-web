// Response pane: tabs, metadata, rendering and saving a response with its sample.

import { $ } from './dom.js';
import { state, findCall } from './state.js';
import { prettyXML, highlightXML, formatSize } from './xml.js';
import { toast } from './toast.js';
import { copyText } from './clipboard.js';
import { apiJSON, projectPath } from './api.js';

let savingResponse = false;

export function initResponse() {
  document.querySelectorAll('.resp-tabs .tab').forEach((tab) => {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.resp-tabs .tab').forEach((item) => {
        item.classList.remove('active');
        item.setAttribute('aria-selected', 'false');
      });
      tab.classList.add('active');
      tab.setAttribute('aria-selected', 'true');
      state.view = tab.dataset.view;
      renderResponse();
    });
  });

  $('btnCopyResp').addEventListener('click', copyResponse);
  $('btnSaveResp').addEventListener('click', saveResponse);
}

function emptyStateHtml() {
  return `<div class="empty-state">
    <div class="empty-icon" aria-hidden="true">→</div>
    <div class="empty-title">发送请求后，响应会显示在这里</div>
    <div class="empty-desc">选择请求样例、填写 Endpoint，然后按 Ctrl+Enter 发送</div>
  </div>`;
}

export function renderResponse() {
  const response = state.response;
  const meta = $('respMeta');
  const body = $('respBody');
  const summary = $('respSummary');
  updateSaveButton();

  if (!response) {
    meta.classList.add('hidden');
    summary.textContent = '';
    body.innerHTML = emptyStateHtml();
    return;
  }

  if (response.error) {
    meta.classList.remove('hidden');
    summary.textContent = '请求失败';
    $('respStatus').textContent = '请求失败';
    $('respStatus').className = 'status-pill err';
    $('respTime').textContent = response.durationMs != null ? response.durationMs + ' ms' : '';
    $('respSize').textContent = '';
    body.textContent = response.error;
    body.scrollTop = 0;
    return;
  }

  meta.classList.remove('hidden');
  const status = $('respStatus');
  status.textContent = (response.status || 0) + (response.statusText ? ' ' + response.statusText : '');
  status.className = 'status-pill ' + (response.status >= 200 && response.status < 300 ? 'ok' : response.status >= 400 ? 'err' : 'warn');
  $('respTime').textContent = response.durationMs != null ? response.durationMs + ' ms' : '';
  $('respSize').textContent = formatSize(response.size) + (response.truncated ? '（已截断）' : '');
  summary.textContent = response.status
    ? 'HTTP ' + response.status + (response._saved ? ' · 已保存' : '')
    : '';

  if (state.view === 'raw') {
    body.textContent = response.body || '';
  } else if (state.view === 'headers') {
    body.textContent = headersToText(response.headers);
  } else {
    body.innerHTML = prettyHtml(response);
  }
  body.scrollTop = 0;
}

// updateSaveButton keeps the action in step with the current sample/response.
function updateSaveButton() {
  const btn = $('btnSaveResp');
  if (!btn) return;
  const selected = state.selected;
  const response = state.response;
  btn.disabled = savingResponse
    || !selected || selected.kind !== 'call'
    || !response || !!response.error;
}

// savedResponsePayload strips client-only fields (`_ids`, `_saved`, the cached
// highlighted HTML) before the response is persisted with the sample.
export function savedResponsePayload(response) {
  return {
    status: response.status || 0,
    statusText: response.statusText || '',
    headers: Object.assign({}, response.headers || {}),
    body: response.body || '',
    durationMs: response.durationMs || 0,
    size: response.size || 0,
    truncated: !!response.truncated,
  };
}

// saveResponse writes the response into the selected request sample, so it is
// visible the next time the sample (or the app) is opened. Sending again
// produces a fresh response (without `_saved`), and saving it replaces the
// stored one; an existing saved response is never a reason to block an update.
async function saveResponse() {
  const selected = state.selected;
  const response = state.response;
  if (!selected || selected.kind !== 'call' || !response || response.error) {
    toast('请先选择请求样例并发送请求', true);
    return;
  }
  if (savingResponse) return;

  const ids = selected.ids;
  const payload = savedResponsePayload(response);
  savingResponse = true;
  updateSaveButton();
  try {
    await apiJSON('PUT', projectPath(
      ids.pid, 'services', ids.sid, 'operations', ids.oid, 'calls', ids.cid, 'response',
    ), payload);
    // Keep the in-memory tree node in sync so switching away and back shows the
    // saved response without a reload. Look it up again: a reload may have
    // replaced the object while the write was in flight.
    const found = findCall(ids);
    if (found) found.call.response = payload;
    // If the user moved to another sample meanwhile, leave that pane alone.
    if (state.selected === selected && state.response === response) {
      response._saved = true;
      renderResponse();
    }
    toast(response.truncated ? '响应已保存到请求样例（内容已截断）' : '响应已保存到请求样例');
  } catch (error) {
    toast('保存响应失败: ' + error.message, true);
  } finally {
    savingResponse = false;
    updateSaveButton();
  }
}

// prettyHtml lazily caches the highlighted HTML on the response object, so tab
// switches do not re-run prettyXML/highlightXML over the whole body.
function prettyHtml(response) {
  if (response._prettyHtml == null) {
    response._prettyHtml = highlightXML(prettyXML(response.body || ''));
  }
  return response._prettyHtml;
}

// headersToText renders the header map as "Name: value" lines, sorted.
function headersToText(headers) {
  return Object.entries(headers || {})
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([key, value]) => key + ': ' + value)
    .join('\n');
}

async function copyResponse() {
  if (!state.response) {
    toast('暂无响应可复制', true);
    return;
  }
  // Copy from the source string, not from the rendered DOM: innerText forces a
  // full synchronous layout (slow on a large highlighted <pre>) and, in the
  // pretty view, would copy re-formatted text instead of the real bytes.
  const text = state.view === 'headers'
    ? headersToText(state.response.headers)
    : (state.response.body || '');
  const ok = await copyText(text, '响应已复制');
  if (!ok) toast('复制失败，请手动选择文本', true);
}
