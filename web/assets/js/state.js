// Application state plus pure lookup helpers for the project tree.

import { STORAGE_KEY } from './constants.js';

export const state = {
  projects: [],
  selected: null, // { kind, node, ids: { pid, sid, oid, cid }, parentOp }
  response: null,
  view: 'pretty',
  theme: 'dark',
  collapsed: {},
  // Workspace the page is currently showing. workspaceId is sent with every
  // API request so a switch in another tab cannot make this tab read or write
  // the wrong data.
  workspaceId: null,
  workspaceName: '',
  workspaces: [], // [{ id, name }]
  workspaceStale: false, // server moved to another workspace, this tab has not synced
};

// saveCollapsed is debounced: writing the whole map on every caret click would
// block the main thread. The workspace id and snapshot are captured when the
// save is scheduled, so a switch cannot write the new map under the old key.
let pendingSave = null; // { key, snapshot, timer }

function collapsedKey(workspaceID) {
  return STORAGE_KEY.COLLAPSED + ':' + workspaceID;
}

// flushCollapsed writes a pending debounced save immediately. Called before
// the collapse map is replaced (workspace switch) so the last caret click in
// the old workspace is never lost to the debounce window.
function flushCollapsed() {
  if (!pendingSave) return;
  const { key, snapshot, timer } = pendingSave;
  pendingSave = null;
  clearTimeout(timer);
  try {
    localStorage.setItem(key, snapshot);
  } catch (e) { /* storage may be unavailable */ }
}

export function saveCollapsed() {
  if (!state.workspaceId) return;
  flushCollapsed();
  const key = collapsedKey(state.workspaceId);
  const snapshot = JSON.stringify(state.collapsed);
  const timer = setTimeout(() => {
    try {
      localStorage.setItem(key, snapshot);
    } catch (e) { /* storage may be unavailable */ }
    if (pendingSave && pendingSave.timer === timer) pendingSave = null;
  }, 300);
  pendingSave = { key, snapshot, timer };
}

// initCollapsed loads the collapse map for a workspace. Keys are node ids, so
// they must be per workspace; the old global key is adopted once.
export function initCollapsed(workspaceID) {
  flushCollapsed();
  state.collapsed = {};
  if (!workspaceID) return;
  try {
    const key = collapsedKey(workspaceID);
    let saved = localStorage.getItem(key);
    if (!saved) {
      const legacy = localStorage.getItem(STORAGE_KEY.COLLAPSED);
      if (legacy) {
        saved = legacy;
        localStorage.setItem(key, legacy);
        localStorage.removeItem(STORAGE_KEY.COLLAPSED);
      }
    }
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

// pruneCollapsed drops keys that no longer correspond to a node (ids are
// regenerated on import, so orphans would otherwise accumulate).
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

// findWorkspaceName resolves a workspace id to its display name, or ''.
export function findWorkspaceName(workspaceID) {
  const found = (state.workspaces || []).find((item) => item.id === workspaceID);
  return found ? found.name : '';
}

// workspaceLandedIn returns the display name when an import result landed in a
// workspace other than the one it started in, or '' when it is still visible.
export function workspaceLandedIn(result, startedIn) {
  if (!result || !result.workspaceId || result.workspaceId === startedIn) return '';
  return findWorkspaceName(result.workspaceId) || '其他工作空间';
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
