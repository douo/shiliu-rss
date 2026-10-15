import { readdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
async function walk(directory) {
  return (
    await Promise.all(
      (await readdir(directory, { withFileTypes: true })).map((entry) =>
        entry.isDirectory() ? walk(join(directory, entry.name)) : join(directory, entry.name)
      )
    )
  ).flat();
}
const files = (await walk(fileURLToPath(new URL('../build/', import.meta.url)))).filter((file) =>
  /\.(html|js|css)$/.test(file)
);
for (const file of files)
  await writeFile(`${file}.gz`, gzipSync(await readFile(file), { level: 9 }));
console.log(`Precompressed ${files.length} static HTML/JS/CSS assets. API data is never included.`);
