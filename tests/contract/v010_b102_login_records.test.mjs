import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const [openapi, generated, handler, audit] = await Promise.all([
  readFile(new URL('../../contracts/openapi/admin-v1.yaml', import.meta.url), 'utf8'),
  readFile(new URL('../../admin/packages/api-client/src/generated/admin-v1.ts', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/transport/http/iam/handler.go', import.meta.url), 'utf8'),
  readFile(new URL('../../server/internal/application/audit/query.go', import.meta.url), 'utf8'),
]);

test('B10.2 user login records expose a bounded scoped read seam', () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/users\/\{id\}\/login-events:/);
  assert.match(openapi, /operationId: listIAMUserLoginEvents/);
  assert.match(openapi, /AuditPageResponse/);
  assert.match(openapi, /name: limit, in: query/);
  assert.match(openapi, /name: offset, in: query/);
  assert.match(generated, /listIAMUserLoginEvents: '\/admin\/v1\/iam\/users\/\{id\}\/login-events'/);
  assert.match(handler, /group\.GET\("\/users\/:id\/login-events", handler\.loginEvents\)/);
  assert.match(handler, /NewHandlerWithAudit/);
  assert.match(handler, /QueryLoginEvents/);
  assert.match(audit, /func \(s \*Service\) QueryLoginEvents\(/);
  assert.doesNotMatch(openapi, /LoginEvents[\s\S]{0,700}(passwordHash|token|authorization)/i);
});
