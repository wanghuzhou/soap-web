// Resizable three-pane layout. Persists column widths as pixels, but always
// clamps them to the current viewport so shrinking the window never pushes the
// right pane off-screen. Below 1100px CSS switches to a stacked layout.

import { $ } from './dom.js';

const STORAGE_KEY = 'soap-web-cols-v2';
const BREAKPOINT_QUERY = '(min-width: 1101px)';
const MIN_LEFT = 200;
const MIN_MID = 340;
const MIN_RIGHT = 320;
const HANDLE_TOTAL = 12; // two 6px resizers
const DEFAULT_LEFT = 272;

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

function readSaved() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY) || localStorage.getItem('soap-web-cols');
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
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify({ left: leftWidth, mid: midWidth })); } catch (error) { /* ignore */ }
  }

  const saved = readSaved();
  if (saved) applyColumns(saved.left, saved.mid);

  function startDrag(handle, onMove) {
    handle.addEventListener('mousedown', (event) => {
      if (event.button !== 0 || !isWide()) return;
      event.preventDefault();

      const startX = event.clientX;
      const startLeft = leftPane.getBoundingClientRect().width;
      const startMid = midPane.getBoundingClientRect().width;
      handle.classList.add('dragging');
      document.body.classList.add('resizing');

      const move = (moveEvent) => onMove(moveEvent.clientX - startX, startLeft, startMid);
      const up = () => {
        handle.classList.remove('dragging');
        document.body.classList.remove('resizing');
        document.removeEventListener('mousemove', move);
        document.removeEventListener('mouseup', up);
        saveColumns();
      };
      document.addEventListener('mousemove', move);
      document.addEventListener('mouseup', up);
    });
  }

  startDrag(leftHandle, (delta, startLeft, startMid) => {
    const total = width();
    const maxLeft = Math.max(MIN_LEFT, total - HANDLE_TOTAL - Math.max(startMid, MIN_MID) - MIN_RIGHT);
    applyColumns(clamp(startLeft + delta, MIN_LEFT, maxLeft), startMid);
  });

  startDrag(rightHandle, (delta, startLeft, startMid) => {
    const total = width();
    const maxMid = Math.max(MIN_MID, total - startLeft - HANDLE_TOTAL - MIN_RIGHT);
    applyColumns(startLeft, clamp(startMid + delta, MIN_MID, maxMid));
  });

  [leftHandle, rightHandle].forEach((handle) => {
    handle.addEventListener('dblclick', () => {
      applyColumns(DEFAULT_LEFT, null);
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
      const maxLeft = Math.max(MIN_LEFT, total - HANDLE_TOTAL - MIN_MID - MIN_RIGHT);
      if (leftWidth > maxLeft) {
        leftWidth = maxLeft;
        workspace.style.setProperty('--col-left', leftWidth + 'px');
      }
      const maxMid = Math.max(MIN_MID, total - leftWidth - HANDLE_TOTAL - MIN_RIGHT);
      if (midWidth > maxMid) {
        midWidth = maxMid;
        workspace.style.setProperty('--col-mid', midWidth + 'px');
      }
    });
  });
}
