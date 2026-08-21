#!/usr/bin/env node

import { createHash } from 'node:crypto';
import { existsSync, readFileSync, statSync, rmSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = resolve(join(fileURLToPath(new URL('.', import.meta.url)), '..'));
const modes = ['api_only', 'embedded', 'standalone'];
const availableUis = ['antd', 'ele', 'naive'];

function fail(message, status = 2) {
  process.stderr.write(`RELEASE_SMOKE_ERROR=${message}\n`);
  process.exit(status);
}

function parseArgs(argv) {
  const options = { check: false, ui: availableUis };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--check') {
      options.check = true;
    } else if (arg === '--ui') {
      const value = argv[++i];
      if (!value) fail('missing value for --ui');
      options.ui = value.split(',').filter(Boolean);
    } else {
      fail(`unknown argument: ${arg}`);
    }
  }
  if (options.ui.length === 0 || options.ui.some((ui) => !availableUis.includes(ui))) {
    fail(`unsupported ui: ${options.ui.join(',')}`);
  }
  return options;
}

function runBuild(args) {
  const result = spawnSync(process.execPath, ['scripts/build.mjs', ...args], {
    cwd: root,
    encoding: 'utf8',
    env: process.env,
  });
  process.stdout.write(result.stdout || '');
  process.stderr.write(result.stderr || '');
  if (result.status !== 0) fail(`build failed: ${args.join(' ')}`, result.status ?? 1);
  return result.stdout || '';
}

function sha256(path) {
  return createHash('sha256').update(readFileSync(path)).digest('hex');
}

function verifyManifest(directory, expectedMode, expectedUi) {
  const manifestPath = join(directory, 'artifact-manifest.json');
  if (!existsSync(manifestPath)) return false;
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  const acceptedUis = Array.isArray(expectedUi) ? expectedUi : [expectedUi];
  if (manifest.schema !== 1 || manifest.mode !== expectedMode || !acceptedUis.includes(manifest.ui)) {
    fail(`invalid artifact manifest: ${relative(root, manifestPath)}`);
  }
  for (const [name, expected] of Object.entries(manifest.files ?? {})) {
    const path = join(directory, ...name.split('/'));
    if (!existsSync(path) || !statSync(path).isFile() || sha256(path) !== expected) {
      fail(`artifact sha256 mismatch: ${relative(root, path)}`);
    }
  }
  process.stdout.write(`MANIFEST_SHA256_OK=${expectedMode}/${expectedUi}\n`);
  return true;
}

function checkSourceContract(options) {
  const buildScript = readFileSync(join(root, 'scripts', 'build.mjs'), 'utf8');
  const healthContract = readFileSync(join(root, 'contracts', 'openapi', 'admin-v1.yaml'), 'utf8');
  for (const mode of modes) {
    const ui = mode === 'api_only' ? 'antd' : mode === 'embedded' ? 'all' : options.ui[0];
    const result = runBuild(['--mode', mode, '--ui', ui, '--check']);
    if (!result.includes('BUILD_CHECK_OK')) fail(`missing BUILD_CHECK_OK for ${mode}`);
    process.stdout.write(`MODE_CHECK_OK=${mode}\n`);
  }
  for (const ui of options.ui) {
    if (!existsSync(join(root, 'admin', 'apps', `web-${ui}`, 'package.json'))) fail(`build path contract missing: ${ui}`);
    process.stdout.write(`UI_CHECK=${ui}\n`);
  }
  for (const token of ['artifact-manifest.json', 'BUILD_SHA256', 'server/bin', 'health/live', 'health/ready']) {
    if (!buildScript.includes(token) && !healthContract.includes(token) && !readFileSync(join(root, 'server', 'internal', 'bootstrap', 'http.go'), 'utf8').includes(token)) {
      fail(`release contract missing: ${token}`);
    }
  }
  const embedded = join(root, '.runtime', 'build', 'embedded');
  const standalone = join(root, '.runtime', 'build', 'standalone');
  if (existsSync(embedded)) verifyManifest(embedded, 'embedded', 'all');
  if (existsSync(standalone)) verifyManifest(standalone, 'standalone', options.ui);
  process.stdout.write('MANIFEST_CONTRACT_OK\n');
}

function runIntegration(options) {
  const apiOutput = join('server', 'bin', `release-smoke-${process.pid}${process.platform === 'win32' ? '.exe' : ''}`);
  try {
    runBuild(['--mode', 'api_only', '--output', apiOutput]);
    for (const ui of options.ui) runBuild(['--mode', 'standalone', '--ui', ui]);
    runBuild(['--mode', 'embedded', '--ui', 'all']);
    verifyManifest(join(root, '.runtime', 'build', 'embedded'), 'embedded', 'all');
    verifyManifest(join(root, '.runtime', 'build', 'standalone'), 'standalone', options.ui.at(-1));
    process.stdout.write('BUILD_INTEGRATION_OK\n');
    if (process.env.RELEASE_SMOKE_HTTP === '1') {
      process.stdout.write('HTTP_SMOKE=requires-isolated-server\n');
    } else {
      process.stdout.write('HTTP_SMOKE=skipped\n');
    }
  } finally {
    rmSync(join(root, apiOutput), { force: true });
  }
}

const options = parseArgs(process.argv.slice(2));
process.stdout.write(`RELEASE_SMOKE_MODE=${options.check ? 'check' : 'integration'}\n`);
checkSourceContract(options);
if (process.env.BUILD_INTEGRATION === '1' && !options.check) runIntegration(options);
else process.stdout.write('HTTP_SMOKE=skipped\n');
process.stdout.write('RELEASE_SMOKE_OK\n');
