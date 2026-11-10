// Request headers editor.
//
// A sample sends the project's headers overlaid by its own (the backend's
// MergeHeaders does the same). The panel shows the merged result, so what the
// user sees is what gets sent. Rows coming from the project are marked 工程 and
// can only be overridden — to change one everywhere, edit the project.
//
// The draft lives here rather than in the DOM because the tree is re-rendered
// on unrelated actions, which would otherwise discard unsaved edits.

import { $, escapeAttr } from './dom.js';
import { state, findProject } from './state.js';
import { isSensitiveName, STORAGE_KEY } from './constants.js';

const MASK_KEY = STORAGE_KEY.MASK_SECRETS;

// draft rows: { name, value, fromProject }. `fromProject` means the row exists
// only because the project declares it; adding a same-named row overrides it.
let draft = [];
let onDirty = () => {};

export function initHeaders({ onDirty: handler } = {}) {
  onDirty = handler || onDirty;
  const list = $('headersList');
  if (!list) return;

  list.addEventListener('input', handleEdit);
  list.addEventListener('click', handleClick);

  $('btnAddHeader').addEventListener('click', () => {
    draft.push({ name: '', value: '', fromProject: false });
    renderHeaders();
    onDirty();
    const inputs = list.querySelectorAll('.header-name');
    const last = inputs[inputs.length - 1];
    if (last) last.focus();
  });

  const mask = $('maskSecrets');
  if (mask) {
    mask.checked = readMaskPreference();
    mask.addEventListener('change', () => {
      writeMaskPreference(mask.checked);
      renderHeaders();
    });
  }
}

// loadHeaders resets the draft for one node. Pass the owning project's headers
// and the sample's overrides; editable=false marks everything read-only (the
// project itself has no separate override layer).
export function loadHeaders(projectHeaders, callHeaders) {
  draft = mergeRows(projectHeaders, callHeaders);
  renderHeaders();
}

export function clearHeaders() {
  draft = [];
  renderHeaders();
}

// collectHeaders returns what to persist on the call: only rows that differ
// from the project default, so later project changes still flow through.
export function collectHeaders() {
  const inherited = new Map(currentProjectHeaders().map((h) => [h.name.toLowerCase(), h.value]));
  return draft
    .filter((row) => row.name.trim() && !row.fromProject)
    .map((row) => ({ name: row.name.trim(), value: row.value }))
    .filter((row) => inherited.get(row.name.toLowerCase()) !== row.value);
}

// headersForSend flattens the merged rows into the map /api/send expects.
export function headersForSend() {
  const out = {};
  draft.forEach((row) => {
    const name = row.name.trim();
    if (name) out[name] = row.value;
  });
  return out;
}

// isHeadersDirty compares the draft with the sample's saved overrides.
export function isHeadersDirty(saved) {
  return JSON.stringify(collectHeaders()) !== JSON.stringify(normaliseList(saved));
}

function currentProjectHeaders() {
  const selected = state.selected;
  const project = selected && findProject(selected.ids && selected.ids.pid);
  return normaliseList(project && project.headers);
}

function normaliseList(list) {
  return (list || [])
    .filter((h) => h && h.name && String(h.name).trim())
    .map((h) => ({ name: String(h.name).trim(), value: String(h.value ?? '') }));
}

// mergeRows overlays the sample's headers on the project's, matching names
// case-insensitively like the backend. Project order is kept; call-only names
// follow in their own order.
function mergeRows(projectHeaders, callHeaders) {
  const out = [];
  const index = new Map();
  const add = (h, fromProject) => {
    const name = String(h.name || '').trim();
    if (!name) return;
    const key = name.toLowerCase();
    const at = index.get(key);
    if (at !== undefined) {
      out[at].value = h.value ?? '';
      // A call-level row overrides the inherited value, so it becomes editable.
      if (!fromProject) out[at].fromProject = false;
      return;
    }
    index.set(key, out.length);
    out.push({ name, value: h.value ?? '', fromProject });
  };
  normaliseList(projectHeaders).forEach((h) => add(h, true));
  normaliseList(callHeaders).forEach((h) => add(h, false));
  return out;
}

function readMaskPreference() {
  try {
    return localStorage.getItem(MASK_KEY) !== '0';
  } catch (e) {
    return true;
  }
}

function writeMaskPreference(on) {
  try {
    localStorage.setItem(MASK_KEY, on ? '1' : '0');
  } catch (e) { /* storage may be unavailable */ }
}

function renderHeaders() {
  const list = $('headersList');
  if (!list) return;
  const count = $('headersCount');
  if (count) count.textContent = String(draft.filter((r) => r.name.trim()).length);

  if (!draft.length) {
    list.innerHTML = '<div class="headers-empty">没有自定义请求头。SOAPAction / Content-Type 由上方字段生成。</div>';
    return;
  }

  const maskOn = $('maskSecrets') ? $('maskSecrets').checked : true;
  list.innerHTML = draft.map((row, i) => {
    // A masked value stays a real input with its real value: the browser's
    // password field hides it, and revealing is a type flip instead of a
    // re-render that would drop focus mid-edit.
    const masked = maskOn && isSensitiveName(row.name);
    const tag = row.fromProject
      ? '<span class="tag header-tag" title="来自工程默认设置；在此填写同名的行即可覆盖">工程</span>'
      : '';
    const remove = row.fromProject
      ? ''
      : `<button class="mini danger header-remove" type="button" data-index="${i}" title="删除该请求头" aria-label="删除该请求头">✕</button>`;
    return `<div class="header-row${row.fromProject ? ' from-project' : ''}" data-index="${i}">
      <input class="header-name" data-field="name" value="${escapeAttr(row.name)}" placeholder="Header 名称" spellcheck="false" autocomplete="off" aria-label="请求头名称">
      <input class="header-value${masked ? ' is-masked' : ''}" data-field="value" type="${masked ? 'password' : 'text'}" value="${escapeAttr(row.value)}" placeholder="值" spellcheck="false" autocomplete="off" aria-label="请求头值">
      ${tag}
      ${remove}
    </div>`;
  }).join('');
}

function handleEdit(event) {
  const input = event.target.closest('input[data-field]');
  if (!input) return;
  const row = input.closest('.header-row');
  if (!row) return;
  const target = draft[Number(row.dataset.index)];
  if (!target) return;

  if (input.dataset.field === 'name') {
    target.name = input.value;
    // Renaming a project row keeps it inherited only while the name matches a
    // project header; otherwise it becomes the user's own row.
    const inherited = currentProjectHeaders().some(
      (h) => h.name.toLowerCase() === input.value.trim().toLowerCase(),
    );
    target.fromProject = inherited;
  } else {
    target.value = input.value;
    // Typing over an inherited value turns the row into an override.
    if (target.fromProject && target.value !== inheritedValue(target.name)) {
      target.fromProject = false;
    }
  }
  onDirty();
}

function inheritedValue(name) {
  const found = currentProjectHeaders().find((h) => h.name.toLowerCase() === name.trim().toLowerCase());
  return found ? found.value : undefined;
}

function handleClick(event) {
  const remove = event.target.closest('.header-remove');
  if (!remove) return;
  event.preventDefault();
  const index = Number(remove.dataset.index);
  if (!Number.isFinite(index)) return;
  draft.splice(index, 1);
  renderHeaders();
  onDirty();
}
