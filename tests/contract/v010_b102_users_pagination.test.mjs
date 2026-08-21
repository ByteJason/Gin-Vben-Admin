import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('0.10 B10.2 user list exposes bounded tenant-scoped pagination and search', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const handler = read('server/internal/transport/http/iam/handler.go');
  const service = read('server/internal/application/iam/service.go');
  const model = read('server/internal/domain/iam/model.go');

  const pathStart = openapi.indexOf('  /api/admin/v1/iam/users:');
  const pathEnd = openapi.indexOf('\n  /api/admin/v1/iam/roles:', pathStart);
  const section = openapi.slice(pathStart, pathEnd);
  assert.match(section, /name: page[\s\S]*in: query/);
  assert.match(section, /name: pageSize[\s\S]*maximum: 100/);
  for (const parameter of ['keyword', 'status', 'roleId', 'orgId', 'sort']) {
    assert.match(section, new RegExp(`name: ${parameter}`), parameter);
  }
  assert.match(section, /IAMUserPage/);
  assert.match(openapi, /status: \{ type: string, enum: \[active, disabled\] \}/);
  assert.match(section, /'400':/);
  assert.match(model, /type UserListQuery struct/);
  assert.match(model, /type UserPage struct/);
  assert.match(service, /func \(s \*Service\) ListUsersPage/);
  assert.match(handler, /Query\("page"\)/);
  assert.match(handler, /Query\("pageSize"\)/);
  assert.match(handler, /Query\("keyword"\)/);
  assert.match(handler, /Organization/);
  assert.doesNotMatch(handler, /ShouldBindJSON\(&req\).*page/);
});
