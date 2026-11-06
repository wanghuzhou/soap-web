// Application state plus pure lookup helpers for the project tree.

import { STORAGE_KEY } from './constants.js';

export const state = {
  projects: [],
  selected: null, // { kind, node, ids: { pid, sid, oid, cid }, parentOp }
  response: null,
  view: 'pretty',
  theme: 'dark',
  collapsed: {},
};

// saveCollapsed is debounced: toggling a caret used to trigger a synchronous
// localStorage write of the whole map on every click, which blocks the main
// thread on disk I/O. Writing once after the user stops clicking is enough.
let saveTimer = 0;

export function saveCollapsed() {
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    saveTimer = 0;
    try {
      localStorage.setItem(STORAGE_KEY.COLLAPSED, JSON.stringify(state.collapsed));
    } catch (e) { /* storage may be unavailable */ }
  }, 300);
}

export function initCollapsed() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY.COLLAPSED);
    if (saved) state.collapsed = JSON.parse(saved) || {};
  } catch (e) {
    state.collapsed = {};
  }
}

export function toggleCollapsed(key) {
  // Expanded is the default, so an expanded key is removed rather than stored
  // as false. Otherwise the map grows forever as nodes are opened and closed.
  if (state.collapsed[key]) delete state.collapsed[key];
  else state.collapsed[key] = true;
  saveCollapsed();
}

// pruneCollapsed drops keys that no longer correspond to a node.
//
// Project/service/operation ids are random hex generated on import, so
// re-importing the same SoapUI project produces all-new ids and the old keys
// would linger as orphans forever. Called after each render with the set of ids
// that still exist.
export function pruneCollapsed(validKeys) {
  let changed = false;
  Object.keys(state.collapsed).forEach((key) => {
    if (!validKeys.has(key)) {
      delete state.collapsed[key];
      changed = true;
    }
  });
  if (changed) saveCollapsed();
}

export function findProject(projectID) {
  return (state.projects || []).find((project) => project.id === projectID) || null;
}

export function findService(projectID, serviceID) {
  const project = findProject(projectID);
  const service = project && (project.services || []).find((item) => item.id === serviceID);
  return service ? { project, service } : null;
}

export function findOperation(projectID, serviceID, operationID) {
  const found = findService(projectID, serviceID);
  const operation = found && (found.service.operations || []).find((item) => item.id === operationID);
  return operation ? { ...found, operation } : null;
}

export function findCall(ids) {
  const found = findOperation(ids.pid, ids.sid, ids.oid);
  const call = found && (found.operation.calls || []).find((item) => item.id === ids.cid);
  return call ? { ...found, call } : null;
}
