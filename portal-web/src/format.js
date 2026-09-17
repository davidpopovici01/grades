export function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// VIEWABLE_EXTENSIONS can be reviewed inline in the browser; everything else
// (pdf, docx, xlsx, images, video…) stays download-only.
const VIEWABLE_EXTENSIONS = new Set([
  '.java', '.py', '.txt', '.md', '.csv', '.json', '.xml', '.html', '.css',
  '.js', '.ts', '.c', '.h', '.cpp', '.hpp', '.cs', '.go', '.rs', '.sql',
  '.yaml', '.yml', '.toml', '.ini', '.sh', '.log', '.tex',
]);

export function isViewable(name) {
  const dot = name.lastIndexOf('.');
  return dot >= 0 && VIEWABLE_EXTENSIONS.has(name.slice(dot).toLowerCase());
}
