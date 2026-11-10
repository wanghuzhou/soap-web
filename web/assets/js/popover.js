// Shared fixed-position placement for popover menus.

// placeMenu positions a body-level menu next to its anchor. `align` controls
// whether the menu's right edge (default) or left edge lines up with the
// anchor; either way it is clamped into the viewport and flips above the
// anchor when there is no room below.
export function placeMenu(menu, anchor, align = 'right') {
  // Lay out at the origin first so the measured size is trustworthy.
  menu.style.left = '0px';
  menu.style.top = '0px';

  const rect = anchor.getBoundingClientRect();
  const size = menu.getBoundingClientRect();
  const gap = 4;
  const margin = 8;

  let left = align === 'left' ? rect.left : rect.right - size.width;
  if (left + size.width > window.innerWidth - margin) {
    left = window.innerWidth - margin - size.width;
  }
  if (left < margin) left = margin;

  let top = rect.bottom + gap;
  if (top + size.height > window.innerHeight - margin) {
    const above = rect.top - size.height - gap;
    top = above >= margin ? above : window.innerHeight - margin - size.height;
  }
  if (top < margin) top = margin;

  menu.style.left = Math.round(left) + 'px';
  menu.style.top = Math.round(top) + 'px';
}

// ITEM_SELECTOR is the focusable menu entry shared by the row menu and the
// workspace switcher.
const ITEM_SELECTOR = '.row-menu-item:not(:disabled)';

// openPopover makes a rendered menu interactive: it positions the menu, moves
// focus into it, and closes on Escape, an outside click, Tab, or a viewport
// change. Callers own the menu contents and their click handlers.
export function openPopover({ menu, anchor, align = 'right', onClose }) {
  menu.classList.remove('hidden');
  anchor.setAttribute('aria-expanded', 'true');
  placeMenu(menu, anchor, align);

  const items = () => Array.from(menu.querySelectorAll(ITEM_SELECTOR));
  const first = items()[0];
  if (first) first.focus();

  let closed = false;
  const close = (restoreFocus = false) => {
    if (closed) return;
    closed = true;
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown, true);
    window.removeEventListener('resize', onViewportChange);
    window.removeEventListener('scroll', onViewportChange, true);
    window.removeEventListener('blur', onViewportChange);
    // Focus the trigger before hiding so :focus-within keeps a tree row's
    // actions visible while the menu disappears.
    if (restoreFocus && anchor.isConnected) anchor.focus();
    menu.classList.add('hidden');
    menu.innerHTML = '';
    if (anchor.isConnected) anchor.setAttribute('aria-expanded', 'false');
    if (onClose) onClose();
  };

  const onPointerDown = (event) => {
    if (menu.contains(event.target) || anchor.contains(event.target)) return;
    close();
  };
  const onViewportChange = () => close();
  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      close(true);
      return;
    }
    const list = items();
    if (!list.length) return;
    const index = list.indexOf(document.activeElement);
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      const next = index < 0 ? 0 : (index + 1) % list.length;
      list[next].focus();
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      const prev = index < 0 ? list.length - 1 : (index - 1 + list.length) % list.length;
      list[prev].focus();
    } else if (event.key === 'Tab') {
      close();
    }
  };

  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown, true);
  window.addEventListener('resize', onViewportChange);
  window.addEventListener('scroll', onViewportChange, true);
  window.addEventListener('blur', onViewportChange);

  return () => close();
}
