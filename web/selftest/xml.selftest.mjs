// Frontend regression checks for web/assets/js/*.js.
//
// The project has no build step and no JS test runner, so this script runs the
// real ES modules directly under Node and asserts the invariants that the
// editor's XML pipeline depends on. Run with:
//
//     node web/selftest/xml.selftest.mjs
//
// It lives outside web/assets/ on purpose: everything under web/assets is
// embedded into the binary and served to the browser via embed.FS.
//
// It exits non-zero on failure so it can gate CI alongside `go test ./...`.

import { prettyXML, minifyXML, normalizeNewlines, highlightXML, formatSize } from '../assets/js/xml.js';

let failures = 0;
let checks = 0;

function check(label, actual, expected) {
  checks++;
  const ok = actual === expected;
  if (!ok) {
    failures++;
    console.error(`FAIL ${label}`);
    console.error(`  expected: ${JSON.stringify(expected)}`);
    console.error(`  actual:   ${JSON.stringify(actual)}`);
  }
}

function checkTrue(label, cond, detail) {
  checks++;
  if (!cond) {
    failures++;
    console.error(`FAIL ${label}${detail ? ' — ' + detail : ''}`);
  }
}

// ---- normalizeNewlines -----------------------------------------------------
// Regression: a bare /\\(r|n)/ replace rewrote ordinary data such as Windows
// paths, JSON escapes and base64, and the editor then persisted the damage.

const literalData = [
  ['windows path', '<r><p>C:\\new\\reports\\2024.xlsx</p></r>'],
  ['json newline', '<r>{"a":"x\\ny"}</r>'],
  ['json crlf', '<r>{"a":"x\\r\\ny"}</r>'],
  ['regex', '<r>\\r\\n|\\n</r>'],
  ['base64', '<r>QUJD\\nREVGRw==</r>'],
  ['backslash t', '<r>a\\tb</r>'],
];
for (const [label, input] of literalData) {
  check(`normalizeNewlines keeps ${label}`, normalizeNewlines(input), input);
}

// Genuine SoapUI artifacts are a literal \r/\n marker followed by a real
// newline; those must still collapse.
check(
  'normalizeNewlines collapses artifact',
  normalizeNewlines('<r>\\r\n  <a>1</a>\n</r>'),
  '<r>\n  <a>1</a>\n</r>',
);
check('normalizeNewlines normalises CRLF', normalizeNewlines('<r>\r\n<a/>\r\n</r>'), '<r>\n<a/>\n</r>');

// ---- prettyXML: semantic preservation ---------------------------------------
// prettyXML must never change the token stream. Re-indenting element-only
// content is fine; rewriting text content is not.

const TOKEN_RE = /<!\[CDATA\[[\s\S]*?\]\]>|<!--[\s\S]*?-->|<\?[\s\S]*?\?>|<\/?[^>]+>|[^<]+/g;

// significantParts extracts the tag sequence and the text/CDATA runs that carry
// meaning (pure indentation is excluded, since it is legitimately reflowed).
function significantParts(text) {
  const tokens = text.match(TOKEN_RE) || [];
  const tags = [];
  const texts = [];
  for (const token of tokens) {
    if (token.startsWith('<')) {
      tags.push(token.trim());
    } else if (!(token.trim() === '' && token.includes('\n'))) {
      texts.push(token);
    }
  }
  return { tags, texts };
}

const semanticCases = [
  ['surrounding spaces', '<ns:Token>  abc  </ns:Token>'],
  ['mixed content', '<r>Hello <b>x</b> world</r>'],
  ['xml:space preserve', '<r xml:space="preserve">  保持  </r>'],
  ['multiline base64', '<data>QUJD\nREVGRw==\n</data>'],
  ['single-line spaces', '<a>   </a>'],
  ['leading spaces', '<sig> 0012 </sig>'],
  ['empty element', '<a></a>'],
  ['cdata', '<r><![CDATA[ a < b ]]></r>'],
  ['comment', '<r><!-- hi --><a/></r>'],
  ['text with newline', '<r>line1\nline2</r>'],
  ['nested element content', '<r><a><b>1</b></a><c/></r>'],
  ['deep nesting', '<a>'.repeat(200) + 'x' + '</a>'.repeat(200)],
];

for (const [label, input] of semanticCases) {
  const out = prettyXML(input);
  const before = significantParts(input);
  const after = significantParts(out);
  checkTrue(
    `prettyXML preserves tags for ${label}`,
    JSON.stringify(before.tags) === JSON.stringify(after.tags),
    `in=${JSON.stringify(before.tags)} out=${JSON.stringify(after.tags)}`,
  );
  checkTrue(
    `prettyXML preserves text for ${label}`,
    JSON.stringify(before.texts) === JSON.stringify(after.texts),
    `in=${JSON.stringify(before.texts)} out=${JSON.stringify(after.texts)}`,
  );
}

// Element-only content should still be re-indented (the feature must work).
const prettyDoc = '<?xml version="1.0"?><a><b><c>1</c></b></e>'.replace('</e>', '</a>');
checkTrue(
  'prettyXML indents element-only content',
  prettyXML(prettyDoc).includes('\n  <b>'),
  prettyXML(prettyDoc),
);

// Indentation must be bounded so a pathological payload cannot explode.
const deep = '<a>'.repeat(4000) + 'x' + '</a>'.repeat(4000);
checkTrue('prettyXML bounds indent growth', prettyXML(deep).length < 4096 * 1024,
  `output ${(prettyXML(deep).length / 1024).toFixed(0)} KB`);

// ---- minifyXML -------------------------------------------------------------
check('minifyXML strips inter-tag whitespace', minifyXML('<a>\n  <b/>\n</a>'), '<a><b/></a>');
check('minifyXML keeps CDATA', minifyXML('<a><![CDATA[ x < y ]]></a>'), '<a><![CDATA[ x < y ]]></a>');
check('minifyXML keeps comments', minifyXML('<a><!-- c --></a>'), '<a><!-- c --></a>');

// ---- highlightXML ----------------------------------------------------------
// The highlighter must escape markup so user data cannot inject HTML.
const xss = '<r><a>&lt;script&gt;alert(1)&lt;/script&gt;</a></r>';
const highlighted = highlightXML(xss);
checkTrue('highlightXML escapes angle brackets', !highlighted.includes('<script>'), highlighted);
// A pre-escaped "&lt;" must survive as a literal entity, i.e. be re-escaped to
// "&amp;lt;", never decoded into a real "<".
checkTrue('highlightXML double-escapes entities', highlighted.includes('&amp;lt;'), highlighted);
checkTrue('highlightXML never emits raw <script', !/<script/i.test(highlighted), highlighted);

// Round-trip: stripping spans must recover the escaped original.
const stripped = highlighted.replace(/<span class="[^"]*">|<\/span>/g, '');
const escaped = xss.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
check('highlightXML round-trips', stripped, escaped);

// ---- formatSize ------------------------------------------------------------
check('formatSize zero', formatSize(0), '0 B');
check('formatSize bytes', formatSize(999), '999 B');
check('formatSize KB', formatSize(2048), '2.0 KB');
check('formatSize MB', formatSize(3 * 1024 * 1024), '3.00 MB');

// ---- report ----------------------------------------------------------------
if (failures === 0) {
  console.log(`OK ${checks} checks passed`);
} else {
  console.error(`${failures}/${checks} checks FAILED`);
  process.exitCode = 1;
}
