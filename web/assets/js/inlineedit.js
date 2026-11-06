// In-place rename for tree rows. Replaces the row's label with a text input and
// reports the result through callbacks; it deliberately knows nothing about
// projects, APIs or rendering so tree.js stays the only owner of that logic.
//
// Design notes:
// - Only the label node is swapped, never the whole tree. Rebuilding the tree
//   on blur would remove the element the user is currently clicking and swallow
//   that click.
// - The original label element is put back on finish, so a failed save leaves
//   the row showing exactly what it showed before.
// - Keydown is stopped from bubbling so global shortcuts (Ctrl+Enter = send)
//   cannot fire while typing a name.
// - IME: Enter during composition must confirm the candidate, not submit.

let active = null;

export function isRenaming() {
  return !!document.querySelector('.rename-input');
}

// cancelActiveRename is called before the tree is rebuilt so a stale editor
// never survives a render with its input detached from the DOM.
export function cancelActiveRename() {
  if (active) active.cancel();
}

export function startInlineRename(row, { value = '', onCommit, onCancel } = {}) {
  if (!row) return;

  // Starting a rename on another row cancels the previous one.
  if (active) active.cancel();

  const label = row.querySelector('.lbl');
  if (!label) return;

  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'rename-input';
  input.value = value;
  input.spellcheck = false;
  input.autocomplete = 'off';
  input.setAttribute('aria-label', '重命名');
  input.maxLength = 200;

  label.replaceWith(input);
  row.classList.add('editing');
  input.focus();
  input.select();

  let done = false;
  let composing = false;

  function finish(mode) {
    if (done) return;
    done = true;
    const next = input.value.trim();
    if (input.parentNode) input.replaceWith(label);
    row.classList.remove('editing');
    if (active && active.input === input) active = null;
    if (mode === 'cancel') {
      if (onCancel) onCancel();
      return;
    }
    if (onCommit) onCommit(next);
  }

  active = { input, cancel: () => finish('cancel') };

  input.addEventListener('compositionstart', () => { composing = true; });
  input.addEventListener('compositionend', () => { composing = false; });

  input.addEventListener('keydown', (event) => {
    // Keep all keys inside the editor: no global shortcut should see them.
    event.stopPropagation();
    if (event.key === 'Escape') {
      event.preventDefault();
      finish('cancel');
      return;
    }
    if (event.key === 'Enter') {
      // isComposing / keyCode 229 are the two signals browsers give for an
      // active IME candidate window; submitting here would truncate the text.
      const imeActive = composing || event.isComposing || event.keyCode === 229;
      if (imeActive || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      finish('commit');
    }
  });

  input.addEventListener('blur', () => {
    // Clicking elsewhere commits. finish() only swaps this row's label, so the
    // pointer's click still reaches whatever the user clicked.
    if (!done) finish('commit');
  });
}
