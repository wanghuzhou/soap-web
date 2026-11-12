// Browser download helpers used by the workspace JSON export.

// downloadText saves text as a file through a temporary object URL.
export function downloadText(filename, text, mime = 'text/plain') {
  const blob = new Blob([text], { type: mime + ';charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  // Revoking immediately can cancel the download in some browsers.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// fileSafeName keeps characters Windows/macOS accept in a filename.
export function fileSafeName(name, fallback = 'file') {
  return String(name || fallback).replace(/[\\/:*?"<>|]/g, '_').trim() || fallback;
}
