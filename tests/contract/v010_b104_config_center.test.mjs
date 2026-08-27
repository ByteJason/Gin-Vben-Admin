import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

test('B10.4 settings service exposes classified schema, precedence, encryption and connection tests', () => {
  const servicePath = 'server/internal/application/settings/service.go';
  const envelopePath = 'server/internal/application/settings/envelope.go';
  const handlerPath = 'server/internal/transport/http/settings/handler.go';
  const schemaPath = 'server/migrations/schema.go';
  const modelPath = 'server/internal/platform/persistence/model/admin_settings_models.go';
  for (const path of [servicePath, envelopePath, handlerPath, schemaPath, modelPath]) {
    assert.equal(existsSync(new URL(path, root)), true, path);
  }
  const service = read(servicePath);
  const envelope = read(envelopePath);
  const handler = read(handlerPath);
  for (const category of ['basic', 'security', 'mail', 'file', 'captcha', 'i18n']) {
    assert.match(service, new RegExp(`Category:\\s*(?:Category${category[0].toUpperCase()}${category.slice(1)}|"?${category})`), `${category} category`);
  }
  for (const key of [
    'basic.site_name',
    'security.jwt_secret',
    'mail.enabled',
    'file.max_size',
    'captcha.enabled',
    'i18n.mode',
    'i18n.default_locale',
    'i18n.supported_locales',
  ]) {
    assert.match(service, new RegExp(key.replace('.', '\\.'), 'g'), key);
  }
  assert.match(service, /SourceEnv|SourceDotEnv|SourceYAML|SourceDatabase|SourceDefault/);
  assert.match(service, /SourceResolver|ResolveSource|EffectiveSource/);
  assert.match(service, /EnvelopeEncryptor|Encryptor/);
  assert.match(envelope, /aes|GCM|envelope/i);
  assert.match(envelope, /Encrypt/);
  assert.match(envelope, /Decrypt/);
  assert.match(handler, /testConnection|connectionTest|TestConnection/);
  assert.match(handler, /history/);
  assert.match(handler, /rollback/);
  assert.match(read(modelPath), /Encrypted|encrypted|ciphertext/i);
});

for (const app of apps) {
  test(`B10.4 ${app} exposes the classified settings center`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/settings.ts`;
    const viewPath = `admin/apps/${app}/src/views/system/settings/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/system.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} settings api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} settings view`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    assert.match(api, /SettingCategory|category/);
    assert.match(api, /source/);
    assert.match(api, /listSettingHistoryApi/);
    assert.match(api, /rollbackSettingApi/);
    assert.match(api, /test.*Connection|connection.*Test/i);
    for (const token of ['listSettingDefinitionsApi', 'getSettingApi', 'updateSettingApi', 'listSettingHistoryApi', 'rollbackSettingApi']) {
      assert.match(view, new RegExp(token), `${app}/${token}`);
    }
    for (const token of ['basic', 'security', 'mail', 'file', 'captcha', 'i18n', 'source', 'history', 'rollback', 'connection']) {
      assert.match(view, new RegExp(token, 'i'), `${app}/${token}`);
    }
    assert.match(route, /views\/system\/settings\/index\.vue/);
    assert.match(route, /name:\s*'menu-system-settings'/);
    assert.match(route, /authority:\s*\['system:settings:read'\]/);
    assert.match(route, /path:\s*'\/system\/settings'/);
  });
}

for (const app of apps) {
  for (const locale of ['zh-CN', 'en-US']) {
    test(`B10.4 ${app}/${locale} has bilingual settings-center copy`, () => {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        'settings', 'settingsDescription', 'settingsLoading', 'settingsLoadError',
        'settingsCategory', 'settingsSource', 'settingsDatabase', 'settingsEnvironment',
        'settingsHistory', 'settingsRollback', 'settingsConnectionTest',
        'settingsConnectionSuccess', 'settingsConnectionError', 'settingsRestartRequired',
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    });
  }
}
