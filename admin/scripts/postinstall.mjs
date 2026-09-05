#!/usr/bin/env node

import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { buildPnpmCommand } from './pnpm-command.mjs';
import { resolveWorkspaceProfile, rootFromScript } from './init-state.mjs';

const root = rootFromScript(import.meta.url);

/**
 * Run workspace stub builds only for the dependency closure that was
 * installed. `pnpm install --filter <ui>...` intentionally leaves unrelated
 * workspace projects without node_modules; running every stub from the root
 * postinstall therefore makes a valid selective install fail with missing
 * modules (for example @vben/node-utils).
 *
 * A full install has no local UI profile yet, so it keeps the historical
 * all-workspace behavior. The selected closure uses pnpm's dependency filter
 * and remains deterministic on every platform.
 */
export function buildPostinstallArgs({ mode, profile } = {}) {
  const args = ['-r', 'run', '--if-present', 'stub'];
  return String(mode || '')
    .trim()
    .toLowerCase() === 'workspace' && profile?.packageName
    ? ['--filter', `${profile.packageName}...`, ...args]
    : args;
}

function installArgs() {
  let profile;
  try {
    profile = resolveWorkspaceProfile(root).profile;
  } catch {
    // Profile validation is performed by the initializer. During a plain
    // bootstrap, retaining the unfiltered command gives pnpm a useful error.
  }
  return buildPostinstallArgs({
    mode: process.env.GIN_VBEN_UI_SELECTION_MODE,
    profile,
  });
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  let invocation;
  try {
    invocation = buildPnpmCommand(installArgs());
  } catch {
    process.exitCode = 1;
  }

  if (invocation) {
    const result = spawnSync(invocation.command, invocation.args, {
      cwd: root,
      env: process.env,
      shell: false,
      stdio: 'inherit',
    });
    process.exitCode = result.error ? 1 : (result.status ?? 1);
  }
}
