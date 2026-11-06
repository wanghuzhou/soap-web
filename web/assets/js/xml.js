// XML helpers: newline normalisation, pretty printing, minifying and the
// lightweight syntax highlighter used by the request/response panes.

export function escapeHtml(value) {
  return String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// MAX_INDENT_DEPTH caps indentation growth. Indent cost is O(depth) per line,
// so a pathologically deep (or truncated) payload would otherwise expand to tens
// of megabytes of whitespace that then gets pushed into innerHTML. Real SOAP
// documents rarely exceed ~20 levels; beyond this we simply stop indenting.
const MAX_INDENT_DEPTH = 64;

// Pretty-print XML with indentation.
//
// Whitespace is significant in XML text content: a value such as "  abc  ", a
// preserved blank line, or the leading newline of a multi-line base64 payload
// must survive verbatim. Re-indenting an element that carries text would
// silently rewrite the payload, and the editor persists what it shows, so the
// damage would be permanent.
//
// Therefore this only reflows where reflowing is provably safe:
//   * element-only content (children, no significant text) -> re-indented;
//   * an element whose content contains significant text (text-only or mixed)
//     -> emitted verbatim, byte for byte;
//   * CDATA / comments / processing instructions keep their inner content.
// Pure inter-element indentation (whitespace containing a newline) is dropped,
// because it carries no meaning in an element-only content model.
//
// Returns input unchanged if it does not look like XML.
export function prettyXML(xml) {
  const text = xml == null ? '' : xml;
  if (!text.trim() || text.trim()[0] !== '<') return text;

  // Tokenise into CDATA, comments, PIs, opening/closing/self-closing tags, text.
  // `index` is kept so verbatim runs can be sliced from the original source.
  const re = /<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->|<\?[\s\S]*?\?>|<\/?[^>]+>|[^<]+/g;
  const tokens = [];
  let match;
  while ((match = re.exec(text)) !== null) {
    tokens.push({ raw: match[0], index: match.index, kind: classifyToken(match[0]) });
  }

  // Pair every opening tag with its closing tag, so we can tell in O(n) whether
  // a given element's content contains text (and thus must stay verbatim).
  const closeOf = new Array(tokens.length).fill(-1);
  {
    const stack = [];
    for (let i = 0; i < tokens.length; i++) {
      if (tokens[i].kind === 'open') stack.push(i);
      else if (tokens[i].kind === 'close' && stack.length) closeOf[stack.pop()] = i;
    }
  }

  let out = '';
  let pad = 0;
  const padStr = () => (pad > MAX_INDENT_DEPTH ? '' : '  '.repeat(pad));

  for (let i = 0; i < tokens.length; i++) {
    const { raw, index, kind } = tokens[i];

    // Pure indentation between elements: insignificant, drop it.
    if (kind === 'text' && isIndentOnly(raw)) continue;

    if (kind === 'close') {
      pad = Math.max(0, pad - 1);
      out += padStr() + raw.trim() + '\n';
      continue;
    }

    if (kind === 'open') {
      const close = closeOf[i];
      if (close < 0) {
        // Unbalanced markup (truncated payload). Emit as-is, do not guess.
        out += padStr() + raw + '\n';
        continue;
      }
      if (contentHasText(tokens, i + 1, close)) {
        // Text content is significant: preserve the whole element verbatim.
        const start = index;
        const end = tokens[close].index + tokens[close].raw.length;
        out += padStr() + text.slice(start, end) + '\n';
        i = close;
        continue;
      }
      if (close === i + 1) {
        // Empty element: <a></a> stays on one line.
        out += padStr() + raw.trim() + tokens[close].raw.trim() + '\n';
        i = close;
        continue;
      }
      out += padStr() + raw.trim() + '\n';
      pad++;
      continue;
    }

    // self / cdata / comment / pi / stray text
    out += padStr() + raw.trim() + '\n';
  }
  return out.replace(/\n$/, '');
}

// classifyToken tags a token from prettyXML's tokenizer.
function classifyToken(token) {
  if (token.startsWith('<![CDATA[')) return 'cdata';
  if (token.startsWith('<!--')) return 'comment';
  if (token.startsWith('<?')) return 'pi';
  if (/^<\//.test(token)) return 'close';
  if (/^<[^>]+\/>$/.test(token)) return 'self';
  if (/^<[^>]+>$/.test(token)) return 'open';
  if (token.startsWith('<')) return 'other';
  return 'text';
}

// isIndentOnly reports whether a text token is nothing but formatting
// whitespace (it must contain a newline; "  " inside <a>  </a> may be content).
function isIndentOnly(raw) {
  return /^[ \t\r\n]*$/.test(raw) && raw.indexOf('\n') >= 0;
}

// contentHasText reports whether the *direct* content of an element is
// significant, i.e. whether re-indenting it would alter the payload.
//
// Descendant text does not count: <a><b>1</b></a> is element-only content and
// can be reflowed freely. Only text that sits directly inside the element
// matters, so the scan tracks nesting depth.
//
// CDATA is treated as significant too: it normally carries real data (base64,
// embedded XML, SQL) and injecting indentation around it changes the content
// model of the element.
function contentHasText(tokens, from, to) {
  let depth = 0;
  for (let i = from; i < to; i++) {
    const { raw, kind } = tokens[i];
    if (kind === 'open') {
      depth++;
    } else if (kind === 'close') {
      depth = Math.max(0, depth - 1);
    } else if (depth === 0) {
      if (kind === 'cdata') return true;
      if (kind === 'text' && !isIndentOnly(raw)) return true;
    }
  }
  return false;
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
// sequence "\r" / "\n" *followed by a real newline*. Normalise only that exact
// shape: the trailing newline is what makes it an artifact rather than content.
//
// Do NOT widen this to a bare /\\(r|n)/ replace. A lone backslash-n inside a
// payload is ordinary data (Windows paths such as C:\new\reports, JSON strings,
// regexes, base64) and rewriting it silently corrupts the request body, which
// the editor then persists on save.
export function normalizeNewlines(value) {
  let s = value == null ? '' : String(value);
  // Collapse literal "\r"/"\n" markers that sit at the end of a real line.
  s = s.replace(/(?:\\r|\\n)+[ \t]*\r?\n/g, '\n');
  // Finally normalise genuine CRLF / CR to LF.
  s = s.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
  return s;
}

export function formatSize(bytes) {
  if (!bytes) return '0 B';
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1024 / 1024).toFixed(2) + ' MB';
}
