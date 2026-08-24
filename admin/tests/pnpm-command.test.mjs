import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import {
  buildDependencySupervisorCommand,
  parseWindowsJobGate,
  waitForWindowsJobGate,
} from '../scripts/dependency-launch.mjs';
import { buildPnpmCommand } from '../scripts/pnpm-command.mjs';

test('Windows pnpm execution prefers Node with a validated npm_execpath file', () => {
  const directory = mkdtempSync(join(tmpdir(), 'gin-vben-pnpm-command-'));
  try {
    const npmExecPath = join(directory, 'pnpm.cjs');
    writeFileSync(npmExecPath, '/* pnpm fixture */\n');

    assert.deepEqual(buildPnpmCommand(['install', '--frozen-lockfile'], {
      platform: 'win32',
      execPath: 'C:\\Program Files\\nodejs\\node.exe',
      env: { npm_execpath: npmExecPath },
    }), {
      command: 'C:\\Program Files\\nodejs\\node.exe',
      args: [realpathSync(npmExecPath), 'install', '--frozen-lockfile'],
    });
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test('dependency supervision can run the validated pnpm JavaScript CLI in-process on POSIX', () => {
  const directory = mkdtempSync(join(tmpdir(), 'gin-vben-pnpm-command-'));
  try {
    const npmExecPath = join(directory, 'pnpm.cjs');
    writeFileSync(npmExecPath, '/* pnpm fixture */\n');

    assert.deepEqual(buildPnpmCommand(['install', '--frozen-lockfile'], {
      platform: 'linux',
      execPath: '/usr/bin/node',
      env: { npm_execpath: npmExecPath },
    }), {
      command: '/usr/bin/node',
      args: [realpathSync(npmExecPath), 'install', '--frozen-lockfile'],
    });
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test('pnpm lifecycle symlinks resolve once to a canonical JavaScript CLI', () => {
  const directory = mkdtempSync(join(tmpdir(), 'gin-vben-pnpm-command-'));
  try {
    const npmExecPath = join(directory, 'pnpm');
    const canonical = join(directory, 'pnpm.mjs');
    writeFileSync(canonical, '/* pnpm fixture */\n');
    symlinkSync(canonical, npmExecPath);

    assert.deepEqual(buildPnpmCommand(['install', '--frozen-lockfile'], {
      platform: 'darwin',
      execPath: '/usr/bin/node',
      env: { npm_execpath: npmExecPath },
    }), {
      command: '/usr/bin/node',
      args: [realpathSync(canonical), 'install', '--frozen-lockfile'],
    });
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test('Windows pnpm execution rejects untrusted npm_execpath values and uses fixed cmd arguments', () => {
  const directory = mkdtempSync(join(tmpdir(), 'gin-vben-pnpm-command-'));
  try {
    const target = join(directory, 'real-pnpm.cjs');
    const symlink = join(directory, 'pnpm.cjs');
    const unrelated = join(directory, 'unrelated.cjs');
    writeFileSync(target, '/* pnpm fixture */\n');
    writeFileSync(unrelated, '/* unrelated fixture */\n');
    symlinkSync(target, symlink);

    for (const npmExecPath of [symlink, unrelated, 'relative/pnpm.cjs', join(directory, 'missing-pnpm.cjs')]) {
      assert.deepEqual(buildPnpmCommand(['-F', '@vben/web-antd', 'run', 'build'], {
        platform: 'win32',
        execPath: 'C:\\node.exe',
        env: { npm_execpath: npmExecPath },
      }), {
        command: 'cmd.exe',
        args: ['/d', '/s', '/c', 'pnpm', '-F', '@vben/web-antd', 'run', 'build'],
      });
    }
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test('explicit test injection remains argument-array based on every platform', () => {
  assert.deepEqual(buildPnpmCommand(['install', '--frozen-lockfile'], {
    platform: 'win32',
    execPath: 'C:\\node.exe',
    env: {
      INIT_PNPM_COMMAND: '/fixture/node',
      INIT_PNPM_PREFIX_ARGS: '["/fixture/fake-pnpm.mjs"]',
    },
  }), {
    command: '/fixture/node',
    args: ['/fixture/fake-pnpm.mjs', 'install', '--frozen-lockfile'],
  });
});

test('init install and selected UI dispatch both use the shared shell-free command builder', () => {
  for (const script of ['init.mjs', 'selected-dispatch.mjs']) {
    const source = readFileSync(join(import.meta.dirname, '..', 'scripts', script), 'utf8');
    assert.match(source, /import \{ buildPnpmCommand \} from '\.\/pnpm-command\.mjs';/);
    assert.match(source, /buildPnpmCommand\(/);
    assert.doesNotMatch(source, /pnpm\.cmd/);
  }
});

test('Windows dependency supervision is wrapped in a kill-on-close Job Object launcher', () => {
  assert.deepEqual(buildDependencySupervisorCommand({
    execPath: 'C:\\Program Files\\nodejs\\node.exe',
    platform: 'win32',
    scriptsDirectory: 'C:\\fixture\\admin\\scripts',
    stateRoot: 'C:\\fixture\\.runtime\\install',
    token: '12345678-1234-1234-1234-123456789abc',
  }), {
    command: 'powershell.exe',
    args: [
      '-NoLogo',
      '-NoProfile',
      '-NonInteractive',
      '-ExecutionPolicy',
      'Bypass',
      '-File',
      'C:\\fixture\\admin\\scripts\\dependency-supervisor-windows.ps1',
      'C:\\Program Files\\nodejs\\node.exe',
      'C:\\fixture\\admin\\scripts\\dependency-supervisor.mjs',
      'C:\\fixture\\.runtime\\install\\dependency-job-gate-12345678-1234-1234-1234-123456789abc.json',
      '12345678-1234-1234-1234-123456789abc',
    ],
  });

  assert.deepEqual(parseWindowsJobGate(
    '{"schema":1,"owner":"admin-dependency-job","token":"12345678-1234-1234-1234-123456789abc","guardianPid":4242}\n',
    '12345678-1234-1234-1234-123456789abc',
  ), { guardianPid: 4242 });
  for (const invalid of [
    '{"schema":1,"owner":"admin-dependency-job","token":"12345678-1234-1234-1234-123456789abc","guardianPid":4242,"extra":true}\n',
    '{"schema":1,"owner":"admin-dependency-job","token":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","guardianPid":4242}\n',
    '{"schema":1,"owner":"admin-dependency-job","token":"12345678-1234-1234-1234-123456789abc","guardianPid":0}\n',
  ]) assert.equal(parseWindowsJobGate(invalid, '12345678-1234-1234-1234-123456789abc'), null);

  const wrapper = readFileSync(join(import.meta.dirname, '..', 'scripts', 'dependency-supervisor-windows.ps1'), 'utf8');
  assert.match(wrapper, /JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE/);
  assert.match(wrapper, /AssignProcessToJobObject/);
  assert.match(wrapper, /guardianPid/);
  assert.match(wrapper, /FileMode\]::CreateNew/);
  assert.ok(wrapper.indexOf('AssignProcessToJobObject') < wrapper.indexOf('[System.IO.FileMode]::CreateNew'));
});

test('Windows Job gate reader retries a visible partial file until the exact payload is durable', async () => {
  const token = '12345678-1234-1234-1234-123456789abc';
  const complete = `{"schema":1,"owner":"admin-dependency-job","token":"${token}","guardianPid":4242}\n`;
  const snapshots = ['', '{"schema":1', complete];
  let index = 0;
  let clock = 0;
  const gate = await waitForWindowsJobGate('/fixture/gate.json', token, {
    delay: async () => { clock += 25; index += 1; },
    existsSync: () => true,
    lstatSync: () => ({ isFile: () => true, isSymbolicLink: () => false, size: snapshots[index].length }),
    now: () => clock,
    readFileSync: () => snapshots[index],
    timeoutMs: 100,
  });

  assert.deepEqual(gate, { guardianPid: 4242 });
  assert.equal(index, 2);
});
