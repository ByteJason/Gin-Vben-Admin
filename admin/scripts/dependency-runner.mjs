import { lstatSync } from 'node:fs';
import { isAbsolute } from 'node:path';
import { pathToFileURL } from 'node:url';
import { workerData } from 'node:worker_threads';

const { args, modulePath } = workerData ?? {};
if (
  typeof modulePath !== 'string'
  || modulePath.includes('\0')
  || !isAbsolute(modulePath)
  || !Array.isArray(args)
  || args.some((argument) => typeof argument !== 'string' || argument.includes('\0'))
) process.exit(2);

try {
  const stat = lstatSync(modulePath);
  if (!stat.isFile() || stat.isSymbolicLink() || !/\.(?:cjs|mjs|js)$/i.test(modulePath)) process.exit(2);
} catch {
  process.exit(2);
}

process.argv = [process.execPath, modulePath, ...args];
await import(pathToFileURL(modulePath).href);
