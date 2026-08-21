import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

test('CI exposes the reproducible RC supply-chain and release smoke gate', () => {
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
  assert.match(workflow, /release-gates:/);
  assert.match(workflow, /scripts\/release\/sbom\.mjs/);
  assert.match(workflow, /scripts\/release\/license-policy\.mjs/);
  assert.match(workflow, /scripts\/release-smoke\.mjs --check/);
  assert.match(workflow, /actions\/upload-artifact@v4/);
  assert.doesNotMatch(workflow, /docker push|npm publish|pnpm publish/);
});
