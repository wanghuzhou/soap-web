// XML helpers: newline normalisation, pretty printing, minifying and the
// lightweight syntax highlighter used by the request/response panes.

export function escapeHtml(value) {
  return String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// Pretty-print XML with indentation. CDATA and comments keep their inner
// content untouched (whitespace inside CDATA is significant and must not be
// reflowed). Returns input unchanged if it does not look like XML.
export function prettyXML(xml) {
  const text = xml == null ? '' : xml;
  if (!text.trim() || text.trim()[0] !== '<') return text;

  // Tokenise into CDATA, comments, PIs, opening/closing/self-closing tags, text.
  const re = /<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->|<\?[\s\S]*?\?>|<\/?[^>]+>|[^<]+/g;
  const tokens = [];
  let match;
  while ((match = re.exec(text)) !== null) tokens.push(match[0]);

  let out = '';
  let pad = 0;
  const padStr = () => '  '.repeat(pad);

  for (let i = 0; i < tokens.length; i++) {
    const raw = tokens[i];
    const token = raw.trim();
    if (token === '') continue;

    const isCdata = token.startsWith('<![CDATA[');
    const isComment = token.startsWith('<!--');
    const isPI = token.startsWith('<?');
    const isClose = /^<\//.test(token);
    const isSelf = /^<[^>]+\/>$/.test(token);
    const isOpen = /^<[^>]+>$/.test(token) && !isClose && !isSelf;

    if (isCdata || isComment || isPI) {
      out += padStr() + token + '\n';
    } else if (isClose) {
      pad = Math.max(0, pad - 1);
      out += padStr() + token + '\n';
    } else if (isSelf) {
      out += padStr() + token + '\n';
    } else if (isOpen) {
      // Keep simple text-only elements on one line: <tag>value</tag>.
      let j = i + 1;
      while (j < tokens.length && tokens[j].trim() === '') j++;
      const next = j < tokens.length ? tokens[j] : '';
      const nextIsText = next !== '' && !next.trim().startsWith('<');
      if (nextIsText) {
        let k = j + 1;
        while (k < tokens.length && tokens[k].trim() === '') k++;
        const close = k < tokens.length ? tokens[k].trim() : '';
        out += padStr() + token + next.trim() + close + '\n';
        i = k; // consume text + closing tag
      } else {
        out += padStr() + token + '\n';
        pad++;
      }
    } else {
      out += padStr() + token + '\n';
    }
  }
  return out.replace(/\n$/, '');
}

// Minify XML: strip whitespace between tags while preserving CDATA/comments.
export function minifyXML(xml) {
  const text = xml == null ? '' : xml;
  const store = [];
  let s = text.replace(/<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->/g, (match) => {
    store.push(match);
    return '\u0000' + (store.length - 1) + '\u0000';
  });
  s = s.replace(/>\s+</g, '><').trim();
  s = s.replace(/\u0000(\d+)\u0000/g, (_, index) => store[+index]);
  return s;
}

export function highlightXML(xml) {
  let s = escapeHtml(xml);
  s = s.replace(/(&lt;!\[CDATA\[[\s\S]*?\]\]&gt;)/g, '<span class="x-cdata">$1</span>');
  s = s.replace(/(&lt;!--[\s\S]*?--&gt;)/g, '<span class="x-comment">$1</span>');
  s = s.replace(/(&lt;\/?)([\w:.-]+)((?:[^&]|&(?!gt;))*?)(\/?&gt;)/g,
    (match, open, name, attrs, close) => {
      const attrHtml = attrs.replace(/([\w:.-]+)\s*=\s*("([^"]*)"|'([^']*)')/g,
        (am, attrName, quoted) => ` <span class="x-attr">${attrName}</span>=<span class="x-val">${quoted}</span>`);
      return `<span class="x-tag">${open}${name}</span>${attrHtml}<span class="x-tag">${close}</span>`;
    });
  return s;
}

// SoapUI stores line breaks in some projects as the literal two-character
// sequence "\r" / "\n" followed by a newline. Normalise these to real newlines
// so the editor shows clean, readable XML instead of stray "\r" tokens.
export function normalizeNewlines(value) {
  let s = value == null ? '' : String(value);
  s = s.replace(/\\(r|n)(\\r|\\n)?[ \t]*\r?\n/g, '\n');
  s = s.replace(/\\(r|n)(\\r|\\n)?/g, '\n');
  s = s.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
  return s;
}

export function formatSize(bytes) {
  if (!bytes) return '0 B';
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1024 / 1024).toFixed(2) + ' MB';
}
