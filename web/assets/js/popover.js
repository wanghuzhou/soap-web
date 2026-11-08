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
