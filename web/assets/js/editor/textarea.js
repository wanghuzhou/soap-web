// Textarea editing: syntax-highlight sync and Tab / Shift+Tab indentation.

import { $ } from '../dom.js';
import { highlightXML } from '../xml.js';

// INDENT is the editor's indent unit (two spaces). Kept in sync with the
// indentation prettyXML emits.
const INDENT = '  ';

// initTextarea wires the highlighted textarea: input re-renders the highlight,
// scroll keeps it aligned, and Tab indents the selection.
export function initTextarea() {
  const textarea = $('requestBody');
  if (!textarea) return;
  textarea.addEventListener('input', scheduleHighlight);
  textarea.addEventListener('scroll', syncScroll, { passive: true });
  textarea.addEventListener('keydown', handleEditorKeydown);
  // 面板宽度（拖动分栏）变化、以及内容溢出导致滚动条出现/消失，都会改变
  // textarea 的文字宽度，必须跟着重算。观察 textarea 自身的内容盒最可靠：
  // 滚动条一出现，内容盒就变窄，回调立即触发。
  if (typeof ResizeObserver !== 'undefined') {
    const ro = new ResizeObserver(() => syncMetrics());
    ro.observe(textarea);
    const wrap = $('editorWrap');
    if (wrap) ro.observe(wrap);
  }
  syncMetrics();
}

// syncMetrics makes the highlight layer use exactly the textarea's text width.
//
// The two layers wrap independently, so any difference in text width makes them
// break lines at different characters: the caret (drawn by the textarea) then
// sits on a different character than the one the highlight layer paints there,
// and typing lands somewhere other than where the caret appears.
//
// The textarea's scrollbar is what breaks a hardcoded padding: it is 0px with
// macOS overlay scrollbars, 10px inside WKWebView, 16px when the system is set
// to always show scroll bars. Measuring it is the only way to stay aligned.
function syncMetrics() {
  const textarea = $('requestBody');
  const highlight = $('requestHighlight');
  if (!textarea || !highlight) return;
  const t = getComputedStyle(textarea);
  const h = getComputedStyle(highlight);
  const textWidth = textarea.clientWidth
    - parseFloat(t.paddingLeft) - parseFloat(t.paddingRight);
  // clientWidth already excludes the scrollbar, which is exactly the width the
  // text wraps in. Give the highlight layer whatever right padding is left over.
  const right = highlight.clientWidth - parseFloat(h.paddingLeft) - textWidth;
  if (right > 0) highlight.style.paddingRight = right + 'px';
}

// scheduleHighlight coalesces a burst of input events into one highlight pass
// per animation frame, so rapid typing or IME composition does not re-tokenize
// the whole document for every event.
let highlightFrame = 0;
function scheduleHighlight() {
  if (highlightFrame) return;
  highlightFrame = requestAnimationFrame(() => {
    highlightFrame = 0;
    renderHighlight();
  });
}

export function renderHighlight() {
  const textarea = $('requestBody');
  const highlight = $('requestHighlight');
  if (!textarea || !highlight) return;
  syncMetrics();
  highlight.innerHTML = highlightXML(textarea.value) + '\n';
  syncScroll();
}

export function syncScroll() {
  const textarea = $('requestBody');
  const highlight = $('requestHighlight');
  if (!textarea || !highlight) return;
  highlight.scrollTop = textarea.scrollTop;
  highlight.scrollLeft = textarea.scrollLeft;
}

// handleEditorKeydown implements Tab / Shift+Tab. It indents every line in a
// multi-line selection and uses execCommand/setRangeText so undo still works.
function handleEditorKeydown(event) {
  if (event.key !== 'Tab') return;
  event.preventDefault();
  const textarea = event.target;
  const value = textarea.value;
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;
  const outdent = event.shiftKey;

  // Single caret: insert / remove one indent level.
  if (start === end) {
    if (!outdent) {
      insertText(textarea, INDENT);
      return;
    }
    // Shift+Tab with no selection removes up to INDENT.length leading spaces.
    const lineStart = value.lastIndexOf('\n', start - 1) + 1;
    let remove = 0;
    while (remove < INDENT.length && value[lineStart + remove] === ' ') remove++;
    if (remove === 0) return;
    textarea.setSelectionRange(lineStart, lineStart + remove);
    insertText(textarea, '');
    textarea.setSelectionRange(Math.max(lineStart, start - remove), Math.max(lineStart, start - remove));
    renderHighlight();
    return;
  }

  // Multi-line selection: operate per line.
  const blockStart = value.lastIndexOf('\n', start - 1) + 1;
  // Include the trailing line when the selection ends exactly at its start.
  let blockEnd = end;
  if (blockEnd > 0 && value[blockEnd - 1] === '\n') blockEnd -= 1;
  const blockEndIdx = value.indexOf('\n', blockEnd);
  const sliceEnd = blockEndIdx === -1 ? value.length : blockEndIdx;

  const lines = value.slice(blockStart, sliceEnd).split('\n');
  let newStart = start;
  let newEnd = end;
  const next = lines.map((line, i) => {
    if (!outdent) {
      // Every line in the block gains an indent, so the selection grows by one
      // indent per line (the first line's gain is absorbed by the offset).
      newEnd += INDENT.length;
      if (i === 0) newStart += INDENT.length;
      return INDENT + line;
    }
    const remove = Math.min(INDENT.length, line.length - line.replace(/^ +/, '').length);
    newEnd -= remove;
    if (i === 0) newStart = Math.max(blockStart, newStart - remove);
    return line.slice(remove);
  }).join('\n');

  // setRangeText keeps the operation a single undo step.
  textarea.setSelectionRange(blockStart, sliceEnd);
  textarea.setRangeText(next, blockStart, sliceEnd, 'preserve');
  textarea.setSelectionRange(newStart, newEnd);
  renderHighlight();
}

// insertText replaces the current selection. execCommand keeps the browser's
// native undo history intact; setRangeText is the fallback when it is
// unavailable (e.g. not focused, or unsupported).
function insertText(textarea, text) {
  if (text === '' || document.execCommand) {
    try {
      textarea.focus();
      if (document.execCommand('insertText', false, text)) return;
    } catch (e) { /* fall through */ }
  }
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;
  textarea.setRangeText(text, start, end, 'end');
}
