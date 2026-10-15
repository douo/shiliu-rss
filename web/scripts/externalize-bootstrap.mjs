import { parse, serialize } from 'parse5';
import { createHash } from 'node:crypto';
import { readFile, writeFile, readdir, mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../build/', import.meta.url));
async function files(directory) {
  return (
    await Promise.all(
      (await readdir(directory, { withFileTypes: true })).map((entry) =>
        entry.isDirectory() ? files(join(directory, entry.name)) : join(directory, entry.name)
      )
    )
  ).flat();
}
let count = 0;
for (const file of (await files(root)).filter((file) => file.endsWith('.html'))) {
  const document = parse(await readFile(file, 'utf8'));
  async function visit(node) {
    if (node.tagName === 'script' && !node.attrs.some((attr) => attr.name === 'src')) {
      const type = node.attrs.find((attr) => attr.name === 'type')?.value || '';
      if (['', 'module', 'text/javascript', 'application/javascript'].includes(type)) {
        const code = node.childNodes.map((child) => child.value || '').join('');
        if (code.trim()) {
          const hash = createHash('sha256').update(code).digest('hex');
          const relative = `_app/immutable/bootstrap/${hash}.js`;
          await mkdir(join(root, '_app/immutable/bootstrap'), { recursive: true });
          await writeFile(join(root, relative), code);
          // Preserve type, defer, nonce and any future generated attributes.
          node.attrs.push({ name: 'src', value: `/${relative}` });
          node.childNodes = [];
          count++;
        }
      }
    }
    for (const child of node.childNodes || []) await visit(child);
  }
  await visit(document);
  await writeFile(file, serialize(document));
}
if (!count)
  throw new Error('No generated bootstrap was externalized; inspect the SvelteKit output.');
console.log(
  `Externalized ${count} generated bootstrap script(s); no unsafe-inline script CSP needed.`
);
