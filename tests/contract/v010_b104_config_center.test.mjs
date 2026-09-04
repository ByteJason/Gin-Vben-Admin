import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

test('system settings exposes the module contract and keeps active defaults mail-free', () => {
  const servicePath = 'server/internal/application/settings/service.go';
  const modulePath = 'server/internal/application/settings/module.go';
  const snapshotPath = 'server/internal/application/settings/runtime_snapshot.go';
  const handlerPath = 'server/internal/transport/http/settings/handler.go';
  const migrationPath = 'server/migrations/versions/admin/v003_settings_mail_cleanup.go';
  const openapi = read('contracts/openapi/admin-v1.yaml');
  for (const path of [servicePath, modulePath, snapshotPath, handlerPath, migrationPath]) {
    assert.equal(existsSync(new URL(path, root)), true, path);
  }

  const service = read(servicePath);
  const module = read(modulePath);
  const handler = read(handlerPath);
  const defaultSection = service.slice(
    service.indexOf('func DefaultDefinitions()'),
    service.indexOf('// legacyMailDefinitions'),
  );
  assert.doesNotMatch(defaultSection, /mail\.|email\.|smtp\./i);
  assert.match(service, /func legacyMailDefinitions\(\)/);
  for (const token of [
    'DisplayName',
    'Description',
    'ValueKind',
    'AllowedValues',
    'SourcePolicy',
    'ScopePolicy',
    'ApplyMode',
    'Sensitive',
  ]) {
    assert.match(service, new RegExp(token), token);
  }
  for (const token of [
    'ValidateModule',
    'SaveModule',
    'ResetModule',
    'ClearCredentials',
    'StatusSavedAndApplied',
    'StatusSavedApplyFailed',
    'ReplaceWithSourcesFor',
  ]) {
    assert.match(module, new RegExp(token), token);
  }
  assert.match(read(snapshotPath), /SnapshotFor/);

  // The production registration seam is module-only. Legacy handlers remain
  // isolated for rolling-upgrade consumers and are not mounted by the app.
  const productionRoutes = handler.slice(
    handler.indexOf('func registerSystemRoutes'),
    handler.indexOf('func registerRoutes(', handler.indexOf('func registerSystemRoutes')),
  );
  assert.match(productionRoutes, /clear-credentials/);
  assert.doesNotMatch(productionRoutes, /history|rollback|testConnection/i);

  assert.match(openapi, /\/api\/admin\/v1\/settings\/modules:\s*\n/);
  assert.match(openapi, /operationId: updateSettingModule/);
  assert.match(openapi, /\/api\/admin\/v1\/settings\/modules\/\{module\}\/clear-credentials:/);
  assert.match(openapi, /operationId: clearSettingModuleCredentials/);
  const settingSchema = openapi.slice(
    openapi.indexOf('    SettingDefinition:'),
    openapi.indexOf('    SettingUpdateRequest:', openapi.indexOf('    SettingDefinition:')),
  );
  assert.doesNotMatch(settingSchema, /mail|smtp|email/i);
});

for (const app of apps) {
  test(`system settings ${app} uses business modules and one save boundary`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/settings.ts`;
    const viewPath = `admin/apps/${app}/src/views/system/settings/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/system.ts`;
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);

    for (const token of [
      'listSettingModulesApi',
      'getSettingModuleApi',
      'updateSettingModuleApi',
      'validateSettingModuleApi',
      'resetSettingModuleApi',
      'clearSettingModuleCredentialsApi',
    ]) {
      assert.match(api, new RegExp(token), `${app}/${token}`);
      assert.match(view, new RegExp(token), `${app}/${token}`);
    }
    assert.doesNotMatch(view, /listSettingHistoryApi|rollbackSettingApi|testConnection/i);
    assert.doesNotMatch(view, /#\{\{\s*activeView\.revision/);
    for (const token of ['displayName', 'description', 'applyMode', 'source', 'saveApply', 'clearCredential']) {
      assert.match(view, new RegExp(token), `${app}/${token}`);
    }
    assert.match(route, /views\/system\/settings\/index\.vue/);
    assert.match(route, /name:\s*'menu-system-settings'/);
    assert.match(route, /authority:\s*\['system:settings:read'\]/);
    assert.match(route, /path:\s*'\/system\/settings'/);
  });

  for (const locale of ['zh-CN', 'en-US']) {
    test(`system settings ${app}/${locale} has module-state copy without retired actions`, () => {
      const parsed = JSON.parse(read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`));
      const settings = parsed.settings;
      assert.ok(settings, `${app}/${locale}/settings`);
      for (const key of [
        'title',
        'description',
        'searchPlaceholder',
        'unsaved',
        'discard',
        'validate',
        'restoreDefaults',
        'saveApply',
        'source',
        'applyMode',
        'status',
        'clearCredential',
      ]) {
        assert.ok(Object.hasOwn(settings, key), `${app}/${locale}/${key}`);
      }
      for (const retired of ['history', 'rollback', 'connectionTest', 'smtp', 'mail']) {
        assert.equal(Object.hasOwn(settings, retired), false, `${app}/${locale}/${retired}`);
      }
    });
  }
}
