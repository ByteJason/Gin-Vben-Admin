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

test('B10.2 admin reset-password exposes a bounded credential-only seam', () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/users\/\{id\}\/reset-password:/);
  assert.match(openapi, /operationId: resetIAMUserPassword/);
  assert.match(openapi, /IAMUserPasswordResetRequest/);
  assert.match(openapi, /password: \{ type: string, minLength: 8, maxLength: 128, writeOnly: true \}/);
  assert.doesNotMatch(openapi, /IAMUserPasswordResetRequest[\s\S]{0,600}(roleIds|profile|loginEvents|token)/);

  assert.match(model, /PasswordChangedAt/);
  assert.match(service, /type UserPasswordResetInput struct/);
  assert.match(service, /func \(s \*Service\) ResetUserPassword\(/);
  assert.match(service, /validManagementPassword/);
  assert.match(service, /UpdateUserPassword/);
  assert.match(handler, /group\.POST\("\/users\/:id\/reset-password", handler\.resetUserPassword\)/);
  assert.match(handler, /func \(h \*Handler\) resetUserPassword\(/);
  assert.match(store, /func \(s \*GORMStore\) UpdateUserPassword\(/);
  assert.match(generated, /resetIAMUserPassword: '\/admin\/v1\/iam\/users\/\{id\}\/reset-password'/);
});
