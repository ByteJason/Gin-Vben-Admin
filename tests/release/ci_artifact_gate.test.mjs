import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

test('CI builds local RC archives and uploads checksums without publishing', () => {
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
  assert.match(workflow, /artifact-gates:/);
  assert.match(workflow, /scripts\/release\/package\.mjs --build/);
  assert.match(workflow, /SHA256SUMS/);
  assert.match(workflow, /actions\/upload-artifact@v4/);
  assert.doesNotMatch(workflow, /docker push|cosign sign|npm publish|pnpm publish/);
});
