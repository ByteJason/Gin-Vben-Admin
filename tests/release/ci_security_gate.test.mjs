import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

test('CI exposes the local-only security policy gate without remote scanning', () => {
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
  assert.match(workflow, /security-gates:/);
  assert.match(workflow, /scripts\/security-gates\.mjs/);
  assert.match(workflow, /tests\/security\/security_gates\.test\.mjs/);
  assert.match(workflow, /actions\/upload-artifact@v4/);
  assert.doesNotMatch(workflow, /zap.*https?:\/\/(?!127\.0\.0\.1|localhost)/i);
});
