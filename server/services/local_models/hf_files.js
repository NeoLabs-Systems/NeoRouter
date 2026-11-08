'use strict';

// The exact files of a catalog model on Hugging Face: path, size and, for
// large files, the SHA-256 the download is checked against.
const HF = 'https://huggingface.co';
const TIMEOUT_MS = 30_000;

// "onnx/*" or "*.json": * stays within one folder.
function globToRegex(glob) {
  const escaped = glob.split('*').map((part) => part.replace(/[.+?^${}()|[\]\\]/g, '\\$&')).join('[^/]*');
  return new RegExp(`^${escaped}$`);
}

async function tree(repo, signal) {
  const response = await fetch(`${HF}/api/models/${repo}/tree/main?recursive=1`, {
    headers: { 'User-Agent': 'NeoRouter' },
    signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(TIMEOUT_MS)]) : AbortSignal.timeout(TIMEOUT_MS),
  });
  if (!response.ok) throw new Error(`Hugging Face answered ${response.status} for ${repo}.`);
  const items = await response.json();
  if (!Array.isArray(items)) throw new Error(`Unexpected file list for ${repo}.`);
  return items.filter((item) => item.type === 'file');
}

/** [{ path, size, sha256, url }] for the entry's files; throws when one is missing. */
async function resolve(entry, signal) {
  const files = await tree(entry.source.repo, signal);
  const wanted = [...entry.source.include, ...(entry.source.projector ? [entry.source.projector] : [])];
  const chosen = new Map();
  for (const pattern of wanted) {
    const regex = globToRegex(pattern);
    const matches = files.filter((file) => regex.test(file.path));
    if (!matches.length) throw new Error(`${entry.source.repo} has no file matching ${pattern}.`);
    for (const file of matches) chosen.set(file.path, file);
  }
  return [...chosen.values()].map((file) => ({
    path: file.path,
    size: file.lfs?.size ?? file.size ?? null,
    sha256: file.lfs?.oid || null,
    url: `${HF}/${entry.source.repo}/resolve/main/${file.path.split('/').map(encodeURIComponent).join('/')}`,
  }));
}

module.exports = { resolve, globToRegex };
