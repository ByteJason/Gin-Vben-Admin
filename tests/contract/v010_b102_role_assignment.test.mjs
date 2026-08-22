import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const [openapi, model, service, handler, store, generated] = await Promise.all([
  readFile(new URL('../../contracts/openapi/admin-v1.yaml', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/domain/iam/model.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/application/iam/service.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/transport/http/iam/handler.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/platform/iamplatform/gorm_store.go', import.meta.url), 'utf8'),
  readFile(new URL('../../admin/packages/api-client/src/generated/admin-v1.ts', import.meta.url), 'utf8'),
]);

test('B10.2 role assignment exposes bounded tenant-scoped replacement', () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/roles\/\{id\}\/users:/);
  assert.match(openapi, /operationId: replaceIAMRoleUsers/);
  assert.match(openapi, /IAMRoleUsersReplaceRequest/);
  assert.match(openapi, /maxItems: 100/);
  assert.match(openapi, /userIds:/);
  assert.doesNotMatch(openapi, /IAMRoleUsersReplaceRequest[\s\S]{0,500}(password|passwordHash|loginEvents)/);

  assert.match(model, /type Role struct/);
  assert.match(service, /type RoleUsersInput struct/);
  assert.match(service, /func \(s \*Service\) ReplaceRoleUsers\(/);
  assert.match(service, /MaxRoleAssignmentUsers/);
  assert.match(service, /AssignRoleUsers/);
  assert.match(handler, /group\.PUT\("\/roles\/:id\/users", handler\.replaceRoleUsers\)/);
  assert.match(handler, /func \(h \*Handler\) replaceRoleUsers\(/);
  assert.match(store, /func \(s \*GORMStore\) AssignRoleUsers\(/);
  assert.match(store, /WithinTransaction/);
  assert.match(generated, /replaceIAMRoleUsers: '\/admin\/v1\/iam\/roles\/\{id\}\/users'/);
});
