// soap-web frontend (no build step, no external deps).
'use strict';

const $ = (id) => document.getElementById(id);

const state = {
  project: null,       // parsed project (native shape from backend)
  selected: null,      // { kind:'op'|'step', node }
  response: null,
  view: 'pretty',
};

/* ---------------- Toast ---------------- */
let toastTimer = null;
function toast(msg, isErr) {
  const t = $('toast');
  t.textContent = msg;
  t.className = 'toast' + (isErr ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add('hidden'), 3200);
}

/* ---------------- XML highlight / pretty ---------------- */
function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// Pretty-print XML with basic indentation.
function prettyXML(xml) {
  const text = xml.trim();
  if (!text) return '';
  // If not obviously XML, return as-is.
  if (!text.startsWith('<')) return xml;

  let formatted = '';
  let pad = 0;
  const padStr = () => '  '.repeat(pad);
  // Tokenise roughly.
  const tokens = text.replace(/>\s*</g, '><').split(/(?=<)/g);
  for (let tok of tokens) {
    tok = tok.trim();
    if (!tok) continue;
    if (/^<\?/.test(tok) || /^<!/.test(tok)) {
      formatted += padStr() + tok + '\n';
    } else if (/^<\/[^>]+>$/.test(tok)) {
      pad = Math.max(0, pad - 1);
      formatted += padStr() + tok + '\n';
    } else if (/^<[^>]+\/>$/.test(tok)) {
      formatted += padStr() + tok + '\n';
    } else if (/^<[^>]+>$/.test(tok)) {
      formatted += padStr() + tok + '\n';
      pad++;
    } else {
      formatted += padStr() + tok + '\n';
    }
  }
  return formatted.replace(/\n$/, '');
}

function highlightXML(xml) {
  // Escape first, then colourise simple XML constructs.
  let s = escapeHtml(xml);
  // CDATA
  s = s.replace(/(&lt;!\[CDATA\[[\s\S]*?\]\]&gt;)/g, '<span class="x-cdata">$1</span>');
  // Comments
  s = s.replace(/(&lt;!--[\s\S]*?--&gt;)/g, '<span class="x-comment">$1</span>');
  // Tags: capture the whole tag, recolour name + attrs
  s = s.replace(/(&lt;\/?)([\w:.-]+)((?:[^&]|&(?!gt;))*?)(\/?&gt;)/g,
    (m, open, name, attrs, close) => {
      const attrHtml = attrs.replace(/([\w:.-]+)=(&quot;|&#39;|")(.*?)\2/g,
        (am, an, q, av) => ` <span class="x-attr">${an}</span>=<span class="x-val">${q}${av}${q}</span>`);
      return `<span class="x-tag">${open}${name}</span>${attrHtml}<span class="x-tag">${close}</span>`;
    });
  return s;
}

/* ---------------- Request fields ---------------- */
function loadNode(node, kind) {
  state.selected = { kind, node };
  $('reqTitle').textContent = (node.name || '') + (kind === 'op' ? '' : '  · 测试步骤');
  $('endpoint').value = node.endpoint || '';
  $('soapAction').value = node.soapAction || node.action || '';
  $('requestBody').value = node.request || '';
  renderTree();
}

/* ---------------- Tree ---------------- */
function renderTree() {
  const tree = $('tree');
  if (!state.project) return;
  const p = state.project;
  let html = '';

  const hasServices = p.services && p.services.length;
  const hasSuites = p.testSuites && p.testSuites.length;
  if (!hasServices && !hasSuites) {
    tree.innerHTML = '<div class="empty-hint">工程为空。</div>';
    return;
  }

  (p.services || []).forEach((svc) => {
    html += `<div class="tree-group"><div class="tree-node"><span class="ico">▸</span>${escapeHtml(svc.name || 'service')}<span class="tag">service</span></div>`;
    (svc.operations || []).forEach((op, i) => {
      const sel = state.selected && state.selected.kind === 'op' && state.selected.node === op ? ' selected' : '';
      html += `<div class="tree-node op${sel}" data-kind="op" data-svc="${esc(svc.name)}" data-idx="${i}">
        <span class="ico">◇</span>${escapeHtml(op.name || 'operation')}<span class="tag">op</span></div>`;
    });
    html += `</div>`;
  });

  (p.testSuites || []).forEach((ts) => {
    html += `<div class="tree-group"><div class="tree-node"><span class="ico">▸</span>${escapeHtml(ts.name || 'TestSuite')}<span class="tag">suite</span></div>`;
    (ts.testCases || []).forEach((tc) => {
      html += `<div class="tree-node op" style="opacity:.85"><span class="ico">▸</span>${escapeHtml(tc.name || 'TestCase')}<span class="tag">case</span></div>`;
      (tc.steps || []).forEach((st, i) => {
        const sel = state.selected && state.selected.kind === 'step' && state.selected.node === st ? ' selected' : '';
        html += `<div class="tree-node op${sel}" style="padding-left:34px" data-kind="step" data-ts="${esc(ts.name)}" data-tc="${esc(tc.name)}" data-idx="${i}">
          <span class="ico">▸</span>${escapeHtml(st.name || 'step')}<span class="tag">${esc(st.type || 'step')}</span></div>`;
      });
    });
    html += `</div>`;
  });

  tree.innerHTML = html;

  tree.querySelectorAll('.tree-node[data-kind="op"]').forEach((elm) => {
    elm.addEventListener('click', () => {
      const svc = (state.project.services || []).find((s) => s.name === elm.dataset.svc);
      if (svc && svc.operations) loadNode(svc.operations[+elm.dataset.idx], 'op');
    });
  });
  tree.querySelectorAll('.tree-node[data-kind="step"]').forEach((elm) => {
    elm.addEventListener('click', () => {
      const ts = (state.project.testSuites || []).find((t) => t.name === elm.dataset.ts);
      const tc = ts && (ts.testCases || []).find((c) => c.name === elm.dataset.tc);
      if (tc && tc.steps) loadNode(tc.steps[+elm.dataset.idx], 'step');
    });
  });
}
function esc(s) { return String(s == null ? '' : s).replace(/"/g, '&quot;'); }

/* ---------------- Response rendering ---------------- */
function renderResponse() {
  const r = state.response;
  const meta = $('respMeta');
  const body = $('respBody');
  if (!r) return;

  if (r.error) {
    meta.classList.remove('hidden');
    $('respStatus').textContent = '请求失败';
    $('respStatus').className = 'status-pill err';
    $('respTime').textContent = r.durationMS ? r.durationMS + ' ms' : '';
    $('respSize').textContent = '';
    body.textContent = r.error;
    return;
  }

  meta.classList.remove('hidden');
  const st = $('respStatus');
  st.textContent = (r.status || 0) + ' ' + (r.statusText || '');
  st.className = 'status-pill ' + (r.status >= 200 && r.status < 300 ? 'ok' : r.status >= 400 ? 'err' : 'warn');
  $('respTime').textContent = r.durationMS + ' ms';
  $('respSize').textContent = fmtSize(r.size);

  if (state.view === 'raw') {
    body.textContent = r.body || '';
  } else if (state.view === 'headers') {
    body.textContent = Object.entries(r.headers || {})
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([k, v]) => k + ': ' + v).join('\n');
  } else {
    const pretty = prettyXML(r.body || '');
    body.innerHTML = highlightXML(pretty);
  }
}

function fmtSize(n) {
  if (!n) return '0 B';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1024 / 1024).toFixed(2) + ' MB';
}

/* ---------------- Send ---------------- */
async function send() {
  const url = $('endpoint').value.trim();
  if (!url) { toast('请先填写 Endpoint', true); return; }
  const ct = $('contentType').value;
  const headers = {};
  const action = $('soapAction').value.trim();
  if (action) {
    if (ct.startsWith('application/soap+xml')) {
      headers['Content-Type'] = ct + '; action="' + action.replace(/"/g, '') + '"';
    } else {
      headers['SOAPAction'] = '"' + action.replace(/"/g, '') + '"';
    }
  }
  const payload = {
    url,
    method: 'POST',
    contentType: ct,
    headers,
    body: $('requestBody').value,
    timeoutMs: 60000,
  };
  $('btnSend').disabled = true;
  $('sendInfo').textContent = '发送中…';
  try {
    const resp = await fetch('/api/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    const data = await resp.json();
    state.response = data;
    renderResponse();
    $('sendInfo').textContent = data.error ? '失败' : (data.status + ' · ' + data.durationMS + 'ms');
  } catch (e) {
    toast('请求出错: ' + e.message, true);
    $('sendInfo').textContent = '';
  } finally {
    $('btnSend').disabled = false;
  }
}

/* ---------------- Import SoapUI ---------------- */
async function importSoapUI(file) {
  const fd = new FormData();
  fd.append('file', file);
  try {
    const resp = await fetch('/api/import/soapui', { method: 'POST', body: fd });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || '导入失败');
    state.project = data;
    state.selected = null;
    renderTree();
    // auto-select first operation if any
    const svc = (data.services || [])[0];
    if (svc && svc.operations && svc.operations[0]) loadNode(svc.operations[0], 'op');
    toast('已导入工程：' + (data.name || file.name));
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------------- Parse WSDL ---------------- */
async function parseWSDL(file) {
  const fd = new FormData();
  fd.append('file', file);
  try {
    const resp = await fetch('/api/wsdl/parse', { method: 'POST', body: fd });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'WSDL 解析失败');
    // Wrap into a project-like structure so the tree renders.
    state.project = {
      name: data.name || file.name,
      source: 'wsdl',
      services: [{ name: data.name || 'service', wsdl: '', operations: data.operations }],
      testSuites: [],
    };
    state.selected = null;
    renderTree();
    if (data.operations && data.operations[0]) loadNode(data.operations[0], 'op');
    toast('WSDL 解析完成，生成 ' + (data.operations || []).length + ' 个操作');
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------------- Wiring ---------------- */
let pendingMode = null;
function pickFile(mode) {
  pendingMode = mode;
  $('fileInput').value = '';
  $('fileInput').click();
}

window.addEventListener('DOMContentLoaded', () => {
  $('btnSend').addEventListener('click', send);
  $('btnFormat').addEventListener('click', () => {
    $('requestBody').value = prettyXML($('requestBody').value);
  });
  $('btnImport').addEventListener('click', () => pickFile('soapui'));
  $('btnWsdl').addEventListener('click', () => pickFile('wsdl'));

  $('fileInput').addEventListener('change', (e) => {
    const f = e.target.files[0];
    if (!f) return;
    if (pendingMode === 'soapui') importSoapUI(f);
    else if (pendingMode === 'wsdl') parseWSDL(f);
  });

  document.querySelectorAll('.resp-tabs .tab').forEach((tab) => {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.resp-tabs .tab').forEach((t) => t.classList.remove('active'));
      tab.classList.add('active');
      state.view = tab.dataset.view;
      renderResponse();
    });
  });

  // Ctrl+Enter to send
  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); send(); }
  });

  // Tab inserts spaces in the editor
  $('requestBody').addEventListener('keydown', (e) => {
    if (e.key === 'Tab') {
      e.preventDefault();
      const el = e.target; const s = el.selectionStart; const en = el.selectionEnd;
      el.value = el.value.slice(0, s) + '  ' + el.value.slice(en);
      el.selectionStart = el.selectionEnd = s + 2;
    }
  });
});
