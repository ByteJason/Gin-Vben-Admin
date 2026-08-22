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
