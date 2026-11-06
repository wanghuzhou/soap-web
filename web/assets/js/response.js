// Response pane: tabs, metadata and syntax-highlighted rendering.

import { $ } from './dom.js';
import { state } from './state.js';
import { prettyXML, highlightXML, formatSize } from './xml.js';
import { toast } from './toast.js';

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
  $('respSize').textContent = formatSize(response.size);
  summary.textContent = response.status ? 'HTTP ' + response.status : '';

  if (state.view === 'raw') {
    body.textContent = response.body || '';
  } else if (state.view === 'headers') {
    body.textContent = Object.entries(response.headers || {})
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([key, value]) => key + ': ' + value)
      .join('\n');
  } else {
    const pretty = prettyXML(response.body || '');
    body.innerHTML = highlightXML(pretty);
  }
  body.scrollTop = 0;
}

async function copyResponse() {
  if (!state.response) {
    toast('暂无响应可复制', true);
    return;
  }
  const text = $('respBody').innerText;
  try {
    await navigator.clipboard.writeText(text);
    toast('响应已复制');
  } catch (error) {
    const helper = document.createElement('textarea');
    helper.value = text;
    helper.style.position = 'fixed';
    helper.style.opacity = '0';
    document.body.appendChild(helper);
    helper.select();
    try {
      document.execCommand('copy');
      toast('响应已复制');
    } catch (e) {
      toast('复制失败，请手动选择文本', true);
    }
    helper.remove();
  }
}
