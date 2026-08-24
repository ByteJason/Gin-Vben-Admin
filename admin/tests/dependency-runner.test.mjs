import assert from 'node:assert/strict';
import {
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

import { buildPnpmCommand } from '../scripts/pnpm-command.mjs';

test('real pnpm Worker performs a frozen offline install including lifecycle scripts', () => {
  const root = mkdtempSync(join(tmpdir(), 'gin-vben-pnpm-worker-'));
  try {
    const invocation = buildPnpmCommand(['install', '--frozen-lockfile', '--offline'], {
      env: { npm_execpath: process.env.npm_execpath },
    });
    assert.equal(invocation.command, process.execPath, 'run this smoke through pnpm so npm_execpath identifies its pinned JS CLI');
    const [modulePath, ...args] = invocation.args;
    const lifecycle = join(root, 'lifecycle.mjs');
    writeFileSync(join(root, 'package.json'), `${JSON.stringify({
      name: 'dependency-runner-smoke',
      private: true,
      scripts: {
        postinstall: 'node lifecycle.mjs postinstall',
        prepare: 'node lifecycle.mjs prepare',
      },
    }, null, 2)}\n`);
    writeFileSync(join(root, 'pnpm-lock.yaml'), [
      "lockfileVersion: '9.0'",
      'settings:',
      '  autoInstallPeers: true',
      '  excludeLinksFromLockfile: false',
      'importers:',
      '  .: {}',
      '',
    ].join('\n'));
    writeFileSync(lifecycle, [
      "import { appendFileSync } from 'node:fs';",
      "appendFileSync('lifecycle.log', `${process.argv[2]}:${process.pid}:${process.ppid}\\n`);",
    ].join('\n'));
    const harness = join(root, 'worker-harness.mjs');
    writeFileSync(harness, [
      "import { Worker } from 'node:worker_threads';",
      'const worker = new Worker(process.argv[2], {',
      '  workerData: { modulePath: process.argv[3], args: process.argv.slice(4) },',
      '});',
      "worker.once('error', (error) => { console.error(error); process.exitCode = 1; });",
      "worker.once('exit', (status) => { process.exitCode = status; });",
    ].join('\n'));

    const result = spawnSync(process.execPath, [
      harness,
      join(import.meta.dirname, '..', 'scripts', 'dependency-runner.mjs'),
      modulePath,
      ...args,
    ], {
      cwd: root,
      encoding: 'utf8',
      env: { ...process.env, npm_config_offline: 'true' },
      timeout: 15_000,
    });

    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
    assert.match(result.stdout, /Done in .* using pnpm v11\./);
    const lifecycleEvents = readFileSync(join(root, 'lifecycle.log'), 'utf8').trim().split('\n');
    assert.equal(lifecycleEvents.length, 2);
    assert.match(lifecycleEvents[0], /^postinstall:[1-9][0-9]*:[1-9][0-9]*$/);
    assert.match(lifecycleEvents[1], /^prepare:[1-9][0-9]*:[1-9][0-9]*$/);
  } finally {
    rmSync(root, { force: true, recursive: true });
  }
});
