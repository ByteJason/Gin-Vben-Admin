#!/usr/bin/env node
import { access, copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

import { renderServerConfig } from './bootstrap-config.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = new Set(process.argv.slice(2));
const value = (name, fallback) => {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
};
const ui = value('--ui', 'antd');
const database = value('--database', 'mysql');
const checkOnly = args.has('--check');
const skipInstall = args.has('--skip-install') || checkOnly;

const required = [
  'admin',
  'server',
  'contracts/openapi/admin-v1.yaml',
  'admin/packages/api-client/package.json',
  'admin/packages/api-client/src/generated/admin-v1.ts',
  'contracts/openapi/client-v1.yaml',
  'server/configs/server.example.yaml',
  'server/cmd/migrate',
  'deploy/docker-compose.yml',
  'deploy/server.Dockerfile',
  'deploy/admin.Dockerfile',
  'scripts/prepare-runtime-compose.mjs',
];

async function exists(relative) {
  try {
    await access(path.join(root, relative), constants.F_OK);
    return true;
  } catch {
    return false;
  }
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

if (!['antd', 'ele', 'naive'].includes(ui)) {
  console.error(`unsupported --ui: ${ui}`);
  process.exit(2);
}
if (!['mysql', 'postgres'].includes(database)) {
  console.error(`unsupported --database: ${database}`);
  process.exit(2);
}

const missing = [];
for (const item of required) {
  if (!(await exists(item))) missing.push(item);
}
if (missing.length) {
  console.error(`BOOTSTRAP_CHECK_FAILED missing=${missing.join(',')}`);
  process.exit(1);
}

console.log(`BOOTSTRAP_ROOT=${root}`);
console.log(`BOOTSTRAP_UI=${ui}`);
console.log(`BOOTSTRAP_DATABASE=${database}`);
console.log(`BOOTSTRAP_PLATFORM=${process.platform}`);

if (checkOnly) {
  console.log('BOOTSTRAP_CHECK_OK');
  process.exit(0);
}

const localConfig = path.join(root, 'server/configs/server.yaml');
if (!(await exists('server/configs/server.yaml'))) {
  const template = await readFile(path.join(root, 'server/configs/server.example.yaml'), 'utf8');
  await writeFile(localConfig, renderServerConfig(template, database), { mode: 0o600 });
  console.log('CONFIG_CREATED=server/configs/server.yaml');
} else {
  console.log('CONFIG_PRESERVED=server/configs/server.yaml');
}

const uiDirectory = `admin/apps/web-${ui}`;
const uiEnvExample = path.join(root, uiDirectory, '.env.development.example');
const uiEnv = path.join(root, uiDirectory, '.env.development');
if (await exists(`${uiDirectory}/.env.development.example`) && !(await exists(`${uiDirectory}/.env.development`))) {
  await copyFile(uiEnvExample, uiEnv);
  console.log(`UI_ENV_CREATED=${uiDirectory}/.env.development`);
} else if (await exists(`${uiDirectory}/.env.development`)) {
  console.log(`UI_ENV_PRESERVED=${uiDirectory}/.env.development`);
}

if (!skipInstall) {
  await run('pnpm', ['--dir', 'admin', 'install', '--frozen-lockfile']);
  await run('go', ['-C', 'server', 'mod', 'download']);
}

console.log('BOOTSTRAP_OK');
