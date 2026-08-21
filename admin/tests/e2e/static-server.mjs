#!/usr/bin/env node
import { createReadStream } from 'node:fs';
import { stat } from 'node:fs/promises';
import { createServer } from 'node:http';
import { extname, relative, resolve, sep } from 'node:path';
import process from 'node:process';

const options = new Map();
for (let index = 2; index < process.argv.length; index += 2) {
  options.set(process.argv[index], process.argv[index + 1]);
}

const root = resolve(options.get('--root') ?? '');
const port = Number(options.get('--port'));
const mount = normalizeMount(options.get('--mount') ?? '/');
if (!Number.isInteger(port) || port < 1 || port > 65_535) {
  throw new Error('a valid --port is required');
}

const types = new Map([
  ['.css', 'text/css; charset=utf-8'],
  ['.html', 'text/html; charset=utf-8'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.svg', 'image/svg+xml'],
  ['.woff2', 'font/woff2'],
]);

const server = createServer(async (request, response) => {
  if (request.method !== 'GET' && request.method !== 'HEAD') {
    response.writeHead(405).end();
    return;
  }
  let pathname;
  try {
    pathname = decodeURIComponent(
      new URL(request.url ?? '/', `http://${request.headers.host}`).pathname,
    );
  } catch {
    response.writeHead(400).end();
    return;
  }
  const relativePath = mountedPath(pathname, mount);
  if (relativePath === null) {
    response.writeHead(404).end();
    return;
  }
  let candidate = safePath(root, relativePath || 'index.html');
  if (!candidate) {
    response.writeHead(404).end();
    return;
  }
  let info = await regularFile(candidate);
  if (!info && extname(relativePath) === '') {
    candidate = safePath(root, 'index.html');
    info = candidate ? await regularFile(candidate) : null;
  }
  if (!candidate || !info) {
    response.writeHead(404).end();
    return;
  }
  response.writeHead(200, {
    'Cache-Control': 'no-store',
    'Content-Length': info.size,
    'Content-Type': types.get(extname(candidate)) ?? 'application/octet-stream',
    'X-Content-Type-Options': 'nosniff',
  });
  if (request.method === 'HEAD') response.end();
  else createReadStream(candidate).pipe(response);
});

server.listen(port, '127.0.0.1', () => {
  console.log(`STATIC_E2E_READY=http://127.0.0.1:${port}${mount}`);
});
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)));
}

function normalizeMount(value) {
  const normalized = `/${String(value).replace(/^\/+|\/+$/g, '')}`;
  return normalized === '/' ? '/' : normalized;
}

function mountedPath(pathname, prefix) {
  if (prefix === '/') return pathname.replace(/^\/+/, '');
  if (pathname === prefix || pathname === `${prefix}/`) return '';
  if (!pathname.startsWith(`${prefix}/`)) return null;
  return pathname.slice(prefix.length + 1);
}

function safePath(base, value) {
  const candidate = resolve(base, value);
  const pathFromRoot = relative(base, candidate);
  if (pathFromRoot === '..' || pathFromRoot.startsWith(`..${sep}`)) return null;
  return candidate;
}

async function regularFile(path) {
  try {
    const info = await stat(path);
    return info.isFile() ? info : null;
  } catch {
    return null;
  }
}
