import { accessSync, constants as fsConstants, lstatSync, realpathSync } from 'node:fs';
import { basename, isAbsolute, win32 } from 'node:path';

function stringArray(value) {
  return Array.isArray(value) && value.every((entry) => typeof entry === 'string' && !entry.includes('\0'));
}

function prefixArgs(env) {
  if (!env.INIT_PNPM_PREFIX_ARGS) return [];
  let parsed;
  try {
    parsed = JSON.parse(env.INIT_PNPM_PREFIX_ARGS);
  } catch {
    throw new Error('PNPM_COMMAND_INVALID');
  }
  if (!stringArray(parsed)) throw new Error('PNPM_COMMAND_INVALID');
  return parsed;
}

function canonicalNpmExecPath(value) {
  if (typeof value !== 'string' || value.includes('\0') || (!isAbsolute(value) && !win32.isAbsolute(value))) return null;
  try {
    const canonical = realpathSync(value);
    const name = win32.isAbsolute(canonical) ? win32.basename(canonical) : basename(canonical);
    if (!/^pnpm\.(?:cjs|mjs|js)$/i.test(name)) return null;
    const stat = lstatSync(canonical);
    if (!stat.isFile() || stat.isSymbolicLink()) return null;
    accessSync(canonical, fsConstants.R_OK);
    return canonical;
  } catch {
    return null;
  }
}

export function buildPnpmCommand(pnpmArgs, options = {}) {
  if (!stringArray(pnpmArgs)) throw new Error('PNPM_COMMAND_INVALID');
  const env = options.env ?? process.env;
  const platform = options.platform ?? process.platform;
  const execPath = options.execPath ?? process.execPath;
  const injectedPrefix = prefixArgs(env);

  if (env.INIT_PNPM_COMMAND) {
    if (typeof env.INIT_PNPM_COMMAND !== 'string' || env.INIT_PNPM_COMMAND.includes('\0')) {
      throw new Error('PNPM_COMMAND_INVALID');
    }
    return { command: env.INIT_PNPM_COMMAND, args: [...injectedPrefix, ...pnpmArgs] };
  }
  if (injectedPrefix.length > 0) throw new Error('PNPM_COMMAND_INVALID');

  const npmExecPath = canonicalNpmExecPath(env.npm_execpath);
  if (npmExecPath) {
    return { command: execPath, args: [npmExecPath, ...pnpmArgs] };
  }
  if (platform === 'win32') {
    return { command: 'cmd.exe', args: ['/d', '/s', '/c', 'pnpm', ...pnpmArgs] };
  }
  return { command: 'pnpm', args: pnpmArgs };
}
