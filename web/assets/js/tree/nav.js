// Tree keyboard navigation (the ARIA tree model) and row selection dispatch.

import { $ } from '../dom.js';
import { findService, findOperation, findCall } from '../state.js';
import { isCollapsed, toggleNode } from './render.js';
import { handleTreeAction, beginRename } from './actions.js';

let onSelect = () => {};

export function initTreeNav({ onSelect: handler } = {}) {
  onSelect = handler || onSelect;
  const tree = $('tree');
  tree.addEventListener('keydown', handleTreeKeydown);
  tree.addEventListener('focusin', handleTreeFocusIn);
}

// handleTreeFocusIn makes the focused row the roving tab stop, so Tab-ing away
// and back returns to where the user was.
function handleTreeFocusIn(event) {
  const row = event.target.closest('.tree-node');
  if (!row) return;
  const tree = $('tree');
  tree.querySelectorAll('.tree-node').forEach((r) => { r.tabIndex = -1; });
  row.tabIndex = 0;
}

// visibleRows returns the rows keyboard navigation may visit: collapsed
// subtrees are hidden with a class, not removed from the DOM.
function visibleRows(tree) {
  return Array.from(tree.querySelectorAll('.tree-node'))
    .filter((row) => !row.classList.contains('is-hidden'));
}

// handleTreeKeydown implements the standard ARIA tree keyboard model:
//   Up/Down   move between visible rows
//   Right     expand, or move into the first child
//   Left      collapse, or move to the parent
//   Enter/Space select the row
//   F2        rename (request samples and projects)
function handleTreeKeydown(event) {
  const row = event.target.closest('.tree-node');
  if (!row) return;
  const tree = $('tree');
  const rows = visibleRows(tree);
  const index = rows.indexOf(row);
  if (index < 0) return;

  const move = (target) => {
    if (!target) return;
    event.preventDefault();
    rows.forEach((r) => { r.tabIndex = -1; });
    target.tabIndex = 0;
    target.focus();
  };

  switch (event.key) {
    case 'ArrowDown':
      move(rows[index + 1]);
      return;
    case 'ArrowUp':
      move(rows[index - 1]);
      return;
    case 'Home':
      move(rows[0]);
      return;
    case 'End':
      move(rows[rows.length - 1]);
      return;
    case 'ArrowRight': {
      event.preventDefault();
      const key = toggleKeyOf(row);
      if (key && isCollapsed(key)) {
        toggleNode(key);
        return;
      }
      const next = rows[index + 1];
      // Only descend when the next row is actually a child (deeper).
      if (next && depthOf(next) > depthOf(row)) move(next);
      return;
    }
    case 'ArrowLeft': {
      event.preventDefault();
      const key = toggleKeyOf(row);
      if (key && !isCollapsed(key)) {
        toggleNode(key);
        return;
      }
      // Move to the nearest preceding row with a shallower depth (its parent).
      for (let i = index - 1; i >= 0; i--) {
        if (depthOf(rows[i]) < depthOf(row)) { move(rows[i]); return; }
      }
      return;
    }
    case 'Enter':
    case ' ':
      event.preventDefault();
      selectRow(row);
      return;
    case 'F2': {
      const data = row.dataset;
      if (data.kind === 'call') {
        event.preventDefault();
        beginRename('call', { pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid });
      } else if (data.kind === 'proj') {
        event.preventDefault();
        beginRename('proj', { pid: data.pid });
      }
      return;
    }
    default:
  }
}

// toggleKeyOf returns the collapse key of a row, or '' when it is a leaf.
function toggleKeyOf(row) {
  const caret = row.querySelector('.caret[data-toggle]');
  return caret ? caret.dataset.toggle : '';
}

function depthOf(row) {
  return Number(row.style.getPropertyValue('--depth')) || 0;
}

// selectRow dispatches selection for a tree row. Shared by mouse and keyboard.
export function selectRow(row) {
  const data = row.dataset;

  if (data.kind === 'proj') {
    toggleNode(data.pid);
    return;
  }

  if (data.kind === 'svc') {
    const found = findService(data.pid, data.sid);
    if (found) onSelect(found.service, 'svc', { ids: { pid: data.pid, sid: data.sid } });
  } else if (data.kind === 'op') {
    const found = findOperation(data.pid, data.sid, data.oid);
    if (found) onSelect(found.operation, 'op', { ids: { pid: data.pid, sid: data.sid, oid: data.oid } });
  } else if (data.kind === 'call') {
    const ids = { pid: data.pid, sid: data.sid, oid: data.oid, cid: data.cid };
    const found = findCall(ids);
    if (found) onSelect(found.call, 'call', { ids, parentOp: found.operation });
  }
}
