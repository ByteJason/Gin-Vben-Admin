import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('runtime commands dispatch once without a redundant profile gate', () => {
  const pkg = JSON.parse(read('admin/package.json'));
  for (const command of ['dev', 'build', 'preview']) {
    assert.match(pkg.scripts[command], /selected-dispatch\.mjs/);
    assert.doesNotMatch(pkg.scripts[command], /profile-gate|&&/);
  }
});

test('configuration examples contain deployment inputs, not copied business defaults', () => {
  const env = read('.env.example');
  const yaml = read('server/configs/server.example.yaml');
  assert.doesNotMatch(env, /^(APP_UI_|AUTH_CAPTCHA_|AUTH_ACCESS_TTL|I18N_)/m);
  assert.doesNotMatch(yaml, /^(version|observability|i18n|mail):/m);
  assert.doesNotMatch(yaml, /^\s+(captcha_\w+|access_ttl|refresh_ttl|allowed_mimes|max_bytes):/m);
  assert.match(env, /^DATABASE_DRIVER=postgres$/m);
  assert.match(yaml, /^  driver: postgres$/m);
});

test('installer writes no unused UI environment mirrors or hard-coded auth defaults', () => {
  const installer = read('server/internal/platform/installplatform/environment_installer.go');
  assert.doesNotMatch(installer, /"(?:APP_UI_ACTIVE|APP_UI_MODE|AUTH_ACCESS_TTL|AUTH_CAPTCHA_\w+)"\s*:/);
  assert.doesNotMatch(read('admin/scripts/init-state.mjs'), /workspaceActiveUIEnvironment|runtimeEnvironmentUpdated/);
});

// The non-browser bootstrap path must not silently restore the old driver.
test('bootstrap defaults to PostgreSQL while keeping explicit MySQL available', () => {
  for (const [args, driver] of [[[], 'postgres'], [['--database', 'mysql'], 'mysql']]) {
    const result = spawnSync(process.execPath, [new URL('../../scripts/bootstrap.mjs', import.meta.url).pathname, '--check', ...args], {encoding: 'utf8'});
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.match(result.stdout, new RegExp(`BOOTSTRAP_DATABASE=${driver}`));
  }
});
