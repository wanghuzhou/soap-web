// Clipboard helper shared by the response pane and the curl copy button.
//
// navigator.clipboard needs a secure context and permission; the fallback is a
// hidden textarea plus execCommand, which returns false instead of throwing.

import { toast } from './toast.js';

export async function copyText(text, okMessage) {
  try {
    await navigator.clipboard.writeText(text);
    if (okMessage) toast(okMessage);
    return true;
  } catch (error) {
    const helper = document.createElement('textarea');
    helper.value = text;
    helper.style.position = 'fixed';
    helper.style.opacity = '0';
    document.body.appendChild(helper);
    helper.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    helper.remove();
    if (ok && okMessage) toast(okMessage);
    return ok;
  }
}
