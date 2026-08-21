#!/usr/bin/env node

import { createHash } from 'node:crypto';
import {
  accessSync,
  constants,
  mkdirSync,
  readFileSync,
} from 'node:fs';
import {
  delimiter,
  dirname,
  isAbsolute,
  join,
  relative,
  resolve,
} from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const MODES = new Set(['embedded', 'standalone', 'api_only', 'dev']);
const UIS = new Set(['antd', 'ele', 'naive', 'all']);
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

function fail(message, exitCode = 2) {
  process.stderr.write(`BUILD_ERROR=${message}\n`);
  process.exit(exitCode);
}

function parseArgs(argv) {
  const options = { check: false, mode: '', output: '', ui: '' };

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--check') {
      options.check = true;
      continue;
    }
    if (argument === '--mode' || argument === '--output' || argument === '--ui') {
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
  if (!options.check && options.mode !== 'api_only') {
    fail(`build execution is not available yet for mode: ${options.mode}`);
  }

  return options;
}

function boundedApiOutput(value) {
  const outputRoot = join(root, 'server', 'bin');
  const defaultName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';
  const output = resolve(root, value || join('server', 'bin', defaultName));
  const child = relative(outputRoot, output);
  if (!child || child.startsWith('..') || isAbsolute(child)) {
    fail('output must be a file below server/bin');
  }
  return output;
}

function runApiOnlyBuild(outputValue) {
  const output = boundedApiOutput(outputValue);
  const runtimeRoot = join(root, '.runtime');
  const goCache = join(runtimeRoot, 'go-cache');
  const goTmp = join(runtimeRoot, 'go-tmp');
  mkdirSync(dirname(output), { recursive: true });
  mkdirSync(goCache, { recursive: true });
  mkdirSync(goTmp, { recursive: true });

  const outputFromServer = relative(join(root, 'server'), output);
  const result = spawnSync(
    'go',
    ['-C', 'server', 'build', '-trimpath', '-o', outputFromServer, './cmd/api'],
    {
      cwd: root,
      encoding: 'utf8',
      env: {
        ...process.env,
        GOCACHE: process.env.GOCACHE || goCache,
        GOTMPDIR: process.env.GOTMPDIR || goTmp,
      },
      shell: false,
    },
  );
  if (result.status !== 0) {
    process.stderr.write(result.stdout || '');
    process.stderr.write(result.stderr || '');
    fail(`go build failed with status ${result.status ?? 'unknown'}`, 1);
  }

  const digest = createHash('sha256').update(readFileSync(output)).digest('hex');
  process.stdout.write(`BUILD_ARTIFACT=${relative(root, output)}\n`);
  process.stdout.write(`BUILD_SHA256=${digest}\n`);
  process.stdout.write('BUILD_OK\n');
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
if (options.check) {
  process.stdout.write('BUILD_CHECK_OK\n');
} else {
  runApiOnlyBuild(options.output);
}
