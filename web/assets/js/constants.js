// Shared localStorage keys. Values used by a single module stay local to it;
// this module exists so keys read by more than one module cannot drift apart.

export const STORAGE_KEY = {
  THEME: 'soap-web-theme',
  COLLAPSED: 'soap-web-collapsed',
  COLUMNS: 'soap-web-cols-v2',
  LEGACY_COLUMNS: 'soap-web-cols',
  AUTO_FORMAT: 'soap-web-autoformat',
};
