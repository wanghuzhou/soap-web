// Application state plus pure lookup helpers for the project tree.

export const state = {
  projects: [],
  selected: null, // { kind, node, ids: { pid, sid, oid, cid }, parentOp }
  response: null,
  view: 'pretty',
  theme: 'dark',
  collapsed: {},
};

const COLLAPSED_KEY = 'soap-web-collapsed';

export function saveCollapsed() {
  try { localStorage.setItem(COLLAPSED_KEY, JSON.stringify(state.collapsed)); } catch (e) { /* ignore */ }
}

export function initCollapsed() {
  try {
    const saved = localStorage.getItem(COLLAPSED_KEY);
    if (saved) state.collapsed = JSON.parse(saved) || {};
  } catch (e) {
    state.collapsed = {};
  }
}

export function toggleCollapsed(key) {
  state.collapsed[key] = !state.collapsed[key];
  saveCollapsed();
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
