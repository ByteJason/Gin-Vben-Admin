#!/usr/bin/env node
import { access, mkdir, readFile, readdir } from 'node:fs/promises';
import { constants } from 'node:fs';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const scopeIndex = process.argv.indexOf('--scope');
const scope = scopeIndex >= 0 ? process.argv[scopeIndex + 1] : 'basic';

async function exists(relative) {
  try {
    await access(path.join(root, relative), constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

async function text(relative) {
  return readFile(path.join(root, relative), 'utf8');
}

async function run(command, commandArgs, cwd = root) {
  const runtimeDir = path.join(root, '.runtime');
  await mkdir(path.join(runtimeDir, 'go-cache'), { recursive: true });
  await mkdir(path.join(runtimeDir, 'go-tmp'), { recursive: true });
  await new Promise((resolve, reject) => {
    const child = spawn(command, commandArgs, {
      cwd,
      stdio: 'inherit',
      shell: false,
      env: {
        ...process.env,
        GOCACHE: process.env.GOCACHE ?? path.join(runtimeDir, 'go-cache'),
        GOTMPDIR: process.env.GOTMPDIR ?? path.join(runtimeDir, 'go-tmp'),
      },
    });
    child.on('error', reject);
    child.on('exit', (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} exited ${code ?? signal}`));
    });
  });
}

const required = [
  'admin/apps/web-antd',
  'admin/apps/web-ele',
  'admin/apps/web-naive',
  'server/cmd/api',
  'server/internal/bootstrap',
  'server/internal/transport/http/admin',
  'server/internal/transport/http/client',
  'contracts/openapi/admin-v1.yaml',
  'contracts/openapi/client-v1.yaml',
  'deploy/compose.dev.yaml',
  'deploy/compose.dependencies.yaml',
  'admin/Dockerfile',
  'docs/README.md',
  'LICENSE',
  'LICENSES/Vue-Vben-Admin-MIT.txt',
  'NOTICE',
];
const missing = required.filter((item) => !exists(item));
const allowedRootDirectories = new Set([
  '.dev-docs',
  '.git',
  '.github',
  '.idea',
  '.pnpm-store',
  '.runtime',
  'LICENSES',
  'admin',
  'contracts',
  'deploy',
  'docs',
  'scripts',
  'server',
  'tests',
]);
const rootEntries = await readdir(root, { withFileTypes: true });
const unexpectedRootDirectories = rootEntries
  .filter((entry) => entry.isDirectory() && !allowedRootDirectories.has(entry.name))
  .map((entry) => entry.name);

const supportedApps = new Set(['web-antd', 'web-ele', 'web-naive']);
const appEntries = await readdir(path.join(root, 'admin/apps'), { withFileTypes: true });
const unexpectedApps = appEntries
  .filter((entry) => entry.isDirectory() && !supportedApps.has(entry.name))
  .map((entry) => entry.name);
if (unexpectedApps.length) {
  console.error(`VERIFY_FAILED unexpected_apps=${unexpectedApps.join(',')}`);
  process.exit(1);
}
if (missing.length || unexpectedRootDirectories.length) {
  console.error(`VERIFY_FAILED missing=${missing.join(',')} unexpected_root=${unexpectedRootDirectories.join(',')}`);
  process.exit(1);
}

const adminContract = await text('contracts/openapi/admin-v1.yaml');
const clientContract = await text('contracts/openapi/client-v1.yaml');
for (const token of ['/health/live', '/health/ready', '/api/admin/v1/ping', 'X-Request-ID']) {
  if (!adminContract.includes(token)) {
    console.error(`VERIFY_FAILED contract_token=${token}`);
    process.exit(1);
  }
}
if (clientContract.includes('/api/admin/v1')) {
  console.error('VERIFY_FAILED client_contract_cross_scope');
  process.exit(1);
}

if (scope !== 'basic' && scope !== 'template') {
  console.error(`unsupported --scope: ${scope}`);
  process.exit(2);
}

if (await exists('server/go.mod')) await run('go', ['test', './...'], path.join(root, 'server'));
console.log(`VERIFY_SCOPE=${scope}`);
console.log('VERIFY_OK');
