// Popover menu anchored to a row button. Used by the project row's "⋯" trigger
// so the row keeps only its primary action visible.
//
// The menu lives at <body> level with position: fixed, because the tree pane
// scrolls (overflow: auto) and would otherwise clip it.

import { $, escapeHtml } from './dom.js';

const MENU_ID = 'rowMenu';

let current = null; // { anchor, items, onSelect, cleanup }

function place(menu, anchor) {
  // Lay out at the origin first so the measured size is trustworthy.
  menu.style.left = '0px';
  menu.style.top = '0px';

  const rect = anchor.getBoundingClientRect();
  const size = menu.getBoundingClientRect();
  const gap = 4;
  const margin = 8;

  // Right-align with the trigger, then keep it inside the viewport.
  let left = rect.right - size.width;
  if (left + size.width > window.innerWidth - margin) {
    left = window.innerWidth - margin - size.width;
  }
  if (left < margin) left = margin;

  // Prefer opening downwards; flip above the trigger when there is no room.
  let top = rect.bottom + gap;
  if (top + size.height > window.innerHeight - margin) {
    const above = rect.top - size.height - gap;
    top = above >= margin ? above : window.innerHeight - margin - size.height;
  }
  if (top < margin) top = margin;

  menu.style.left = Math.round(left) + 'px';
  menu.style.top = Math.round(top) + 'px';
}

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
  place(menu, anchor);

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

export function isRowMenuOpenFor(anchor) {
  return !!current && current.anchor === anchor;
}
