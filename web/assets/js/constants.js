// Shared localStorage keys. Values used by a single module stay local to it;
// this module exists so keys read by more than one module cannot drift apart.

export const STORAGE_KEY = {
  THEME: 'soap-web-theme',
  COLLAPSED: 'soap-web-collapsed',
  COLUMNS: 'soap-web-cols-v2',
  LEGACY_COLUMNS: 'soap-web-cols',
  AUTO_FORMAT: 'soap-web-autoformat',
  MASK_SECRETS: 'soap-web-mask-secrets',
};

// SENSITIVE_HEADERS are masked by default in the header editor: these carry
// credentials far more often than not.
const SENSITIVE_HEADERS = ['authorization', 'proxy-authorization', 'cookie', 'set-cookie'];

// SENSITIVE_FRAGMENTS catch the naming conventions people actually use for
// keys and tokens (x-api-key, app-token, client-secret).
const SENSITIVE_FRAGMENTS = ['token', 'secret', 'password', 'apikey', 'api-key', 'x-auth'];

// isSensitiveName reports whether a header should be masked by default and
// flagged when copying a request, so a password is not leaked by accident.
export function isSensitiveName(name) {
  const lower = String(name || '').trim().toLowerCase();
  if (!lower) return false;
  if (SENSITIVE_HEADERS.includes(lower)) return true;
  return SENSITIVE_FRAGMENTS.some((frag) => lower.includes(frag));
}

// TIME limits. The numbers below are the fallback used before /api/health has
// answered; the server is the authority and applyServerLimits() replaces them,
// so the editor can never accept a timeout the server would silently clamp (or
// reject one it would accept). A local copy is still needed because the editor
// renders before the first health probe returns.
export const TIMEOUT = {
  MIN: 1000,
  DEFAULT: 60000,
  MAX: 600000,
};

// applyServerLimits adopts the bounds reported by /api/health. Missing or
// nonsensical values are ignored so a stale/partial response cannot break
// validation; the previous numbers stay in place.
export function applyServerLimits(limits) {
  if (!limits) return false;
  const min = Number(limits.minTimeoutMs);
  const def = Number(limits.defaultTimeoutMs);
  const max = Number(limits.maxTimeoutMs);
  if (!Number.isFinite(min) || !Number.isFinite(def) || !Number.isFinite(max)) return false;
  if (min <= 0 || max < min || def < min || def > max) return false;
  if (TIMEOUT.MIN === min && TIMEOUT.DEFAULT === def && TIMEOUT.MAX === max) return false;
  TIMEOUT.MIN = min;
  TIMEOUT.DEFAULT = def;
  TIMEOUT.MAX = max;
  return true;
}
