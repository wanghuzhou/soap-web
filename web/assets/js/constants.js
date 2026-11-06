// Shared constants. Keeping these in one module means the node kinds, tree
// actions, view names, proxy modes and storage keys cannot drift apart between
// modules that must agree on them.

// Node kinds used in state.selected, tree row datasets and API paths.
export const NODE_KIND = {
  PROJECT: 'proj',
  SERVICE: 'svc',
  OPERATION: 'op',
  CALL: 'call',
};

// Actions dispatched by tree row buttons (data-act).
export const TREE_ACTION = {
  ADD_SERVICE: 'add-svc',
  ADD_OPERATION: 'add-op',
  ADD_CALL: 'add-call',
  DELETE_PROJECT: 'del-proj',
  DELETE_SERVICE: 'del-svc',
  DELETE_OPERATION: 'del-op',
  DELETE_CALL: 'del-call',
  RENAME_PROJECT: 'rename-proj',
  RENAME_SERVICE: 'rename-svc',
  RENAME_OPERATION: 'rename-op',
  RENAME_CALL: 'rename-call',
  PROXY_PROJECT: 'proxy-proj',
  EXPORT_PROJECT: 'export-proj',
};

// Response pane views.
export const RESP_VIEW = {
  PRETTY: 'pretty',
  RAW: 'raw',
  HEADERS: 'headers',
};

// Proxy modes accepted by the backend.
export const PROXY_MODE = {
  SYSTEM: 'system',
  DIRECT: 'direct',
  CUSTOM: 'custom',
};

// localStorage keys.
export const STORAGE_KEY = {
  THEME: 'soap-web-theme',
  COLLAPSED: 'soap-web-collapsed',
  COLUMNS: 'soap-web-cols-v2',
  LEGACY_COLUMNS: 'soap-web-cols',
  AUTO_FORMAT: 'soap-web-autoformat',
};

// Indent unit shared by the editor's Tab handling and prettyXML.
export const INDENT = '  ';

// SEND_TIMEOUT_MS bounds a single request; the server enforces its own copy.
export const SEND_TIMEOUT_MS = 60_000;

// TOAST_MS is how long a toast stays on screen.
export const TOAST_MS = 3200;

// HEALTH_INTERVAL_MS is the backend heartbeat period.
export const HEALTH_INTERVAL_MS = 10_000;
