// In-place rename for tree rows. Only the label node is swapped (rebuilding the
// tree would swallow the click), the original label is restored on failure, and
// keydown is stopped so global shortcuts cannot fire while typing. IME Enter
// confirms the candidate instead of submitting.

let active = null;

// isRenaming reports whether an in-place rename is in progress.
// Uses the authoritative module state instead of querying the DOM: the previous
// implementation ran a document-wide querySelector on every keystroke.
export function isRenaming() {
  return !!active;
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
