import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');

test('B1.2 dictionary contract has tenant overrides, localization, and migration parity', () => {
  const paths = [
    'server/internal/application/dictionary/service.go',
    'server/internal/platform/dictionary/gorm_repository.go',
    'server/internal/transport/http/dictionary/handler.go',
    'server/migrations/mysql/000016_dictionary.up.sql',
    'server/migrations/mysql/000016_dictionary.down.sql',
    'server/migrations/postgres/000016_dictionary.up.sql',
    'server/migrations/postgres/000016_dictionary.down.sql',
  ];
  for (const path of paths) assert.ok(existsSync(new URL(path, root)), `missing ${path}`);
  const service = read('server/internal/application/dictionary/service.go');
  assert.match(service, /SystemOwned/);
  assert.match(service, /CacheVersion/);
  assert.match(service, /ImportItems/);
  const handler = read('server/internal/transport/http/dictionary/handler.go');
  assert.match(handler, /\/api\/admin\/v1\/dictionaries/);
  assert.match(handler, /Accept-Language/);
  const mysql = read('server/migrations/mysql/000016_dictionary.up.sql');
  const postgres = read('server/migrations/postgres/000016_dictionary.up.sql');
  for (const sql of [mysql, postgres]) {
    for (const token of ['dictionary_types', 'dictionary_items', 'dictionary_cache_versions', 'tenant_id', 'org_id', 'deleted_at', 'created_at', 'updated_at']) {
      assert.match(sql, new RegExp(token));
    }
  }
});

test('B1.2 three UI templates expose equivalent dictionary management pages', () => {
  for (const app of ['web-antd', 'web-ele', 'web-naive']) {
    const files = [
      `admin/apps/${app}/src/api/core/dictionary.ts`,
      `admin/apps/${app}/src/views/system/dictionary/index.vue`,
      `admin/apps/${app}/src/locales/langs/zh-CN/page.json`,
      `admin/apps/${app}/src/locales/langs/en-US/page.json`,
      `admin/apps/${app}/src/router/routes/modules/system.ts`,
    ];
    for (const path of files) assert.ok(existsSync(new URL(path, root)), `missing ${path}`);
    const page = read(`admin/apps/${app}/src/views/system/dictionary/index.vue`);
    const api = read(`admin/apps/${app}/src/api/core/dictionary.ts`);
    const route = read(`admin/apps/${app}/src/router/routes/modules/system.ts`);
    assert.match(page, /Accept-Language|locale/);
    assert.match(page, /aria-labelledby/);
    assert.match(page, /overflow-x-auto|table-scroll/);
    assert.match(api, /listDictionary/);
    assert.match(route, /SystemDictionary|system\/dictionary|dictionary/);
    for (const locale of ['zh-CN', 'en-US']) {
      const messages = JSON.parse(read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`));
      assert.ok(messages.dictionary?.title, `${app} ${locale} dictionary.title missing`);
      assert.ok(messages.dictionary?.save, `${app} ${locale} dictionary.save missing`);
    }
  }
});
