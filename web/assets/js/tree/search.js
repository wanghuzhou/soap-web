// Project tree filter.
//
// Typing in the search box keeps only the rows whose name matches, plus their
// ancestors so the path to a result stays readable. Filtering is a class change
// on the existing rows (no rebuild), so focus, scroll and collapse state
// survive every keystroke, and clearing restores the previous view instantly.
//
// Collapse state and filtering compose: a row is visible only when it matches
// (or has a matching descendant) AND its ancestors are expanded.

import { $ } from '../dom.js';

// query is the current filter, lower-cased and trimmed. Empty means "no filter".
let query = '';
let onChange = () => {};

export function initTreeSearch({ onChange: handler } = {}) {
  onChange = handler || onChange;
  const input = $('treeSearch');
  const clear = $('treeSearchClear');
  if (!input) return;

  input.addEventListener('input', () => setQuery(input.value));
  input.addEventListener('keydown', (event) => {
    // Escape clears the filter instead of closing whatever else is open; the
    // focus stays in the box so the user can keep refining.
    if (event.key === 'Escape' && input.value) {
      event.preventDefault();
      input.value = '';
      setQuery('');
    }
  });
  if (clear) {
    clear.addEventListener('click', () => {
      input.value = '';
      setQuery('');
      input.focus();
    });
  }
}

export function getQuery() {
  return query;
}

function setQuery(raw) {
  const next = String(raw || '').trim().toLowerCase();
  if (next === query) return;
  query = next;
  const clear = $('treeSearchClear');
  if (clear) clear.classList.toggle('hidden', !query);
  applyTreeFilter();
  onChange(query);
}

// matchInfo reports, for every row, whether it should be visible and whether it
// matched directly (for highlighting). Rows are read from the DOM in document
// order, which is also parent-before-child order.
function matchInfo(tree) {
  const rows = Array.from(tree.querySelectorAll('.tree-node'));
  const self = new Map(); // row -> matched directly
  const keep = new Set(); // rows to show

  rows.forEach((row) => {
    const label = row.querySelector('.lbl');
    const text = (label ? label.textContent : '').toLowerCase();
    if (!text.includes(query)) return;
    self.set(row, true);
    // Keep the row and walk up to its ancestors so the path is visible.
    let node = row;
    while (node) {
      keep.add(node);
      node = parentRowOf(tree, node);
    }
  });
  return { self, keep };
}

// parentRowOf finds the enclosing row of a given row. The markup nests rows in
// sibling <div class="tree-group"> blocks per project, so the parent chain is
// derived from the data attributes: a call belongs to op → svc → proj.
function parentRowOf(tree, row) {
  const d = row.dataset;
  let selector = '';
  if (d.kind === 'call') selector = `.tree-node[data-kind="op"][data-oid="${cssEscape(d.oid)}"]`;
  else if (d.kind === 'op') selector = `.tree-node[data-kind="svc"][data-sid="${cssEscape(d.sid)}"]`;
  else if (d.kind === 'svc') selector = `.tree-node[data-kind="proj"][data-pid="${cssEscape(d.pid)}"]`;
  else return null;
  return tree.querySelector(selector);
}

function cssEscape(value) {
  return String(value).replace(/["\\]/g, '\\$&');
}

// applyTreeFilter recomputes visibility from the current query. Called on every
// keystroke and after a re-render. While a filter is active it overrides the
// collapse state: an ancestor of a match is shown even if collapsed, otherwise
// the path to the result would be unreachable.
export function applyTreeFilter() {
  const tree = $('tree');
  if (!tree) return;

  const rows = tree.querySelectorAll('.tree-node');
  if (!query) {
    rows.forEach((row) => row.classList.remove('is-hidden', 'is-match'));
    return;
  }

  const { self, keep } = matchInfo(tree);
  rows.forEach((row) => {
    const visible = keep.has(row);
    row.classList.toggle('is-hidden', !visible);
    row.classList.toggle('is-match', self.has(row));
    if (!visible) row.tabIndex = -1;
  });
  ensureTabStop(tree, rows);
}

// ensureTabStop keeps exactly one visible row in the tab order. Without this,
// collapsing everything a filter showed could leave no row reachable by Tab.
function ensureTabStop(tree, rows) {
  const visible = Array.from(rows).filter((row) => !row.classList.contains('is-hidden'));
  if (!visible.length) return;
  if (visible.some((row) => row.tabIndex === 0)) return;
  const preferred = visible.find((row) => row.classList.contains('selected')) || visible[0];
  preferred.tabIndex = 0;
}
