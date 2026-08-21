#!/usr/bin/env node

import { accessSync, constants } from 'node:fs';
import { delimiter, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const MODES = new Set(['embedded', 'standalone', 'api_only', 'dev']);
const UIS = new Set(['antd', 'ele', 'naive', 'all']);
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

function fail(message, exitCode = 2) {
  process.stderr.write(`BUILD_ERROR=${message}\n`);
  process.exit(exitCode);
}

function parseArgs(argv) {
  const options = { check: false, mode: '', ui: '' };

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--check') {
      options.check = true;
      continue;
    }
    if (argument === '--mode' || argument === '--ui') {
      const value = argv[index + 1];
      if (!value || value.startsWith('--')) {
        fail(`missing value for ${argument}`);
      }
      options[argument.slice(2)] = value;
      index += 1;
      continue;
    }
    fail(`unknown argument: ${argument}`);
  }

  if (!MODES.has(options.mode)) {
    fail(`unsupported mode: ${options.mode || '<empty>'}`);
  }
  if (options.mode === 'api_only') {
    if (options.ui && !UIS.has(options.ui)) {
      fail(`unsupported ui: ${options.ui}`);
    }
    options.ui = 'none';
  } else if (!UIS.has(options.ui)) {
    fail(`unsupported ui: ${options.ui || '<empty>'}`);
  }
  if (options.ui === 'all' && options.mode !== 'embedded') {
    fail('ui all is only valid for embedded mode');
  }
  if (!options.check) {
    fail('this build entry currently requires --check');
  }

  return options;
}

function requirePath(relativePath) {
  const absolutePath = join(root, relativePath);
  try {
    accessSync(absolutePath, constants.R_OK);
  } catch {
    fail(`required path is not readable: ${relativePath}`, 1);
  }
}

function commandExists(command) {
  const pathValue = process.env.PATH ?? '';
  const extensions = process.platform === 'win32'
    ? (process.env.PATHEXT ?? '.EXE;.CMD;.BAT;.COM').split(';')
    : [''];

  for (const directory of pathValue.split(delimiter).filter(Boolean)) {
    for (const extension of extensions) {
      try {
        accessSync(join(directory, `${command}${extension}`), constants.X_OK);
        return true;
      } catch {
        // Continue through the bounded PATH candidates.
      }
    }
  }
  return false;
}

function validate(options) {
  requirePath('server/go.mod');
  if (!commandExists('go')) {
    fail('required command is not executable: go', 1);
  }

  if (options.mode !== 'api_only') {
    requirePath('install/package.json');
    if (!commandExists('pnpm')) {
      fail('required command is not executable: pnpm', 1);
    }

    const selectedUis = options.ui === 'all'
      ? ['antd', 'ele', 'naive']
      : [options.ui];
    for (const ui of selectedUis) {
      requirePath(`admin/apps/web-${ui}/package.json`);
    }
  }
}

const options = parseArgs(process.argv.slice(2));
validate(options);

process.stdout.write(`BUILD_MODE=${options.mode}\n`);
process.stdout.write(`BUILD_UI=${options.ui}\n`);
process.stdout.write(`BUILD_PLATFORM=${process.platform}\n`);
process.stdout.write('BUILD_CHECK_OK\n');
