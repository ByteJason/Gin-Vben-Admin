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

test('0.10 all admin login forms submit a shared username-or-email identifier field', () => {
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const login = readFileSync(
      join(root, `admin/apps/${ui}/src/views/_core/authentication/login.vue`),
      'utf8',
    );
    assert.match(login, /fieldName:\s*'identifier'/, `${ui} identifier field`);
    assert.match(login, /authentication\.identifier['"]/, `${ui} identifier label`);
    assert.match(login, /authentication\.identifierTip['"]/, `${ui} identifier tip`);
    assert.doesNotMatch(login, /fieldName:\s*'username'/, `${ui} legacy username field`);

    const store = readFileSync(join(root, `admin/apps/${ui}/src/store/auth.ts`), 'utf8');
    assert.match(store, /identifier:\s*params\.identifier(?:\s*\?\?\s*params\.username)?/, `${ui} store identifier payload`);
    assert.doesNotMatch(store, /username:\s*params\.username/, `${ui} legacy username payload`);
  }

  const zh = JSON.parse(
    readFileSync(join(root, 'admin/packages/locales/src/langs/zh-CN/authentication.json'), 'utf8'),
  );
  const en = JSON.parse(
    readFileSync(join(root, 'admin/packages/locales/src/langs/en-US/authentication.json'), 'utf8'),
  );
  assert.equal(zh.identifier, '用户名或邮箱');
  assert.equal(zh.identifierTip, '请输入用户名或邮箱');
  assert.equal(en.identifier, 'Username or email');
  assert.equal(en.identifierTip, 'Please enter username or email');
});
