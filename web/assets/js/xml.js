// XML helpers: newline normalisation, pretty printing, minifying and the
// lightweight syntax highlighter used by the request/response panes.

import { escapeAttr as escapeHtml } from './dom.js';

export { escapeHtml };

// TOKEN_RE splits XML into CDATA, comments, PIs, tags and text.
//
// Every alternative is anchored by a distinct prefix and none can match the
// same input twice, so matching runs in linear time. Both prettyXML and
// highlightXML use it, so formatting and colouring always agree on what
// counts as a token.
const TOKEN_RE = /<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->|<\?[\s\S]*?\?>|<\/?[^>]+>|[^<]+/g;

// MAX_INDENT_DEPTH caps indentation growth. Indent cost is O(depth) per line,
// so a pathologically deep (or truncated) payload would otherwise expand to tens
// of megabytes of whitespace that then gets pushed into innerHTML. Real SOAP
// documents rarely exceed ~20 levels; beyond this we simply stop indenting.
const MAX_INDENT_DEPTH = 64;

// prettyXML re-indents XML without rewriting significant text. Element-only
// content is reflowed; text, mixed content, CDATA, comments and PIs stay
// verbatim; inter-element whitespace containing a newline is dropped. Returns
// the input unchanged when it does not look like XML.
export function prettyXML(xml) {
  const text = xml == null ? '' : xml;
  if (!text.trim() || text.trim()[0] !== '<') return text;

  // `index` is kept so verbatim runs can be sliced from the original source.
  const tokens = [];
  let match;
  TOKEN_RE.lastIndex = 0;
  while ((match = TOKEN_RE.exec(text)) !== null) {
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

// contentHasText reports whether an element's direct content is significant.
// Descendant text does not count; CDATA does (it carries real data).
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

// NEWLINE_RUN_RE matches whitespace containing a newline: always formatting,
// since parsing normalises it to a space. Horizontal runs may be real content.
const NEWLINE_RUN_RE = /[ \t\r\n]*\n[ \t\r\n]*/g;

// CDATA_OPEN / CDATA_CLOSE delimit a CDATA section. Kept as constants so the
// minifier never accidentally truncates a payload whose data ends in "]".
const CDATA_OPEN = '<![CDATA[';
const CDATA_CLOSE = ']]>';

// minifyXML removes formatting whitespace. Only whitespace containing a
// newline is dropped, which is safe for mixed content, xml:space="preserve"
// and base64. CDATA payloads collapse to one space; tags, comments and PIs are
// kept verbatim. One token pass keeps it linear.
export function minifyXML(xml) {
  const text = xml == null ? '' : xml;
  if (!text.trim()) return text;

  let out = '';
  const tokens = text.match(TOKEN_RE) || [];
  for (const token of tokens) {
    if (token.startsWith(CDATA_OPEN)) {
      // Keep the section delimiters; squeeze only the payload in between.
      const inner = token.slice(CDATA_OPEN.length, token.length - CDATA_CLOSE.length);
      out += CDATA_OPEN + inner.replace(NEWLINE_RUN_RE, ' ') + CDATA_CLOSE;
    } else if (token.startsWith('<')) {
      out += token;
    } else {
      // Plain text: collapse formatting newlines, drop surrounding whitespace.
      out += token.replace(NEWLINE_RUN_RE, ' ').trim();
    }
  }
  return out.trim();
}

// ATTR_RE matches name="value" / name='value' inside a tag. The tag text is
// already delimited by TOKEN_RE, so this never has to scan past the tag.
// It runs on the *raw* (unescaped) attribute text, so quotes are still real
// quote characters here; escaping happens afterwards on the pieces.
const ATTR_RE = /([\w:.\-]+)\s*=\s*("[^"]*"|'[^']*')/g;

// highlightXML returns syntax-coloured HTML. It tokenises once and colours each
// token independently, so there is no regex backtracking or nested-span bug;
// runtime is linear in the input length.
export function highlightXML(xml) {
  const text = xml == null ? '' : String(xml);
  const tokens = text.match(TOKEN_RE);
  if (!tokens) return escapeHtml(text);

  let out = '';
  for (const token of tokens) {
    if (token.startsWith('<![CDATA[')) {
      out += '<span class="x-cdata">' + escapeHtml(token) + '</span>';
    } else if (token.startsWith('<!--')) {
      out += '<span class="x-comment">' + escapeHtml(token) + '</span>';
    } else if (token.startsWith('<?')) {
      out += '<span class="x-pi">' + escapeHtml(token) + '</span>';
    } else if (token.startsWith('<')) {
      out += highlightTag(token);
    } else {
      out += escapeHtml(token);
    }
  }
  return out;
}

// highlightTag colours one tag token. The token is already delimited, so the
// attribute regex cannot run away.
function highlightTag(token) {
  const open = token.startsWith('</') ? '</' : '<';
  const close = token.endsWith('/>') ? '/>' : '>';
  const body = token.slice(open.length, token.length - close.length);

  // Split the tag name from the attribute list at the first whitespace.
  let name = body;
  let rest = '';
  const space = body.search(/[\s]/);
  if (space >= 0) {
    name = body.slice(0, space);
    rest = body.slice(space);
  }

  // Match attributes on the RAW text (quotes are still literal here), then
  // escape each piece. Escaping first would turn " into &quot; and the
  // attribute regex would no longer see a quoted value.
  let attrs = '';
  let cursor = 0;
  ATTR_RE.lastIndex = 0;
  let m;
  while ((m = ATTR_RE.exec(rest)) !== null) {
    attrs += escapeHtml(rest.slice(cursor, m.index));
    attrs += '<span class="x-attr">' + escapeHtml(m[1]) + '</span>='
      + '<span class="x-val">' + escapeHtml(m[2]) + '</span>';
    cursor = m.index + m[0].length;
  }
  attrs += escapeHtml(rest.slice(cursor));

  return '<span class="x-tag">' + escapeHtml(open + name) + '</span>'
    + attrs
    + '<span class="x-tag">' + escapeHtml(close) + '</span>';
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
