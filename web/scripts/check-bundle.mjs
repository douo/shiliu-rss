import { readdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
async function walk(directory) {
  const paths = await readdir(directory, { withFileTypes: true });
  return (
    await Promise.all(
      paths.map((entry) =>
        entry.isDirectory() ? walk(join(directory, entry.name)) : join(directory, entry.name)
      )
    )
  ).flat();
}
const files = (await walk(fileURLToPath(new URL('../build', import.meta.url)))).filter((path) =>
  /\.(css|js)$/.test(path)
);
let total = 0;
for (const file of files) total += gzipSync(await readFile(file)).length;
console.log(
  `All generated JS + CSS, including lazy settings: ${(total / 1024).toFixed(1)} KiB gzip (${files.length} files). Initial-load budget: 150 KiB.`
);
if (total > 150 * 1024) throw new Error('Total frontend assets exceed the initial-load budget.');
