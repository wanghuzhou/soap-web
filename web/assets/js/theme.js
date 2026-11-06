// Light/dark theme, persisted in localStorage and following the OS on first run.

import { $ } from './dom.js';
import { state } from './state.js';

const THEME_KEY = 'soap-web-theme';

function hasSavedTheme() {
  try { return localStorage.getItem(THEME_KEY) !== null; } catch (e) { return false; }
}

export function applyTheme(theme, persist = true) {
  const next = theme === 'light' ? 'light' : 'dark';
  if (next === 'light') document.documentElement.setAttribute('data-theme', 'light');
  else document.documentElement.removeAttribute('data-theme');

  const icon = $('themeIcon');
  const label = $('themeLabel');
  if (icon) icon.textContent = next === 'light' ? '🌙' : '☀';
  if (label) label.textContent = next === 'light' ? '夜间' : '白天';
  state.theme = next;
  if (persist) {
    try { localStorage.setItem(THEME_KEY, next); } catch (e) { /* ignore */ }
  }
}

export function initTheme() {
  let queryTheme = null;
  try {
    const query = new URLSearchParams(location.search).get('theme');
    if (query === 'light' || query === 'dark') queryTheme = query;
  } catch (e) { /* ignore */ }

  const saved = hasSavedTheme();
  let initial = document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
  if (queryTheme) initial = queryTheme;
  else if (!saved && window.matchMedia) {
    initial = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }
  applyTheme(initial, false);

  if (!queryTheme && !saved && window.matchMedia) {
    const media = window.matchMedia('(prefers-color-scheme: light)');
    const onChange = (event) => applyTheme(event.matches ? 'light' : 'dark', false);
    if (media.addEventListener) media.addEventListener('change', onChange);
    else if (media.addListener) media.addListener(onChange);
  }

  $('btnTheme').addEventListener('click', () => {
    applyTheme(state.theme === 'light' ? 'dark' : 'light');
  });
}
