import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const files = [
  'CONTRIBUTING.md',
  'SECURITY.md',
  'CHANGELOG.md',
  'THIRD_PARTY_NOTICES.md',
  'UPSTREAM_SNAPSHOT.md',
  'docs/release/0.9.0-rc-runbook.md',
];

test('release documentation package is present and separates public operations from private decisions', () => {
  for (const file of files) assert.equal(existsSync(join(root, file)), true, file);
  const runbook = readFileSync(join(root, 'docs/release/0.9.0-rc-runbook.md'), 'utf8');
  for (const token of ['备份', '迁移', '回滚', 'health/ready', 'SHA256SUMS', '远程对象存储']) assert.match(runbook, new RegExp(token));
  assert.doesNotMatch(runbook, /DEC-0\d+|Q\d+|内部决策登记/);
});

test('legal and contribution documents link to the tracked license and attribution files', () => {
  const legal = readFileSync(join(root, 'THIRD_PARTY_NOTICES.md'), 'utf8');
  assert.match(legal, /LICENSES\/Vue-Vben-Admin-MIT\.txt/);
  assert.match(legal, /e3369bd63523831abb24a604da7721ba4f8c8db6/);
  const contribution = readFileSync(join(root, 'CONTRIBUTING.md'), 'utf8');
  assert.match(contribution, /测试|test/i);
  assert.match(readFileSync(join(root, 'SECURITY.md'), 'utf8'), /漏洞|security/i);
});
