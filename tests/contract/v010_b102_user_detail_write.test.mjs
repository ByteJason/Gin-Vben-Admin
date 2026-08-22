import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const [openapi, service, model, handler, store] = await Promise.all([
  readFile(new URL('../../contracts/openapi/admin-v1.yaml', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/application/iam/service.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/domain/iam/model.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/transport/http/iam/handler.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/platform/iamplatform/gorm_store.go', import.meta.url), 'utf8'),
]);

test('B10.2 user detail/create/update exposes bounded tenant-scoped write seams', () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/users\/\{id\}:/);
  assert.match(openapi, /operationId: getIAMUser/);
  assert.match(openapi, /operationId: updateIAMUser/);
  assert.match(openapi, /IAMUserCreateRequest/);
  assert.match(openapi, /IAMUserUpdateRequest/);
  assert.match(openapi, /password:[\s\S]*writeOnly: true/);
  assert.match(openapi, /'409':[\s\S]*IAMUserConflict/);
  assert.match(openapi, /IAMUserConflict:[\s\S]*const: 10011/);
  assert.match(openapi, /orgId:[\s\S]*string/);
  assert.doesNotMatch(openapi, /IAMUser(CreateRequest|UpdateRequest)[\s\S]{0,500}roleIds/);

  assert.match(model, /ErrUserConflict/);
  assert.match(model, /PasswordHash\s+string/);
  assert.match(service, /func \(s \*Service\) GetUser\(/);
  assert.match(service, /func \(s \*Service\) CreateUser\(/);
  assert.match(service, /func \(s \*Service\) UpdateUser\(/);
  assert.match(service, /ErrUserConflict/);
  assert.match(service, /tenant\.ErrOrganizationDenied/);
  assert.match(handler, /group\.GET\("\/users\/:id", handler\.getUser\)/);
  assert.match(handler, /group\.PATCH\("\/users\/:id", handler\.updateUser\)/);
  assert.match(handler, /Password.*json:\"password\"/);
  assert.match(handler, /http\.StatusConflict/);
  assert.match(store, /func \(s \*GORMStore\) CreateUser\(/);
  assert.match(store, /func \(s \*GORMStore\) UpdateUser\(/);
  assert.match(store, /ErrUserConflict/);
  assert.match(service, /PasswordHasher/);
});
