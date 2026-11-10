// Body-level popover menu for tree rows, so the scrolling tree cannot clip it.

import { $, escapeHtml } from './dom.js';
import { openPopover } from './popover.js';

const MENU_ID = 'rowMenu';

let current = null; // { anchor, close }

export function closeRowMenu() {
  if (current) current.close();
}

export function openRowMenu({ anchor, items, onSelect }) {
  const menu = $(MENU_ID);
  if (!menu || !anchor) return;

  // Clicking the same trigger again closes it.
  if (current && current.anchor === anchor) {
    closeRowMenu();
    return;
  }
  closeRowMenu();

  menu.innerHTML = items.map((item, index) => {
    if (item.separator) return '<div class="row-menu-sep" role="separator"></div>';
    const cls = 'row-menu-item' + (item.danger ? ' danger' : '');
    return `<button type="button" class="${cls}" role="menuitem" data-index="${index}">${escapeHtml(item.label)}</button>`;
  }).join('');

  // Without this the row's hover-only action buttons would disappear while the
  // trigger still holds focus.
  const anchorRow = anchor.closest('.tree-node');
  if (anchorRow) anchorRow.classList.add('menu-open');

  const close = openPopover({
    menu,
    anchor,
    align: 'right',
    onClose: () => {
      current = null;
      if (anchorRow) anchorRow.classList.remove('menu-open');
    },
  });
  current = { anchor, close };

  menu.querySelectorAll('.row-menu-item').forEach((button) => {
    button.addEventListener('click', () => {
      const item = items[Number(button.dataset.index)];
      closeRowMenu();
      if (item && onSelect) onSelect(item);
    });
  });
}
