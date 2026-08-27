import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

test('B10.5 audit service exposes taxonomy, redaction, export, retention dry-run and sinks', () => {
  const servicePath = 'server/internal/application/audit/query.go';
  const exportTestPath = 'server/internal/application/audit/export.go';
  const handlerPath = 'server/internal/transport/http/audit/handler.go';
  const schemaPath = 'server/migrations/schema.go';
  const modelPath = 'server/internal/platform/persistence/model/audit_models.go';
  for (const path of [servicePath, exportTestPath, handlerPath, schemaPath, modelPath]) {
    assert.equal(existsSync(new URL(path, root)), true, path);
  }
  const service = read(servicePath);
  const exported = read(exportTestPath);
  const handler = read(handlerPath);
  for (const token of ['CategoryLogin', 'CategoryOperation', 'CategorySystem', 'redact', 'Category']) {
    assert.match(`${service}\n${exported}`, new RegExp(token), token);
  }
  for (const token of ['ExportFormatCSV', 'ExportFormatJSON', 'RetentionDryRun', 'ConsoleSink', 'FileSink']) {
    assert.match(exported, new RegExp(token), token);
  }
  assert.match(handler, /export/);
  assert.match(handler, /retention\/dry-run/);
  assert.match(read(modelPath), /Category/);
});

test('B10.5 OpenAPI and generated client expose export and retention routes', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const client = read('admin/packages/api-client/src/generated/admin-v1.ts');
  for (const token of ['audit/events/export', 'audit/retention/dry-run', 'exportAuditEvents', 'auditRetentionDryRun']) {
    assert.match(`${openapi}\n${client}`, new RegExp(token), token);
  }
  for (const token of ['AuditCategory', 'AuditExport', 'RetentionDryRun']) {
    assert.match(openapi, new RegExp(token), token);
  }
});

for (const app of apps) {
  test(`B10.5 ${app} exposes audit log UI and export controls`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/audit.ts`;
    const viewPath = `admin/apps/${app}/src/views/system/audit/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/system.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} audit api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} audit view`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    for (const token of ['queryAuditEventsApi', 'exportAuditEventsApi', 'retentionDryRunApi']) {
      assert.match(api, new RegExp(token), `${app}/${token}`);
    }
    for (const token of ['login', 'operation', 'system', 'CSV', 'JSON', 'dry-run', 'redact', 'loading', 'empty', 'error']) {
      assert.match(view, new RegExp(token, 'i'), `${app}/${token}`);
    }
    assert.match(route, /views\/system\/audit\/index\.vue/);
    assert.match(route, /name:\s*'menu-operations-audit'/);
    assert.match(route, /authority:\s*\['ops:audit:read'\]/);
    assert.match(route, /path:\s*'\/system\/audit'/);
  });
  for (const locale of ['zh-CN', 'en-US']) {
    test(`B10.5 ${app}/${locale} has audit bilingual copy`, () => {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of ['audit', 'auditDescription', 'auditLoading', 'auditEmpty', 'auditExportCSV', 'auditExportJSON', 'auditRetentionDryRun']) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    });
  }
}
