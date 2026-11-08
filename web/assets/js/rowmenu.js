// Body-level popover menu for tree rows, so the scrolling tree cannot clip it.

import { $, escapeHtml } from './dom.js';
import { placeMenu } from './popover.js';

const MENU_ID = 'rowMenu';

let current = null; // { anchor, items, onSelect, cleanup }

export function closeRowMenu() {
  if (!current) return;
  const { anchor, cleanup } = current;
  current = null;
  cleanup();

  const menu = $(MENU_ID);
  if (menu) {
    menu.classList.add('hidden');
    menu.innerHTML = '';
  }
  if (anchor && anchor.isConnected) {
    anchor.setAttribute('aria-expanded', 'false');
    // Without this the row's hover-only action buttons would disappear while
    // the trigger still holds focus.
    const row = anchor.closest('.tree-node');
    if (row) row.classList.remove('menu-open');
  }
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

  menu.classList.remove('hidden');
  anchor.setAttribute('aria-expanded', 'true');
  const anchorRow = anchor.closest('.tree-node');
  if (anchorRow) anchorRow.classList.add('menu-open');
  placeMenu(menu, anchor, 'right');

  const onPointerDown = (event) => {
    if (menu.contains(event.target) || event.target === anchor) return;
    closeRowMenu();
  };
  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      // Focus the trigger while the row is still visible, then close: once it
      // has focus, :focus-within keeps the row's actions on screen.
      anchor.focus();
      closeRowMenu();
      return;
    }
    const buttons = Array.from(menu.querySelectorAll('.row-menu-item'));
    if (!buttons.length) return;
    const index = buttons.indexOf(document.activeElement);
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      buttons[(index + 1) % buttons.length].focus();
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      buttons[(index - 1 + buttons.length) % buttons.length].focus();
    } else if (event.key === 'Tab') {
      closeRowMenu();
    }
  };
  const closeOnViewportChange = () => closeRowMenu();

  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown, true);
  window.addEventListener('scroll', closeOnViewportChange, true);
  window.addEventListener('resize', closeOnViewportChange);
  window.addEventListener('blur', closeOnViewportChange);

  const cleanup = () => {
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown, true);
    window.removeEventListener('scroll', closeOnViewportChange, true);
    window.removeEventListener('resize', closeOnViewportChange);
    window.removeEventListener('blur', closeOnViewportChange);
  };

  current = { anchor, items, onSelect, cleanup };

  menu.querySelectorAll('.row-menu-item').forEach((button) => {
    button.addEventListener('click', () => {
      const item = items[Number(button.dataset.index)];
      closeRowMenu();
      if (item && onSelect) onSelect(item);
    });
  });

  const first = menu.querySelector('.row-menu-item');
  if (first) first.focus();
}


