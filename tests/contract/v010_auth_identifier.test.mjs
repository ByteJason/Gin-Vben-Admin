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
  const schemaPath = join(root, 'server/migrations/schema.go');
  const modelPath = join(root, 'server/internal/platform/persistence/model/identity_models.go');
  assert.equal(existsSync(schemaPath), true, 'single GORM schema file');
  assert.equal(existsSync(modelPath), true, 'identity persistence models');
  const schema = readFileSync(modelPath, 'utf8');
  for (const field of ['UsernameNormalized', 'EmailNormalized', 'Nickname', 'Avatar', 'Phone', 'LastLoginIP', 'LastLoginAt', 'PasswordChangedAt']) {
    assert.match(schema, new RegExp(`\\b${field}\\b`), field);
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

    const identifierField = login.match(
      /component:\s*'VbenInput',[\s\S]*?fieldName:\s*'identifier'/,
    )?.[0];
    assert.ok(identifierField, `${ui} identifier field schema`);
    assert.match(
      identifierField,
      /autocomplete:\s*'username'/,
      `${ui} identifier autocomplete`,
    );
    assert.match(
      identifierField,
      /autocapitalize:\s*'none'/,
      `${ui} identifier autocapitalize`,
    );
    assert.match(
      identifierField,
      /spellcheck:\s*false/,
      `${ui} identifier spellcheck`,
    );

    const passwordField = login.match(
      /component:\s*'VbenInputPassword',[\s\S]*?fieldName:\s*'password'/,
    )?.[0];
    assert.ok(passwordField, `${ui} password field schema`);
    assert.match(
      passwordField,
      /autocomplete:\s*'current-password'/,
      `${ui} password autocomplete`,
    );

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

test('0.10 shared login remembers only the identifier under the existing storage key', () => {
  const login = readFileSync(
    join(root, 'admin/packages/effects/common-ui/src/ui/authentication/login.vue'),
    'utf8',
  );

  assert.match(
    login,
    /const REMEMBER_ME_KEY = `REMEMBER_ME_USERNAME_\$\{location\.hostname\}`/,
    'keeps the existing localStorage key compatible',
  );
  assert.match(
    login,
    /const localIdentifier = localStorage\.getItem\(REMEMBER_ME_KEY\) \|\| ''/,
    'reads the remembered identifier',
  );
  assert.match(
    login,
    /rememberMe\.value \? values\?\.identifier : ''/,
    'stores the identifier when remember-me is checked',
  );
  assert.match(
    login,
    /formApi\.setFieldValue\('identifier', localIdentifier\)/,
    'restores the remembered value into the identifier field',
  );
  assert.doesNotMatch(login, /values(?:\?\.|\.)(?:password|username)/);
});
