// Shared modal behaviour: show the overlay, close on Escape or a backdrop
// click, and restore focus to the element that opened it.

import { $ } from './dom.js';

// openModal reveals overlayId and returns a close function. onClose runs after
// the overlay is hidden, so callers can clear their own editing state.
export function openModal(overlayId, { onClose, fallbackFocus } = {}) {
  const overlay = $(overlayId);
  if (!overlay) return () => {};

  const lastFocus = document.activeElement;
  overlay.classList.remove('hidden');

  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    document.removeEventListener('keydown', onKeyDown, true);
    overlay.removeEventListener('click', onOverlayClick);
    overlay.classList.add('hidden');
    if (onClose) onClose();
    // The opener may have been re-rendered away; fall back to a stable element
    // instead of leaving focus on <body>.
    const back = lastFocus && lastFocus.isConnected
      ? lastFocus
      : (fallbackFocus ? $(fallbackFocus) : null);
    if (back && back.focus) back.focus();
  };

  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  };
  const onOverlayClick = (event) => {
    if (event.target === overlay) close();
  };

  document.addEventListener('keydown', onKeyDown, true);
  overlay.addEventListener('click', onOverlayClick);
  return close;
}
