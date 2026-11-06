// JSON API client. All server errors are normalised into Error(message).

async function readJSON(response) {
  const text = await response.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch (e) { data = null; }
  }
  if (!response.ok) {
    const message = (data && (data.error || data.message)) || ('请求失败 (' + response.status + ')');
    throw new Error(message);
  }
  return data;
}

export async function apiGet(url) {
  return readJSON(await fetch(url));
}

export async function apiJSON(method, url, body) {
  const response = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  return readJSON(response);
}

export async function apiUpload(url, file) {
  const form = new FormData();
  form.append('file', file);
  return readJSON(await fetch(url, { method: 'POST', body: form }));
}

export async function apiSend(payload) {
  return apiJSON('POST', '/api/send', payload);
}

// apiHealth probes /api/health and reports whether the local backend answers.
// It never throws: the badge should switch to an offline state, not show an error.
export async function apiHealth(timeoutMs = 3000) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch('/api/health', {
      signal: controller.signal,
      cache: 'no-store',
    });
    if (!response.ok) return false;
    const data = await response.json().catch(() => null);
    return !!data && data.status === 'ok';
  } catch (error) {
    return false;
  } finally {
    clearTimeout(timer);
  }
}
