// Resizable three-pane layout. Persists column widths as pixels, but always
// clamps them to the current viewport so shrinking the window never pushes the
// right pane off-screen. Below 1100px CSS switches to a stacked layout.

import { $ } from './dom.js';

const STORAGE_KEY = 'soap-web-cols-v2';
const LEGACY_KEY = 'soap-web-cols';
const BREAKPOINT_QUERY = '(min-width: 1101px)';
const MIN_LEFT = 200;
const MIN_MID = 340;
const MIN_RIGHT = 320;
const HANDLE_TOTAL = 12; // two 6px resizers
const DEFAULT_LEFT = 272;
const KEYBOARD_STEP = 16;
const KEYBOARD_PAGE = 80;

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

function readSaved() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY) || localStorage.getItem(LEGACY_KEY);
    if (!raw) return null;
    const saved = JSON.parse(raw);
    const left = Number(saved.left);
    const mid = Number(saved.mid);
    return {
      left: Number.isFinite(left) ? left : DEFAULT_LEFT,
      mid: Number.isFinite(mid) ? mid : null,
    };
  } catch (error) {
    return null;
  }
}

export function initLayout() {
  const workspace = $('workspace');
  const leftPane = $('paneLeft');
  const midPane = $('paneMid');
  const rightPane = $('paneRight');
  const leftHandle = $('resizerLeft');
  const rightHandle = $('resizerRight');
  if (!workspace || !leftPane || !midPane || !rightPane || !leftHandle || !rightHandle) return;

  const isWide = () => window.matchMedia(BREAKPOINT_QUERY).matches;

  // width() reads layout once per operation, not once per pointermove.
  function width() {
    return workspace.getBoundingClientRect().width;
  }

  function applyColumns(leftWidth, midWidth) {
    if (!isWide()) return;
    const total = width();
    const maxLeft = Math.max(MIN_LEFT, total - HANDLE_TOTAL - MIN_MID - MIN_RIGHT);
    const nextLeft = clamp(leftWidth, MIN_LEFT, maxLeft);
    workspace.style.setProperty('--col-left', nextLeft + 'px');

    if (midWidth == null) {
      workspace.style.removeProperty('--col-mid');
      return;
    }
    const maxMid = Math.max(MIN_MID, total - nextLeft - HANDLE_TOTAL - MIN_RIGHT);
    workspace.style.setProperty('--col-mid', clamp(midWidth, MIN_MID, maxMid) + 'px');
  }

  function saveColumns() {
    const leftWidth = Math.round(leftPane.getBoundingClientRect().width);
    const midWidth = Math.round(midPane.getBoundingClientRect().width);
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ left: leftWidth, mid: midWidth }));
    } catch (error) { /* storage may be unavailable */ }
  }

  const saved = readSaved();
  if (saved) applyColumns(saved.left, saved.mid);

  // currentColumns returns the widths currently in effect.
  function currentColumns() {
    return {
      left: leftPane.getBoundingClientRect().width,
      mid: midPane.getBoundingClientRect().width,
    };
  }

  // startDrag wires a resizer to pointer events. Pointer capture guarantees the
  // pointerup arrives, moves are coalesced into one write per frame, and
  // pointercancel cleans up.
  function startDrag(handle, onDelta) {
    handle.addEventListener('pointerdown', (event) => {
      if (event.button !== 0 || !isWide()) return;
      event.preventDefault();
      handle.setPointerCapture(event.pointerId);

      const startX = event.clientX;
      const start = currentColumns();
      handle.classList.add('dragging');
      document.body.classList.add('resizing');

      let pendingDelta = 0;
      let frame = 0;

      const flush = () => {
        frame = 0;
        onDelta(pendingDelta, start);
      };

      const move = (moveEvent) => {
        pendingDelta = moveEvent.clientX - startX;
        if (!frame) frame = requestAnimationFrame(flush);
      };

      const finish = (cancel) => {
        if (frame) { cancelAnimationFrame(frame); frame = 0; }
        handle.classList.remove('dragging');
        document.body.classList.remove('resizing');
        handle.removeEventListener('pointermove', move);
        handle.removeEventListener('pointerup', up);
        handle.removeEventListener('pointercancel', cancel);
        if (handle.hasPointerCapture && handle.hasPointerCapture(event.pointerId)) {
          handle.releasePointerCapture(event.pointerId);
        }
        if (!cancel) saveColumns();
      };

      const up = () => finish(false);
      const cancel = () => finish(true);

      handle.addEventListener('pointermove', move);
      handle.addEventListener('pointerup', up);
      handle.addEventListener('pointercancel', cancel);
    });
  }

  startDrag(leftHandle, (delta, start) => {
    const total = width();
    const maxLeft = Math.max(MIN_LEFT, total - HANDLE_TOTAL - Math.max(start.mid, MIN_MID) - MIN_RIGHT);
    applyColumns(clamp(start.left + delta, MIN_LEFT, maxLeft), start.mid);
  });

  startDrag(rightHandle, (delta, start) => {
    const total = width();
    const maxMid = Math.max(MIN_MID, total - start.left - HANDLE_TOTAL - MIN_RIGHT);
    applyColumns(start.left, clamp(start.mid + delta, MIN_MID, maxMid));
  });

  [leftHandle, rightHandle].forEach((handle, index) => {
    handle.addEventListener('dblclick', () => {
      applyColumns(DEFAULT_LEFT, null);
      saveColumns();
    });

    // Keyboard resizing. role="separator" is an interactive role, so it must be
    // focusable and expose its value; without this the layout was unusable
    // without a mouse.
    handle.addEventListener('keydown', (event) => {
      if (!isWide()) return;
      const columns = currentColumns();
      let delta = 0;
      switch (event.key) {
        case 'ArrowLeft': delta = -KEYBOARD_STEP; break;
        case 'ArrowRight': delta = KEYBOARD_STEP; break;
        case 'PageUp': delta = -KEYBOARD_PAGE; break;
        case 'PageDown': delta = KEYBOARD_PAGE; break;
        case 'Home':
          event.preventDefault();
          applyColumns(DEFAULT_LEFT, null);
          saveColumns();
          return;
        default:
          return;
      }
      event.preventDefault();
      if (index === 0) {
        const total = width();
        const maxLeft = Math.max(MIN_LEFT, total - HANDLE_TOTAL - Math.max(columns.mid, MIN_MID) - MIN_RIGHT);
        applyColumns(clamp(columns.left + delta, MIN_LEFT, maxLeft), columns.mid);
      } else {
        const total = width();
        const maxMid = Math.max(MIN_MID, total - columns.left - HANDLE_TOTAL - MIN_RIGHT);
        applyColumns(columns.left, clamp(columns.mid + delta, MIN_MID, maxMid));
      }
      saveColumns();
    });
  });

  let resizeFrame = 0;
  window.addEventListener('resize', () => {
    cancelAnimationFrame(resizeFrame);
    resizeFrame = requestAnimationFrame(() => {
      if (!isWide()) return;
      const total = width();
      let leftWidth = leftPane.getBoundingClientRect().width;
      let midWidth = midPane.getBoundingClientRect().width;
      let changed = false;
      const maxLeft = Math.max(MIN_LEFT, total - HANDLE_TOTAL - MIN_MID - MIN_RIGHT);
      if (leftWidth > maxLeft) {
        leftWidth = maxLeft;
        workspace.style.setProperty('--col-left', leftWidth + 'px');
        changed = true;
      }
      const maxMid = Math.max(MIN_MID, total - leftWidth - HANDLE_TOTAL - MIN_RIGHT);
      if (midWidth > maxMid) {
        midWidth = maxMid;
        workspace.style.setProperty('--col-mid', midWidth + 'px');
        changed = true;
      }
      // Persist the clamped widths too, otherwise the stored values keep the
      // pre-clamp size and the layout snaps back on the next load.
      if (changed) saveColumns();
    });
  });
}
