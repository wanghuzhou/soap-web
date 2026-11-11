// Project tree: module wiring. Rendering lives in tree/render.js, keyboard
// navigation in tree/nav.js and CRUD actions in tree/actions.js.

import { $ } from './dom.js';
import { renderTree, syncSelection, toggleNode, applyCollapseVisibility } from './tree/render.js';
import { initTreeNav, selectRow } from './tree/nav.js';
import { initTreeActions, handleTreeAction, openProjectMenu, deleteNode } from './tree/actions.js';
import { initTreeSearch, getQuery } from './tree/search.js';

export { renderTree, syncSelection, toggleNode, deleteNode };

export function initTree(options) {
  initTreeActions(options);
  initTreeNav({ onSelect: options.onSelect });
  // Filtering only changes visibility classes; the rows themselves stay put, so
  // focus and scroll survive typing. Clearing the filter hands visibility back
  // to the collapse state, which the filter deliberately overrode.
  initTreeSearch({
    onChange: () => {
      if (!getQuery()) applyCollapseVisibility();
    },
  });
  const tree = $('tree');
  tree.addEventListener('click', handleTreeClick);
}

function handleTreeClick(event) {
  // Never let a click inside the rename input select / collapse the row.
  if (event.target.closest('.rename-input')) return;

  const moreButton = event.target.closest('[data-more]');
  if (moreButton) {
    event.stopPropagation();
    openProjectMenu(moreButton);
    return;
  }

  const actionButton = event.target.closest('[data-act]');
  if (actionButton) {
    event.stopPropagation();
    handleTreeAction(actionButton.dataset);
    return;
  }

  const caret = event.target.closest('.caret[data-toggle]');
  if (caret) {
    event.stopPropagation();
    toggleNode(caret.dataset.toggle);
    return;
  }

  const row = event.target.closest('.tree-node');
  if (!row) return;
  selectRow(row);
}
