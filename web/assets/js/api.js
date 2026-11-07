// JSON API client. All server errors are normalised into Error(message).
//
// Every call goes through request(), which adds:
//   - a timeout (AbortController), so a wedged backend cannot hang the UI
//     forever;
//   - cache: 'no-store', because this is a live local tool: /api/projects is
//     re-read after every write, and a cached response would hide the change;
//   - the X-Workspace-Id header, so the server can refuse a request from a tab
//     that still points at a workspace another tab has switched away from;
//   - optional caller-supplied abort signal, used by the editor to cancel a
//     send or discard a superseded one.

const DEFAULT_TIMEOUT_MS = 30_000;
const WORKSPACE_HEADER = 'X-Workspace-Id';

// workspaceID is the workspace this page loaded. Kept in this module (instead
// of importing state) so every request path shares one source of truth without
// a module cycle.
let workspaceID = null;
let mismatchHandler = null;

export function setWorkspaceID(id) {
  workspaceID = id || null;
}

// setWorkspaceMismatchHandler registers the callback invoked when the server
// answers 409 workspace_mismatch. The workspace module uses it to mark the tab
// as stale (or resync automatically).
export function setWorkspaceMismatchHandler(fn) {
  mismatchHandler = fn;
}

// request performs a fetch and returns the raw Response.
// The caller-supplied signal (if any) is chained to the internal timeout.
async function request(url, init = {}, timeoutMs = DEFAULT_TIMEOUT_MS) {
  const controller = new AbortController();
  const timer = timeoutMs > 0 ? setTimeout(() => controller.abort(), timeoutMs) : null;

  // Abort our request when the caller aborts theirs.
  const external = init.signal;
  if (external) {
    if (external.aborted) controller.abort();
    else external.addEventListener('abort', () => controller.abort(), { once: true });
  }

  const headers = new Headers(init.headers || {});
  if (workspaceID) headers.set(WORKSPACE_HEADER, workspaceID);

  try {
    return await fetch(url, { ...init, headers, signal: controller.signal, cache: 'no-store' });
  } finally {
    if (timer) clearTimeout(timer);
  }
}

async function readJSON(response) {
  const text = await response.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch (e) { data = null; }
  }
  if (!response.ok) {
    const message = (data && (data.error || data.message)) || ('请求失败 (' + response.status + ')');
    // The workspace changed under this tab. Notify the workspace module before
    // throwing so the UI can offer to resync instead of silently retrying.
    if (response.status === 409 && data && data.code === 'workspace_mismatch' && mismatchHandler) {
      try { mismatchHandler(data.workspace); } catch (e) { /* never mask the API error */ }
    }
    const error = new Error(message);
    error.status = response.status;
    error.code = (data && data.code) || '';
    throw error;
  }
  return data;
}

export async function apiGet(url, options = {}) {
  return readJSON(await request(url, { method: 'GET', signal: options.signal }, options.timeoutMs));
}

export async function apiJSON(method, url, body, options = {}) {
  return readJSON(await request(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal: options.signal,
  }, options.timeoutMs));
}

export async function apiUpload(url, file, options = {}) {
  const form = new FormData();
  form.append('file', file);
  return readJSON(await request(url, {
    method: 'POST',
    body: form,
    signal: options.signal,
  }, options.timeoutMs));
}

// apiSend posts a SOAP request. The timeout comes from the payload (the server
// honours its own copy too), and an abort signal lets the UI cancel a send.
export async function apiSend(payload, options = {}) {
  const timeoutMs = options.timeoutMs != null
    ? options.timeoutMs
    : (payload && payload.timeoutMs ? payload.timeoutMs + 5000 : DEFAULT_TIMEOUT_MS);
  return apiJSON('POST', '/api/send', payload, { signal: options.signal, timeoutMs });
}

// apiHealth probes /api/health and returns the payload (which includes the
// server's current workspace), or null when the backend is unreachable. It
// never throws: the badge should switch to an offline state, not show an error.
export async function apiHealth(timeoutMs = 3000) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch('/api/health', {
      signal: controller.signal,
      cache: 'no-store',
    });
    if (!response.ok) return null;
    const data = await response.json().catch(() => null);
    if (!data || data.status !== 'ok') return null;
    return data;
  } catch (error) {
    return null;
  } finally {
    clearTimeout(timer);
  }
}
