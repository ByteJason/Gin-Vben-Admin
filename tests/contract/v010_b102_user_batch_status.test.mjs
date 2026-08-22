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

test('B10.2 batch status exposes bounded per-item tenant-scoped results', () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/users\/batch-status:/);
  assert.match(openapi, /operationId: batchUpdateIAMUserStatus/);
  assert.match(openapi, /IAMUserBatchStatusRequest/);
  assert.match(openapi, /IAMUserBatchStatusResponse/);
  assert.match(openapi, /maxItems: 100/);
  assert.match(openapi, /IAMUserBatchStatusResult/);
  assert.match(openapi, /status:[\s\S]*enum: \[active, disabled, not_found, forbidden, invalid, error\]/);
  assert.doesNotMatch(openapi, /IAMUserBatchStatusRequest[\s\S]{0,800}(password|roleIds|loginEvents)/);

  assert.match(model, /type UserStatusChange struct/);
  assert.match(service, /func \(s \*Service\) BatchUpdateUserStatus\(/);
  assert.match(service, /maxUserBatchStatusItems\s*=\s*100/);
  assert.match(service, /UpdateUserStatuses/);
  assert.match(handler, /group\.POST\("\/users\/batch-status", handler\.batchUpdateUserStatus\)/);
  assert.match(handler, /func \(h \*Handler\) batchUpdateUserStatus\(/);
  assert.match(store, /func \(s \*GORMStore\) UpdateUserStatuses\(/);
  assert.match(generated, /batchUpdateIAMUserStatus: '\/admin\/v1\/iam\/users\/batch-status'/);
});
