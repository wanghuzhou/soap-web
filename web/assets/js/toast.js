// Bottom-centre toast notifications.

import { $ } from './dom.js';

let timer = null;

export function toast(message, isError = false) {
  const el = $('toast');
  if (!el) return;
  el.textContent = message;
  el.className = 'toast' + (isError ? ' err' : '');
  clearTimeout(timer);
  timer = setTimeout(() => el.classList.add('hidden'), 3200);
}
