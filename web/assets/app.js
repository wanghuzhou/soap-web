// soap-web frontend (no build step, no external deps).
'use strict';

const $ = (id) => document.getElementById(id);

const state = {
  projects: [],        // all persisted projects (from /api/projects)
  selected: null,      // { kind, node, ids:{pid,sid,oid,cid}, parentOp }
  response: null,
  view: 'pretty',
  theme: 'dark',
  collapsed: {},       // id -> true when collapsed
};

/* ---------------- Collapse state persistence ---------------- */
function saveCollapsed() {
  try { localStorage.setItem('soap-web-collapsed', JSON.stringify(state.collapsed)); } catch (e) { /* ignore */ }
}
function initCollapsed() {
  try {
    const s = localStorage.getItem('soap-web-collapsed');
    if (s) state.collapsed = JSON.parse(s) || {};
  } catch (e) { state.collapsed = {}; }
}

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

// Pretty-print XML with indentation. CDATA and comments keep their inner
// content untouched (whitespace inside CDATA is significant and must not be
// reflowed). Returns input unchanged if it does not look like XML.
function prettyXML(xml) {
  const text = xml == null ? '' : xml;
  if (!text.trim() || text.trim()[0] !== '<') return text;

  // Tokenise into CDATA, comments, PIs, opening/closing/self-closing tags, text.
  const re = /<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->|<\?[\s\S]*?\?>|<\/?[^>]+>|[^<]+/g;
  const toks = [];
  let m;
  while ((m = re.exec(text)) !== null) {
    const raw = m[0];
    if (raw.trim() === '' && !/^\s*$/.test(raw)) { /* keep */ }
    toks.push(raw);
  }

  let out = '';
  let pad = 0;
  const padStr = () => '  '.repeat(pad);

  for (let i = 0; i < toks.length; i++) {
    const raw = toks[i];
    const t = raw.trim();
    if (t === '') continue;

    const isCdata = t.startsWith('<![CDATA[');
    const isComment = t.startsWith('<!--');
    const isPI = t.startsWith('<?');
    const isClose = /^<\//.test(t);
    const isSelf = /^<[^>]+\/>$/.test(t);
    const isOpen = /^<[^>]+>$/.test(t) && !isClose && !isSelf;

    if (isCdata || isComment || isPI) {
      out += padStr() + t + '\n';
    } else if (isClose) {
      pad = Math.max(0, pad - 1);
      out += padStr() + t + '\n';
    } else if (isSelf) {
      out += padStr() + t + '\n';
    } else if (isOpen) {
      // Look ahead: if the next meaningful token is text (not a tag), print the
      // opening tag, the text, and the following closing tag together on a line.
      let j = i + 1;
      while (j < toks.length && toks[j].trim() === '') j++;
      const nextTok = j < toks.length ? toks[j] : '';
      const nextIsText = nextTok !== '' && !nextTok.trim().startsWith('<');
      if (nextIsText) {
        // Find the closing tag after the text.
        let k = j + 1;
        while (k < toks.length && toks[k].trim() === '') k++;
        const closeTok = k < toks.length ? toks[k].trim() : '';
        out += padStr() + t + nextTok.trim() + closeTok + '\n';
        i = k; // consume text + closing tag
      } else {
        out += padStr() + t + '\n';
        pad++;
      }
    } else {
      // Lone text node.
      out += padStr() + t + '\n';
    }
  }
  return out.replace(/\n$/, '');
}

// Minify XML: strip whitespace between tags while preserving CDATA/comments.
function minifyXML(xml) {
  const text = xml == null ? '' : xml;
  // Protect CDATA and comments by placeholders.
  const store = [];
  let s = text.replace(/<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->/g, (m) => {
    store.push(m);
    return '\u0000' + (store.length - 1) + '\u0000';
  });
  s = s.replace(/>\s+</g, '><').trim();
  s = s.replace(/\u0000(\d+)\u0000/g, (_, i) => store[+i]);
  return s;
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

/* ---------------- Editor: highlighted overlay ---------------- */
// Renders the request body into the highlight layer and keeps both layers'
// scroll positions in sync.
function syncHighlight() {
  const ta = $('requestBody');
  const hl = $('requestHighlight');
  if (!ta || !hl) return;
  // A trailing newline keeps the highlight layer the same height as the textarea
  // when the content ends with a newline.
  hl.innerHTML = highlightXML(ta.value) + '\n';
  hl.scrollTop = ta.scrollTop;
  hl.scrollLeft = ta.scrollLeft;
}

// Set the request editor content (optionally formatting first).
function setRequestBody(value, doFormat) {
  const ta = $('requestBody');
  let v = normalizeNewlines(value || '');
  if (doFormat) v = prettyXML(v);
  ta.value = v;
  syncHighlight();
}

// SoapUI stores line breaks in some projects as the literal two-character
// sequence "\r" / "\n" followed by a newline. Normalise these to real newlines
// so the editor shows clean, readable XML instead of stray "\r" tokens.
function normalizeNewlines(s) {
  // SoapUI sometimes stores line breaks as literal two-character sequences
  // ("\r\n", "\r", "\n") often immediately followed by a real newline, which
  // yields a doubled break. Collapse that combination precisely, without
  // disturbing intentional blank lines inside CDATA.
  s = s.replace(/\\(r|n)(\\r|\\n)?[ \t]*\r?\n/g, '\n');
  s = s.replace(/\\(r|n)(\\r|\\n)?/g, '\n');
  // Normalise real CRLF / CR to LF.
  s = s.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
  return s;
}

/* ---------------- Request fields ---------------- */
// Selection carries the full id path so edits/deletes can address the server.
// sel = { kind, ids:{pid,sid,oid,cid}, node, parentOp }
function loadNode(node, kind, path) {
  state.selected = { kind, node, ids: path.ids || {}, parentOp: path.parentOp || null };
  const parentOp = path.parentOp || null;

  // Selecting a service or operation shows only its header. The request editor
  // stays empty because there is no concrete request sample to display yet
  // (only a request sample "call" carries actual request data).
  if (kind === 'svc' || kind === 'op') {
    let title;
    if (kind === 'svc') title = '服务：' + (node.name || '');
    else {
      const n = (node.calls && node.calls.length) ? node.calls.length : 0;
      title = '操作：' + (node.name || '') + (n ? ('  ·  ' + n + ' 个请求，请选择一个请求样例查看报文') : '  ·  暂无请求样例');
    }
    $('reqTitle').textContent = title;
    $('endpoint').value = '';
    $('soapAction').value = '';
    setRequestBody('', false);
    updateEditButtons();
    renderTree();
    return;
  }

  let title = '';
  if (kind === 'call') title = (node.title || '请求') + (parentOp ? '  ·  ' + (parentOp.name || '') : '');
  else title = (node.name || '') + '  · 测试步骤';
  $('reqTitle').textContent = title;
  $('endpoint').value = node.endpoint || (parentOp && parentOp.endpoint) || '';
  $('soapAction').value = node.soapAction || node.action || (parentOp && parentOp.soapAction) || '';
  const autoFmt = $('autoFormat') ? $('autoFormat').checked : true;
  setRequestBody(node.request || '', autoFmt);
  updateEditButtons();
  renderTree();
}

// Enable/disable the save/delete/send buttons depending on what's selected.
function updateEditButtons() {
  const sel = state.selected;
  const canSave = sel && sel.kind === 'call';
  const canDelete = sel && (sel.kind === 'call' || sel.kind === 'op' || sel.kind === 'svc');
  const canSend = sel && sel.kind === 'call';
  $('btnSaveCall').disabled = !canSave;
  $('btnDeleteNode').disabled = !canDelete;
  if ($('btnSend')) $('btnSend').disabled = !canSend;
}

function esc(s) { return String(s == null ? '' : s).replace(/"/g, '&quot;'); }

/* ---------------- API helpers ---------------- */
async function apiGet(url) {
  const r = await fetch(url);
  const d = await r.json();
  if (!r.ok) throw new Error(d.error || ('请求失败 ' + r.status));
  return d;
}
async function apiJSON(method, url, body) {
  const r = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.error || ('请求失败 ' + r.status));
  return d;
}

/* ---------------- Projects loading ---------------- */
async function loadProjects(selectFirst) {
  try {
    const list = await apiGet('/api/projects');
    state.projects = list || [];
    renderTree();
    if (selectFirst && state.projects.length) {
      const p = state.projects[0];
      const svc = (p.services || [])[0];
      const op = svc && (svc.operations || [])[0];
      if (op && op.calls && op.calls.length) {
        loadNode(op.calls[0], 'call', { ids: { pid: p.id, sid: svc.id, oid: op.id, cid: op.calls[0].id }, parentOp: op });
      } else if (op) {
        loadNode(op, 'op', { ids: { pid: p.id, sid: svc.id, oid: op.id } });
      }
    }
  } catch (e) {
    toast('加载工程失败: ' + e.message, true);
  }
}

/* ---------------- Tree (multi-project, with CRUD) ---------------- */
function renderTree() {
  const tree = $('tree');
  const projects = state.projects || [];
  if (!projects.length) {
    tree.innerHTML = '<div class="empty-hint">还没有工程。<br>点击顶部「导入 SoapUI 工程」或「解析 WSDL」新增。</div>';
    return;
  }
  const sel = state.selected;
  let html = '';

  const isCollapsed = (key) => !!state.collapsed[key];
  // Expand/collapse caret. leaf === true renders a non-clickable spacer.
  const caret = (key, leaf) => {
    if (leaf) return '<span class="ico leaf"></span>';
    const c = isCollapsed(key) ? '▸' : '▾';
    return `<span class="ico caret" data-toggle="${esc(key)}" title="展开/收起">${c}</span>`;
  };

  projects.forEach((p) => {
    const pCollapsed = isCollapsed(p.id);
    html += `<div class="tree-group">
      <div class="tree-node proj" data-kind="proj" data-pid="${p.id}">
        ${caret(p.id, false)}<span class="lbl">${escapeHtml(p.name || '工程')}</span>
        <span class="tag">${(p.source || 'native')}</span>
        <span class="node-actions">
          <button class="mini" title="重命名工程" data-act="rename-proj" data-pid="${p.id}">✎</button>
          <button class="mini" title="导出 SoapUI XML" data-act="export-proj" data-pid="${p.id}">导出</button>
          <button class="mini" title="新建服务" data-act="add-svc" data-pid="${p.id}">+服务</button>
          <button class="mini danger" title="删除工程" data-act="del-proj" data-pid="${p.id}">✕</button>
        </span>
      </div>`;

    if (!pCollapsed) (p.services || []).forEach((svc) => {
      const svcSel = sel && sel.kind === 'svc' && sel.ids.sid === svc.id ? ' selected' : '';
      const svcCollapsed = isCollapsed(svc.id);
      const svcLeaf = !(svc.operations && svc.operations.length);
      html += `<div class="tree-node svc${svcSel}" style="padding-left:20px" data-kind="svc" data-pid="${p.id}" data-sid="${svc.id}">
        ${caret(svc.id, svcLeaf)}<span class="lbl">${escapeHtml(svc.name || 'service')}</span>
        <span class="tag">service</span>
        <span class="node-actions">
          <button class="mini" title="新建操作" data-act="add-op" data-pid="${p.id}" data-sid="${svc.id}">+操作</button>
          <button class="mini danger" title="删除服务" data-act="del-svc" data-pid="${p.id}" data-sid="${svc.id}">✕</button>
        </span>
      </div>`;

      if (!svcCollapsed) (svc.operations || []).forEach((op) => {
        const opSel = sel && sel.kind === 'op' && sel.ids.oid === op.id ? ' selected' : '';
        const cnt = (op.calls && op.calls.length) ? op.calls.length + ' 请求' : 'op';
        const opCollapsed = isCollapsed(op.id);
        const opLeaf = !(op.calls && op.calls.length);
        html += `<div class="tree-node op${opSel}" style="padding-left:38px" data-kind="op" data-pid="${p.id}" data-sid="${svc.id}" data-oid="${op.id}">
          ${caret(op.id, opLeaf)}<span class="lbl">${escapeHtml(op.name || 'operation')}</span><span class="tag">${cnt}</span>
          <span class="node-actions">
            <button class="mini" title="新建请求样例" data-act="add-call" data-pid="${p.id}" data-sid="${svc.id}" data-oid="${op.id}">+请求</button>
            <button class="mini danger" title="删除操作" data-act="del-op" data-pid="${p.id}" data-sid="${svc.id}" data-oid="${op.id}">✕</button>
          </span>
        </div>`;

        if (!opCollapsed) (op.calls || []).forEach((call) => {
          const cSel = sel && sel.kind === 'call' && sel.ids.cid === call.id ? ' selected' : '';
          html += `<div class="tree-node op${cSel}" style="padding-left:56px" data-kind="call" data-pid="${p.id}" data-sid="${svc.id}" data-oid="${op.id}" data-cid="${call.id}">
            ${caret(call.id, true)}<span class="call-title">${escapeHtml(call.title || '请求')}</span>
            <span class="node-actions">
              <button class="mini" title="重命名" data-act="rename-call" data-pid="${p.id}" data-sid="${svc.id}" data-oid="${op.id}" data-cid="${call.id}">✎</button>
              <button class="mini danger" title="删除请求" data-act="del-call" data-pid="${p.id}" data-sid="${svc.id}" data-oid="${op.id}" data-cid="${call.id}">✕</button>
            </span>
          </div>`;
        });
      });
    });
    html += `</div>`;
  });

  tree.innerHTML = html;

  // Caret click toggles expand/collapse (does not select).
  tree.querySelectorAll('.ico.caret[data-toggle]').forEach((elm) => {
    elm.addEventListener('click', (ev) => {
      ev.stopPropagation();
      const key = elm.dataset.toggle;
      state.collapsed[key] = !state.collapsed[key];
      saveCollapsed();
      renderTree();
    });
  });

  // Clicking a node selects it (but clicking a button does the action).
  tree.querySelectorAll('.tree-node').forEach((elm) => {
    elm.addEventListener('click', (ev) => {
      if (ev.target.closest('.node-actions')) return; // button handled separately
      if (ev.target.closest('.ico.caret')) return;    // caret handled separately
      const d = elm.dataset;
      const proj = projects.find((x) => x.id === d.pid);
      if (!proj) return;
      if (d.kind === 'proj') {
        // Clicking the project row toggles its collapse.
        state.collapsed[d.pid] = !state.collapsed[d.pid];
        saveCollapsed();
        renderTree();
      } else if (d.kind === 'svc') {
        const svc = (proj.services || []).find((s) => s.id === d.sid);
        if (svc) loadNode(svc, 'svc', { ids: { pid: d.pid, sid: d.sid } });
      } else if (d.kind === 'op') {
        const svc = (proj.services || []).find((s) => s.id === d.sid);
        const op = svc && (svc.operations || []).find((o) => o.id === d.oid);
        if (op) loadNode(op, 'op', { ids: { pid: d.pid, sid: d.sid, oid: d.oid } });
      } else if (d.kind === 'call') {
        const svc = (proj.services || []).find((s) => s.id === d.sid);
        const op = svc && (svc.operations || []).find((o) => o.id === d.oid);
        const call = op && (op.calls || []).find((c) => c.id === d.cid);
        if (call) loadNode(call, 'call', { ids: { pid: d.pid, sid: d.sid, oid: d.oid, cid: d.cid }, parentOp: op });
      }
    });
  });

  // Action buttons.
  tree.querySelectorAll('.node-actions button[data-act]').forEach((btn) => {
    btn.addEventListener('click', (ev) => {
      ev.stopPropagation();
      handleTreeAction(btn.dataset, btn);
    });
  });
}

/* ---------------- Tree CRUD actions ---------------- */
async function handleTreeAction(d, btn) {
  const act = d.act;
  try {
    if (act === 'add-svc') {
      const name = prompt('新服务名称：', 'NewService');
      if (name == null) return;
      await apiJSON('POST', `/api/projects/${d.pid}/services`, { name: name || 'Service' });
      await reload();
    } else if (act === 'del-svc') {
      if (!confirm('确定删除该服务及其下所有操作？')) return;
      await apiJSON('DELETE', `/api/projects/${d.pid}/services/${d.sid}`);
      await reload();
    } else if (act === 'add-op') {
      const name = prompt('新操作名称：', 'NewOperation');
      if (name == null) return;
      await apiJSON('POST', `/api/projects/${d.pid}/services/${d.sid}/operations`, { name: name || 'Operation' });
      await reload();
    } else if (act === 'del-op') {
      if (!confirm('确定删除该操作及其下所有请求？')) return;
      await apiJSON('DELETE', `/api/projects/${d.pid}/services/${d.sid}/operations/${d.oid}`);
      await reload();
    } else if (act === 'add-call') {
      const title = prompt('新请求样例标题：', '新请求');
      if (title == null) return;
      const dir = `/api/projects/${d.pid}/services/${d.sid}/operations/${d.oid}/calls`;
      // Seed the new request with the operation's XML template so the user
      // starts from a ready-to-edit body instead of a blank editor.
      const proj0 = (state.projects || []).find((x) => x.id === d.pid);
      const svc0 = proj0 && (proj0.services || []).find((s) => s.id === d.sid);
      const op0 = svc0 && (svc0.operations || []).find((o) => o.id === d.oid);
      let template = (op0 && op0.request) || '';
      if (!template && op0 && op0.calls && op0.calls.length) {
        template = op0.calls[0].request || '';
      }
      const created = await apiJSON('POST', dir, {
        title: title || '新请求',
        endpoint: (op0 && op0.endpoint) || '',
        action: (op0 && op0.soapAction) || '',
        request: template,
      });
      await reload();
      // select the newly created call
      const proj = (state.projects || []).find((x) => x.id === d.pid);
      const svc = proj && (proj.services || []).find((s) => s.id === d.sid);
      const op = svc && (svc.operations || []).find((o) => o.id === d.oid);
      const call = op && (op.calls || []).find((c) => c.id === created.id);
      if (call) loadNode(call, 'call', { ids: { pid: d.pid, sid: d.sid, oid: d.oid, cid: call.id }, parentOp: op });
    } else if (act === 'del-call') {
      if (!confirm('确定删除该请求样例？')) return;
      await apiJSON('DELETE', `/api/projects/${d.pid}/services/${d.sid}/operations/${d.oid}/calls/${d.cid}`);
      await reload();
    } else if (act === 'rename-call') {
      const proj = (state.projects || []).find((x) => x.id === d.pid);
      const svc = proj && (proj.services || []).find((s) => s.id === d.sid);
      const op = svc && (svc.operations || []).find((o) => o.id === d.oid);
      const call = op && (op.calls || []).find((c) => c.id === d.cid);
      const cur = call ? call.title : '';
      const title = prompt('重命名请求样例：', cur);
      if (title == null || title === cur) return;
      const url = `/api/projects/${d.pid}/services/${d.sid}/operations/${d.oid}/calls/${d.cid}`;
      await apiJSON('PUT', url, { id: d.cid, title, endpoint: call.endpoint, action: call.action, request: call.request });
      await reload();
    } else if (act === 'del-proj') {
      const proj = (state.projects || []).find((x) => x.id === d.pid);
      if (!confirm('确定删除整个工程「' + ((proj && proj.name) || '') + '」？')) return;
      await apiJSON('DELETE', `/api/projects/${d.pid}`);
      await reload();
    } else if (act === 'rename-proj') {
      const proj = (state.projects || []).find((x) => x.id === d.pid);
      if (!proj) return;
      const name = prompt('重命名工程：', proj.name || '');
      if (name == null || name === proj.name) return;
      const updated = Object.assign({}, proj, { name });
      await apiJSON('PUT', `/api/projects/${d.pid}`, updated);
      await reload();
      toast('工程已重命名');
    } else if (act === 'export-proj') {
      // Trigger a file download of the SoapUI-compatible XML.
      const a = document.createElement('a');
      a.href = `/api/projects/${d.pid}/export`;
      a.download = '';
      document.body.appendChild(a);
      a.click();
      a.remove();
      toast('已导出 SoapUI XML');
    }
  } catch (e) {
    toast(e.message, true);
  }
}

// Reload project list from server, preserving current selection if possible.
async function reload() {
  const sel = state.selected;
  try {
    state.projects = await apiGet('/api/projects');
  } catch (e) {
    toast(e.message, true);
    return;
  }
  // Try to re-resolve the selected node by id; fall back to clearing.
  if (sel && sel.kind === 'call' && sel.ids.cid) {
    const found = findCallById(sel.ids);
    if (found) { loadNode(found.call, 'call', { ids: sel.ids, parentOp: found.op }); return; }
  }
  if (sel && sel.kind === 'op' && sel.ids.oid) {
    const found = findOpById(sel.ids);
    if (found) { loadNode(found.op, 'op', { ids: sel.ids }); return; }
  }
  if (state.projects.length && !$('requestBody').value) renderTree();
  else renderTree();
}

function findCallById(ids) {
  const p = (state.projects || []).find((x) => x.id === ids.pid);
  const svc = p && (p.services || []).find((s) => s.id === ids.sid);
  const op = svc && (svc.operations || []).find((o) => o.id === ids.oid);
  const call = op && (op.calls || []).find((c) => c.id === ids.cid);
  return call ? { call, op } : null;
}
function findOpById(ids) {
  const p = (state.projects || []).find((x) => x.id === ids.pid);
  const svc = p && (p.services || []).find((s) => s.id === ids.sid);
  const op = svc && (svc.operations || []).find((o) => o.id === ids.oid);
  return op ? { op } : null;
}

/* ---------------- Save current request sample ---------------- */
async function saveCurrentCall() {
  const sel = state.selected;
  if (!sel || sel.kind !== 'call') { toast('请先选择一个请求样例', true); return; }
  const ids = sel.ids;
  const url = `/api/projects/${ids.pid}/services/${ids.sid}/operations/${ids.oid}/calls/${ids.cid}`;
  const body = {
    id: ids.cid,
    title: sel.node.title || '请求',
    endpoint: $('endpoint').value.trim(),
    action: $('soapAction').value.trim(),
    request: $('requestBody').value,
  };
  try {
    await apiJSON('PUT', url, body);
    await reload();
    toast('已保存');
  } catch (e) {
    toast('保存失败: ' + e.message, true);
  }
}

/* ---------------- Delete current selected node ---------------- */
async function deleteCurrentNode() {
  const sel = state.selected;
  if (!sel) return;
  const ids = sel.ids;
  try {
    if (sel.kind === 'call') {
      if (!confirm('确定删除该请求样例？')) return;
      await apiJSON('DELETE', `/api/projects/${ids.pid}/services/${ids.sid}/operations/${ids.oid}/calls/${ids.cid}`);
    } else if (sel.kind === 'op') {
      if (!confirm('确定删除该操作及其下所有请求？')) return;
      await apiJSON('DELETE', `/api/projects/${ids.pid}/services/${ids.sid}/operations/${ids.oid}`);
    } else if (sel.kind === 'svc') {
      if (!confirm('确定删除该服务？')) return;
      await apiJSON('DELETE', `/api/projects/${ids.pid}/services/${ids.sid}`);
    } else {
      return;
    }
    state.selected = null;
    await reload();
    toast('已删除');
  } catch (e) {
    toast(e.message, true);
  }
}

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
  if (!state.selected || state.selected.kind !== 'call') {
    toast('请先在工程树中选择一个请求样例', true);
    return;
  }
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
    // Backend persists and returns the saved project.
    await loadProjects(false);
    toast('已导入并保存工程：' + (data.name || file.name));
    // Auto-select first call/op of the imported project.
    const proj = (state.projects || []).find((x) => x.id === data.id);
    if (proj) selectFirst(proj);
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
    await loadProjects(false);
    toast('WSDL 解析完成并已保存工程');
    if (data.project) {
      const proj = (state.projects || []).find((x) => x.id === data.project.id);
      if (proj) selectFirst(proj);
    }
  } catch (e) {
    toast(e.message, true);
  }
}

// Select the first available call (or op) within a project.
function selectFirst(proj) {
  const svc = (proj.services || [])[0];
  const op = svc && (svc.operations || [])[0];
  if (op && op.calls && op.calls.length) {
    loadNode(op.calls[0], 'call', { ids: { pid: proj.id, sid: svc.id, oid: op.id, cid: op.calls[0].id }, parentOp: op });
  } else if (op) {
    loadNode(op, 'op', { ids: { pid: proj.id, sid: svc.id, oid: op.id } });
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
    const ta = $('requestBody');
    ta.value = prettyXML(ta.value);
    syncHighlight();
  });
  $('btnMinify').addEventListener('click', () => {
    const ta = $('requestBody');
    ta.value = minifyXML(ta.value);
    syncHighlight();
  });
  $('btnImport').addEventListener('click', () => pickFile('soapui'));
  $('btnWsdl').addEventListener('click', () => pickFile('wsdl'));
  $('btnSaveCall').addEventListener('click', saveCurrentCall);
  $('btnDeleteNode').addEventListener('click', deleteCurrentNode);
  $('btnNewProject').addEventListener('click', async () => {
    const name = prompt('新工程名称：', '新工程');
    if (name == null) return;
    try {
      const p = await apiJSON('POST', '/api/projects', { name: name || '新工程', services: [] });
      await loadProjects(false);
      toast('已创建工程：' + p.name);
    } catch (e) { toast(e.message, true); }
  });

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

  // Keep the highlight layer in sync while editing / scrolling.
  const ta = $('requestBody');
  ta.addEventListener('input', syncHighlight);
  ta.addEventListener('scroll', syncHighlight);
  ta.addEventListener('change', syncHighlight);
  window.addEventListener('resize', syncHighlight);

  // Ctrl+Enter to send; Ctrl+Shift+F to format.
  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); send(); }
    if ((e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'F' || e.key === 'f')) {
      e.preventDefault();
      ta.value = prettyXML(ta.value);
      syncHighlight();
    }
  });

  // Tab inserts spaces in the editor
  ta.addEventListener('keydown', (e) => {
    if (e.key === 'Tab') {
      e.preventDefault();
      const el = e.target; const s = el.selectionStart; const en = el.selectionEnd;
      el.value = el.value.slice(0, s) + '  ' + el.value.slice(en);
      el.selectionStart = el.selectionEnd = s + 2;
      syncHighlight();
    }
  });

  // Theme toggle (persisted).
  initTheme();
  $('btnTheme').addEventListener('click', toggleTheme);

  // Restore tree collapse state.
  initCollapsed();

  // Resizable panes.
  initResizers();

  // Initial paint + load persisted projects.
  syncHighlight();
  updateEditButtons();
  loadProjects(true);
});

/* ---------------- Theme (light / dark) ---------------- */
function applyTheme(theme) {
  const t = theme === 'light' ? 'light' : 'dark';
  if (t === 'light') document.documentElement.setAttribute('data-theme', 'light');
  else document.documentElement.removeAttribute('data-theme');
  const btn = $('btnTheme');
  if (btn) btn.textContent = t === 'light' ? '🌙 夜间' : '☀ 白天';
  state.theme = t;
  try { localStorage.setItem('soap-web-theme', t); } catch (e) { /* ignore */ }
}
function initTheme() {
  // Allow explicit override via URL (?theme=light / ?theme=dark).
  let q = null;
  try { q = new URLSearchParams(location.search).get('theme'); } catch (e) { /* ignore */ }
  if (q === 'light' || q === 'dark') { applyTheme(q); return; }
  let saved = null;
  try { saved = localStorage.getItem('soap-web-theme'); } catch (e) { /* ignore */ }
  if (!saved) {
    // Respect OS preference on first visit.
    saved = (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches) ? 'light' : 'dark';
  }
  applyTheme(saved);
}
function toggleTheme() {
  applyTheme(state.theme === 'light' ? 'dark' : 'light');
}

/* ---------------- Resizable three panes ---------------- */
// Persists left and middle pane widths; the right pane takes the remainder.
function initResizers() {
  const ws = document.querySelector('.workspace');
  if (!ws) return;
  const left = $('paneLeft'), mid = $('paneMid'), right = $('paneRight');
  const rL = $('resizerLeft'), rR = $('resizerRight');
  if (!left || !mid || !right || !rL || !rR) return;

  // Restore saved widths.
  try {
    const js = localStorage.getItem('soap-web-cols');
    if (js) {
      const c = JSON.parse(js);
      if (c.left) ws.style.setProperty('--col-left', c.left + 'px');
      if (c.mid) ws.style.setProperty('--col-mid', c.mid + 'px');
      if (c.right) ws.style.setProperty('--col-right', c.right + 'px');
    }
  } catch (e) { /* ignore */ }

  const MIN = 160;

  function startDrag(handle, onMove) {
    handle.addEventListener('mousedown', (e) => {
      e.preventDefault();
      handle.classList.add('dragging');
      document.body.classList.add('resizing');
      const move = (ev) => onMove(ev);
      const up = () => {
        handle.classList.remove('dragging');
        document.body.classList.remove('resizing');
        document.removeEventListener('mousemove', move);
        document.removeEventListener('mouseup', up);
        saveCols();
      };
      document.addEventListener('mousemove', move);
      document.addEventListener('mouseup', up);
    });
  }

  function saveCols() {
    const rectL = left.getBoundingClientRect().width;
    const rectM = mid.getBoundingClientRect().width;
    const rectR = right.getBoundingClientRect().width;
    try {
      localStorage.setItem('soap-web-cols', JSON.stringify({ left: Math.round(rectL), mid: Math.round(rectM), right: Math.round(rectR) }));
    } catch (e) { /* ignore */ }
  }

  // Drag left divider: resizes the left pane.
  startDrag(rL, (ev) => {
    const wsRect = ws.getBoundingClientRect();
    let w = ev.clientX - wsRect.left;
    w = Math.max(MIN, Math.min(w, wsRect.width - 320));
    ws.style.setProperty('--col-left', w + 'px');
    syncHighlight();
  });

  // Drag right divider: resizes the middle pane (right takes remainder).
  startDrag(rR, (ev) => {
    const wsRect = ws.getBoundingClientRect();
    // middle width = dividerX - leftWidth - handle widths
    const leftW = left.getBoundingClientRect().width;
    let midW = ev.clientX - wsRect.left - leftW - 12; // 2 handles * 6px
    midW = Math.max(MIN, Math.min(midW, wsRect.width - leftW - MIN - 12));
    ws.style.setProperty('--col-mid', midW + 'px');
    syncHighlight();
  });

  window.addEventListener('resize', syncHighlight);
}
