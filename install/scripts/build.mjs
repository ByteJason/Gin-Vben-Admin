#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const source = join(root, 'src');
const output = join(root, 'dist');
const staging = join(root, `.dist-staging-${process.pid}`);
const files = ['index.html', 'app.js', 'styles.css'];

await rm(staging, { force: true, recursive: true });
await mkdir(staging, { recursive: true });

try {
  const manifest = { version: 1, files: {} };
  for (const relative of files) {
    const contents = await readFile(join(source, relative));
    await writeFile(join(staging, relative), contents);
    manifest.files[relative] = createHash('sha256').update(contents).digest('hex');
  }
  await writeFile(
    join(staging, 'asset-manifest.json'),
    `${JSON.stringify(manifest, null, 2)}\n`,
    { mode: 0o644 },
  );
  await rm(output, { force: true, recursive: true });
  await rename(staging, output);
  console.log(`INSTALL_BUILD_OK files=${files.length} output=${output}`);
} catch (error) {
  await rm(staging, { force: true, recursive: true });
  console.error('INSTALL_BUILD_FAILED');
  throw error;
}
