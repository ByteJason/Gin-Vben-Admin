import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

test('0.10 authentication contract exposes username/email identifiers and profile fields', () => {
  const openapi = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const generated = readFileSync(join(root, 'admin/packages/api-client/src/generated/admin-v1.ts'), 'utf8');

  assert.match(openapi, /identifierType/);
  assert.match(openapi, /enum: \[username, email\]/);
  assert.match(openapi, /identifier:/);
  assert.match(openapi, /username:/);
  for (const field of ['nickname', 'avatar', 'email', 'phone', 'lastLoginIp', 'lastLoginAt', 'passwordChangedAt']) {
    assert.match(openapi, new RegExp(field), field);
  }
  assert.match(generated, /identifierType/);
  assert.match(generated, /identifier\?: string/);
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const auth = readFileSync(join(root, `admin/apps/${ui}/src/api/core/auth.ts`), 'utf8');
    assert.match(auth, /identifier/);
    assert.match(auth, /AUTH_ENDPOINTS\.login/);
  }
  for (const driver of ['mysql', 'postgres']) {
    assert.equal(existsSync(join(root, `server/migrations/${driver}/000009_user_profile.up.sql`)), true);
    assert.equal(existsSync(join(root, `server/migrations/${driver}/000009_user_profile.down.sql`)), true);
  }
});
