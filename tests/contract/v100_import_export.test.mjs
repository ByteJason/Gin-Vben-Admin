import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = process.cwd();

test('B1.4 OpenAPI and dual-database migration seams are present', () => {
  const openapi = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  for (const token of [
    '/api/admin/v1/import-export/imports/preview',
    '/api/admin/v1/import-export/imports/commit',
    '/api/admin/v1/import-export/exports',
    '/api/admin/v1/import-export/jobs/{id}/errors',
    'ImportPreview:',
    'ImportExportJob:',
    'downloadImportTemplate',
  ]) assert.match(openapi, new RegExp(token.replace(/[{}]/g, '\\$&')));
  for (const driver of ['mysql', 'postgres']) {
    for (const suffix of ['up', 'down']) {
      assert.ok(existsSync(join(root, `server/migrations/${driver}/000019_import_export_jobs.${suffix}.sql`)));
    }
  }
});

test('B1.4 three UI templates expose equivalent bounded job controls', () => {
  for (const app of ['web-antd', 'web-ele', 'web-naive']) {
    const api = readFileSync(join(root, `admin/apps/${app}/src/api/core/import-export.ts`), 'utf8');
    const page = readFileSync(join(root, `admin/apps/${app}/src/views/system/import-export/index.vue`), 'utf8');
    const route = readFileSync(join(root, `admin/apps/${app}/src/router/routes/modules/system.ts`), 'utf8');
    for (const token of ['previewImportApi', 'commitImportApi', 'startExportApi', 'cancelImportExportJobApi', 'retryImportExportJobApi', 'downloadErrorRowsApi']) assert.match(api, new RegExp(token));
    for (const token of ['preview', 'commit', 'cancel', 'retry', 'download', 'export', '50']) assert.match(page, new RegExp(token, 'i'));
    assert.match(route, /import-export/);
    for (const locale of ['zh-CN', 'en-US']) {
      const text = readFileSync(join(root, `admin/apps/${app}/src/locales/langs/${locale}/page.json`), 'utf8');
      for (const key of ['importExport', 'importPreview', 'importCommit', 'importCancel', 'importRetry', 'importDownloadErrors', 'exportExpired']) assert.match(text, new RegExp(`"${key}"\\s*:`));
    }
  }
});
