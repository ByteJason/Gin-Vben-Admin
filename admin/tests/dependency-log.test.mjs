import assert from 'node:assert/strict';
import {
  closeSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readFileSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
  linkSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import { openDependencyLog } from '../scripts/dependency-log.mjs';

test('dependency log rejects dangling symlinks without creating the external target', () => {
  const root = mkdtempSync(join(tmpdir(), 'gin-vben-dependency-log-'));
  try {
    const log = join(root, 'dependency-install.log');
    const external = join(root, 'external.log');
    symlinkSync(external, log);

    assert.throws(() => openDependencyLog(log), /DEPENDENCY_INSTALL_FAILED/);
    assert.equal(lstatSync(log).isSymbolicLink(), true);
    assert.throws(() => lstatSync(external), { code: 'ENOENT' });
  } finally {
    rmSync(root, { force: true, recursive: true });
  }
});

test('dependency log rejects an existing hardlink without appending to external bytes', () => {
  const root = mkdtempSync(join(tmpdir(), 'gin-vben-dependency-log-'));
  try {
    const log = join(root, 'dependency-install.log');
    const external = join(root, 'external.log');
    writeFileSync(external, 'important\n');
    linkSync(external, log);

    assert.throws(() => openDependencyLog(log), /DEPENDENCY_INSTALL_FAILED/);
    assert.equal(readFileSync(external, 'utf8'), 'important\n');
    assert.equal(readFileSync(log, 'utf8'), 'important\n');
  } finally {
    rmSync(root, { force: true, recursive: true });
  }
});

test('dependency log rejects an existing-path replacement before exposing its descriptor to pnpm', () => {
  const root = mkdtempSync(join(tmpdir(), 'gin-vben-dependency-log-'));
  try {
    const log = join(root, 'dependency-install.log');
    const external = join(root, 'external.log');
    writeFileSync(log, 'owned\n');
    writeFileSync(external, 'important\n');
    let replaced = false;

    assert.throws(() => openDependencyLog(log, {
      open(path, flags, mode) {
        if (!replaced) {
          replaced = true;
          renameSync(path, `${path}.original`);
          symlinkSync(external, path);
        }
        return openSync(path, flags, mode);
      },
    }), /DEPENDENCY_INSTALL_FAILED/);

    assert.equal(readFileSync(external, 'utf8'), 'important\n');
    assert.equal(readFileSync(`${log}.original`, 'utf8'), 'owned\n');
    assert.equal(lstatSync(log).isSymbolicLink(), true);
  } finally {
    rmSync(root, { force: true, recursive: true });
  }
});

test('dependency log exclusively creates a private regular file and returns an append descriptor', () => {
  const root = mkdtempSync(join(tmpdir(), 'gin-vben-dependency-log-'));
  try {
    const log = join(root, 'dependency-install.log');
    const descriptor = openDependencyLog(log);
    writeFileSync(descriptor, 'line\n');
    closeSync(descriptor);
    assert.equal(readFileSync(log, 'utf8'), 'line\n');
    const stat = lstatSync(log);
    assert.equal(stat.isFile(), true);
    assert.equal(stat.isSymbolicLink(), false);
    assert.equal(Number(stat.nlink), 1);
  } finally {
    rmSync(root, { force: true, recursive: true });
  }
});
