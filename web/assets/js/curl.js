// Builds a copy-pasteable curl command for the current request.
//
// The shape follows what Postman and browser devtools emit, because that is
// what other HTTP tools (and people) expect to paste:
//
//   curl --location --request POST 'http://host/svc' \
//   --header 'Content-Type: text/xml; charset=utf-8' \
//   --data-raw '<soapenv:Envelope ...>'
//
// Notes on the choices:
//   - The URL comes right after --request, not at the end. Some importers read
//     the target from that position.
//   - --header / --data-raw are the long spellings; -H / -d are shorter but
//     less portable through copy-paste and importers.
//   - --data-raw (not --data-binary) keeps the body byte-for-byte, including
//     newlines, and matches what the reference output uses.
//   - Arguments are single-quoted for bash/zsh (Git Bash and WSL on Windows).
//     An embedded quote is escaped the POSIX way: close, emit \', reopen.

// Chrome's copy-as-curl has no body limit, but a 16 MB one-line command is
// unusable in a terminal; 512 KB covers real SOAP payloads with room to spare.
const MAX_BODY_BYTES = 512 * 1024;

// quote wraps a value in single quotes for bash, escaping any embedded quote.
export function quote(value) {
  return "'" + String(value).replace(/'/g, `'\\''`) + "'";
}

// buildCurl returns the command, or throws with a user-readable reason.
// timeoutMs is accepted for call-site symmetry but is not emitted; see above.
export function buildCurl({ url, method, headers, body }) {
  if (!url) throw new Error('请先填写 Endpoint');

  const verb = (method || 'POST').toUpperCase();
  // A verbatim newline inside a single-quoted argument is valid bash and keeps
  // the XML readable across the continuation lines, which is how the other
  // tools format it too.
  //
  // The timeout is deliberately not emitted: the reference format (Postman,
  // devtools) has no --max-time, and a copied command is usually re-run by hand
  // where the shell can be interrupted. Add it back if a timeout is wanted.
  const lines = [`curl --location --request ${verb} ${quote(url)}`];

  Object.entries(headers || {}).forEach(([name, value]) => {
    if (!String(name).trim()) return;
    lines.push('--header ' + quote(name + ': ' + value));
  });

  const payload = body || '';
  if (payload) {
    if (payload.length > MAX_BODY_BYTES) {
      throw new Error(`报文太大（${Math.round(payload.length / 1024)} KB），超过 512 KB 不适合转成命令行`);
    }
    lines.push('--data-raw ' + quote(payload));
  }

  return lines.join(' \\\n');
}

// sensitiveNames returns the header names a copy would expose, so the caller
// can warn before putting a password on the clipboard.
export function sensitiveNames(headers, isSensitiveName) {
  return Object.keys(headers || {}).filter((name) => isSensitiveName(name));
}
