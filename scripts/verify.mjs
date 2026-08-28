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
  'server/cmd/api',
  'server/cmd/migrate',
  'server/internal/bootstrap',
  'server/internal/transport/http/admin',
  'server/internal/transport/http/client',
  'contracts/openapi/admin-v1.yaml',
  'contracts/openapi/client-v1.yaml',
  'contracts/openapi/install-v1.yaml',
  'admin/packages/api-client/package.json',
  'admin/packages/api-client/src/generated/admin-v1.ts',
  'admin/apps/install/src/index.html',
  'admin/apps/install/src/app.js',
  'admin/apps/install/src/styles.css',
  'admin/apps/install/package.json',
  'admin/pnpm-lock.yaml',
  'deploy/docker-compose.yml',
  'deploy/server.Dockerfile',
  'deploy/admin.Dockerfile',
  'scripts/prepare-runtime-compose.mjs',
  'docs/README.md',
  'LICENSE',
  'LICENSES/Vue-Vben-Admin-MIT.txt',
  'NOTICE',
];
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

const localProfilePath = 'admin/.ui-profile.local.json';
const trackedProfilePath = 'admin/.ui-profile.json';
const workspaceMode = await exists('admin/pnpm-workspace.yaml') || await exists(localProfilePath);
const profilePath = await exists(localProfilePath)
  ? localProfilePath
  : await exists(trackedProfilePath)
    ? trackedProfilePath
    : '';
let profile = null;
if (profilePath) {
  try {
    profile = JSON.parse(await text(profilePath));
  } catch {
    console.error('VERIFY_FAILED ui_profile=invalid');
    process.exit(1);
  }
  if (
    !profile
    || !['antd', 'ele', 'naive'].includes(profile.selectedUi)
    || profile.appDirectory !== `apps/web-${profile.selectedUi}`
    || profile.packageName !== `@vben/web-${profile.selectedUi}`
  ) {
    console.error('VERIFY_FAILED ui_profile=invalid');
    process.exit(1);
  }
}
// In the non-destructive workspace model a profile controls dispatch, not
// source presence. Verify all three tracked templates so a pull cannot hide a
// missing adapter. The single-template expectation remains for old checkouts
// that have no workspace manifest and only a legacy tracked profile.
const expectedManagementApps = workspaceMode
  ? ['web-antd', 'web-ele', 'web-naive']
  : profile
    ? [`web-${profile.selectedUi}`]
    : ['web-antd', 'web-ele', 'web-naive'];
for (const app of expectedManagementApps) required.push(`admin/apps/${app}`);
const existence = await Promise.all(required.map(async (item) => [item, await exists(item)]));
const missing = existence.filter(([, present]) => !present).map(([item]) => item);

const supportedApps = new Set(['install', ...expectedManagementApps]);
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

// Generate disposable development/acceptance topology files outside deploy/.
// The production entrypoint remains the only checked-in Compose file.
await run(process.execPath, ['scripts/prepare-runtime-compose.mjs']);
const runtimeCompose = ['dev.yaml', 'postgres.yaml', 'read-write.yaml', 'ha.yaml', 'observability.yaml'];
const missingRuntime = [];
for (const name of runtimeCompose) {
  if (!(await exists(`.runtime/compose/${name}`))) missingRuntime.push(name);
}
if (missingRuntime.length) {
  console.error(`VERIFY_FAILED missing_runtime_compose=${missingRuntime.join(',')}`);
  process.exit(1);
}

const adminContract = await text('contracts/openapi/admin-v1.yaml');
const clientContract = await text('contracts/openapi/client-v1.yaml');
const installContract = await text('contracts/openapi/install-v1.yaml');
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
if (!installContract.includes('/api/system/install/v1/status') || installContract.includes('/api/admin/v1')) {
  console.error('VERIFY_FAILED install_contract_scope');
  process.exit(1);
}

if (scope !== 'basic' && scope !== 'template') {
  console.error(`unsupported --scope: ${scope}`);
  process.exit(2);
}

await run(process.execPath, ['scripts/generate-openapi.mjs', '--check']);
if (await exists('server/go.mod')) await run('go', ['test', './...'], path.join(root, 'server'));
console.log(`VERIFY_SCOPE=${scope}`);
console.log('VERIFY_OK');
