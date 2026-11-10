// Shared modal behaviour: show the overlay, close on Escape or a backdrop
// click, and restore focus to the element that opened it.

import { $ } from './dom.js';

// openModals maps an open overlay to its close function, so a double click
// reuses the existing instance instead of stacking listeners.
const openModals = new Map();

// openModal reveals overlayId and returns a close function. onClose runs after
// the overlay is hidden, so callers can clear their own editing state.
export function openModal(overlayId, { onClose, fallbackFocus } = {}) {
  const overlay = $(overlayId);
  if (!overlay) return () => {};
  if (openModals.has(overlayId)) return openModals.get(overlayId);

  const lastFocus = document.activeElement;
  overlay.classList.remove('hidden');

  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    openModals.delete(overlayId);
    document.removeEventListener('keydown', onKeyDown, true);
    overlay.removeEventListener('click', onOverlayClick);
    overlay.classList.add('hidden');
    if (onClose) onClose();
    // The opener may have been re-rendered away (or focus may have fallen to
    // <body> when a menu closed); use the fallback in both cases.
    const back = lastFocus && lastFocus !== document.body && lastFocus.isConnected
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
  openModals.set(overlayId, close);
  return close;
}
