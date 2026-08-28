#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { cpSync, lstatSync, readFileSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const PROFILES = Object.freeze({
  antd: { selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd' },
  ele: { selectedUi: 'ele', packageName: '@vben/web-ele', appDirectory: 'apps/web-ele' },
  naive: { selectedUi: 'naive', packageName: '@vben/web-naive', appDirectory: 'apps/web-naive' },
});

function readProfile(root) {
  // Container builds are explicit and reproducible. A per-clone selector is
  // never an input; only the tracked legacy profile may constrain ADMIN_UI.
  const path = join(root, '.ui-profile.json');
  try {
    const info = lstatSync(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size > 8192) throw new Error('UI_PROFILE_INVALID');
    const value = JSON.parse(readFileSync(path, 'utf8'));
    const expected = PROFILES[value?.selectedUi];
    const keys = value && typeof value === 'object' && !Array.isArray(value) ? Object.keys(value).sort().join(',') : '';
    if (!expected || keys !== 'appDirectory,packageName,schema,selectedUi' || value.schema !== 1 || value.packageName !== expected.packageName || value.appDirectory !== expected.appDirectory) {
      throw new Error('UI_PROFILE_INVALID');
    }
    return expected;
  } catch (error) {
    if (error && typeof error === 'object' && error.code === 'ENOENT') return null;
    if (error instanceof Error && error.message.startsWith('UI_PROFILE_')) throw error;
    throw new Error('UI_PROFILE_INVALID');
  }
}

export function resolveDockerSelection(root, explicitUi) {
  const explicit = String(explicitUi || process.env.ADMIN_UI || process.env.APP_UI || '').trim();
  if (explicit && !PROFILES[explicit]) throw new Error('UI_INVALID');
  const profile = readProfile(root);
  if (profile && explicit && profile.selectedUi !== explicit) throw new Error('UI_PROFILE_MISMATCH');
  const selected = profile || PROFILES[explicit];
  if (!selected) throw new Error('UI_PROFILE_REQUIRED');

  const packagePath = join(root, selected.appDirectory, 'package.json');
  try {
    const packageJSON = JSON.parse(readFileSync(packagePath, 'utf8'));
    if (packageJSON.name !== selected.packageName) throw new Error('UI_PACKAGE_MISMATCH');
  } catch (error) {
    if (error instanceof Error && error.message === 'UI_PACKAGE_MISMATCH') throw error;
    throw new Error('UI_PACKAGE_MISMATCH');
  }
  return { ...selected };
}

function option(args, name) {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] || '' : '';
}

function main() {
  const args = process.argv.slice(2);
  const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
  const output = option(args, '--out');
  const selection = resolveDockerSelection(root, option(args, '--ui'));
  if (args.includes('--print-package')) {
    process.stdout.write(`${selection.packageName}\n`);
    return;
  }
  if (!output) throw new Error('OUTPUT_REQUIRED');

  const pnpm = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm';
  const build = spawnSync(pnpm, ['-F', selection.packageName, 'run', 'build'], {
    cwd: root,
    env: process.env,
    shell: false,
    stdio: 'inherit',
  });
  if (build.error || build.status !== 0) throw new Error('UI_BUILD_FAILED');
  rmSync(output, { force: true, recursive: true });
  cpSync(join(root, selection.appDirectory, 'dist'), output, { recursive: true });
  process.stdout.write(`DOCKER_UI=${selection.selectedUi}\nDOCKER_UI_OUTPUT=${output}\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    const code = error instanceof Error ? error.message : 'UI_BUILD_FAILED';
    process.stderr.write(`${code}\n`);
    process.exit(code === 'UI_PROFILE_MISMATCH' ? 3 : code.endsWith('_REQUIRED') || code === 'UI_INVALID' ? 2 : 1);
  }
}
