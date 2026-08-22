import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const [openapi, service, handler, store, generated] = await Promise.all([
  readFile(new URL('../../contracts/openapi/admin-v1.yaml', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/application/iam/service.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/transport/http/iam/handler.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/platform/iamplatform/gorm_store.go', import.meta.url), 'utf8'),
  readFile(new URL('../../admin/packages/api-client/src/generated/admin-v1.ts', import.meta.url), 'utf8'),
]);

test('B10.2 user delete is a tenant-scoped idempotent soft-delete seam', () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/users\/\{id\}:/);
  assert.match(openapi, /operationId: deleteIAMUser/);
  assert.match(openapi, /summary: 默认软删除管理用户/);
  assert.match(openapi, /delete:[\s\S]*'200':[\s\S]*SuccessResponse/);
  assert.match(openapi, /delete:[\s\S]*'404':[\s\S]*NotFound/);
  assert.match(openapi, /delete:[\s\S]*'403':[\s\S]*Forbidden/);

  assert.match(service, /func \(s \*Service\) DeleteUser\(/);
  assert.match(service, /SoftDeleteUser/);
  assert.match(service, /Active = false/);
  assert.match(handler, /group\.DELETE\("\/users\/:id", handler\.deleteUser\)/);
  assert.match(handler, /func \(h \*Handler\) deleteUser\(/);
  assert.match(store, /func \(s \*GORMStore\) SoftDeleteUser\(/);
  assert.match(store, /"status"\s*:\s*"disabled"/);
  assert.doesNotMatch(store, /SoftDeleteUser[\s\S]{0,1000}DELETE FROM users/);
  assert.match(generated, /deleteIAMUser: '\/admin\/v1\/iam\/users\/\{id\}'/);
});
