import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

test('B10.6 local file provider exposes metadata, safe storage and lifecycle HTTP', () => {
  const paths = [
    'server/internal/application/file/local.go',
    'server/internal/transport/http/file/handler.go',
    'server/migrations/mysql/000013_file_objects.up.sql',
    'server/migrations/postgres/000013_file_objects.up.sql',
  ];
  for (const path of paths) assert.equal(existsSync(new URL(path, root)), true, path);
  const source = paths.slice(0, 2).map(read).join('\n');
  for (const token of ['LocalStore', 'CleanupDryRun', 'TenantID', 'SignedURL', 'multipart', 'preview', 'ACL']) {
    assert.match(source, new RegExp(token, 'i'), token);
  }
  assert.match(read(paths[2]), /tenant_id/i);
  assert.match(read(paths[3]), /sha256/i);
});

test('B10.6 OpenAPI and generated client expose file center routes', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const client = read('admin/packages/api-client/src/generated/admin-v1.ts');
  for (const token of ['files/upload', 'files/{id}/download', 'files/{id}/signed-url', 'files/cleanup/dry-run', 'listFiles', 'uploadFile', 'deleteFile']) {
    assert.match(`${openapi}\n${client}`, new RegExp(token), token);
  }
  for (const token of ['FileObject', 'FilePage', 'FileCleanupDryRun']) assert.match(openapi, new RegExp(token), token);
});

for (const app of apps) {
  test(`B10.6 ${app} exposes local file center UI`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/files.ts`;
    const viewPath = `admin/apps/${app}/src/views/system/files/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/system.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} files api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} files view`);
    for (const token of ['listFilesApi', 'uploadFileApi', 'downloadFileApi', 'deleteFileApi', 'cleanupDryRunApi']) assert.match(read(apiPath), new RegExp(token), `${app}/${token}`);
    for (const token of ['upload', 'preview', 'download', 'delete', 'MIME', 'size', 'ACL', 'loading', 'empty', 'error']) assert.match(read(viewPath), new RegExp(token, 'i'), `${app}/${token}`);
    assert.match(read(routePath), /views\/system\/files\/index\.vue/);
    assert.match(read(routePath), /name:\s*'menu-system-files'/);
    assert.match(read(routePath), /authority:\s*\['system:files:read'\]/);
    assert.match(read(routePath), /path:\s*'\/system\/files'/);
  });
  for (const locale of ['zh-CN', 'en-US']) {
    test(`B10.6 ${app}/${locale} has file center copy`, () => {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of ['files', 'filesDescription', 'filesUpload', 'filesPreview', 'filesDownload', 'filesDelete', 'filesCleanupDryRun']) assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
    });
  }
}
