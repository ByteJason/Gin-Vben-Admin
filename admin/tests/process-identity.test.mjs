import assert from 'node:assert/strict';
import test from 'node:test';

import { processStartToken, validProcessStartToken } from '../scripts/process-identity.mjs';

test('Linux identity parses starttime after a parenthesized command and binds boot ID', () => {
  const reads = new Map([
    ['/proc/42/stat', '42 (command with a ) character) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 987654 20\n'],
    ['/proc/sys/kernel/random/boot_id', '12345678-1234-1234-1234-123456789abc\n'],
  ]);
  const first = processStartToken(42, {
    platform: 'linux',
    readFileSync: (path) => reads.get(path),
  });
  const second = processStartToken(42, {
    platform: 'linux',
    readFileSync: (path) => path.endsWith('/stat') ? reads.get(path).replace('987654', '987655') : reads.get(path),
  });
  assert.equal(validProcessStartToken(first), true);
  assert.notEqual(first, second);
});

test('Darwin identity uses untruncated locale-stable ps output', () => {
  let call;
  const token = processStartToken(42, {
    platform: 'darwin',
    spawnSync: (command, args, options) => {
      call = { command, args, options };
      return { status: 0, stdout: 'Sun Aug 24 10:00:00 2026 /usr/bin/node /fixture/dependency-supervisor.mjs\n' };
    },
  });
  assert.equal(validProcessStartToken(token), true);
  assert.equal(call.command, '/bin/ps');
  assert.deepEqual(call.args, ['-ww', '-p', '42', '-o', 'lstart=', '-o', 'command=']);
  assert.equal(call.options.env.LC_ALL, 'C');
  assert.equal(call.options.env.LANG, 'C');
  assert.equal(call.options.shell, false);
});

test('Windows identity uses process creation ticks without requiring an executable Path', () => {
  let call;
  const token = processStartToken(42, {
    platform: 'win32',
    spawnSync: (command, args, options) => {
      call = { command, args, options };
      return { status: 0, stdout: '133852608000000000' };
    },
  });
  assert.equal(validProcessStartToken(token), true);
  assert.equal(call.command, 'powershell.exe');
  assert.deepEqual(call.args.slice(0, 5), ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', call.args[4]]);
  assert.match(call.args[4], /Get-Process -Id 42/);
  assert.match(call.args[4], /StartTime\.ToUniversalTime\(\)\.Ticks/);
  assert.doesNotMatch(call.args[4], /\.Path/);
});

test('current process identity is stable and strict on supported platforms', () => {
  const first = processStartToken(process.pid);
  const second = processStartToken(process.pid);
  assert.equal(validProcessStartToken(first), true);
  assert.equal(second, first);
  assert.equal(processStartToken(0), null);
});
