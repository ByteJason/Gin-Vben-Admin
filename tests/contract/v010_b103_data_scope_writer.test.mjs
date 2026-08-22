import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(`../../${path}`, import.meta.url), "utf8");

const [openapi, model, service, handler, store, generated] = await Promise.all([
  read("contracts/openapi/admin-v1.yaml"),
  read("server/internal/domain/iam/model.go"),
  read("server/internal/application/iam/service.go"),
  read("server/internal/transport/http/iam/handler.go"),
  read("server/internal/platform/iamplatform/gorm_store.go"),
  read("admin/packages/api-client/src/generated/admin-v1.ts"),
]);

test("B10.3 data-scope writer exposes bounded atomic role replacement", () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/roles\/\{id\}\/data-scopes:/);
  assert.match(openapi, /operationId: replaceIAMRoleDataScopes/);
  assert.match(openapi, /IAMRoleDataScopesReplaceRequest/);
  assert.match(openapi, /maxItems: 50/);
  assert.match(model, /type DataScope struct[\s\S]{0,260}Scope/);
  assert.match(service, /type RoleDataScopesInput struct/);
  assert.match(service, /MaxRoleDataScopeBindings/);
  assert.match(service, /func \(s \*Service\) ReplaceRoleDataScopes\(/);
  assert.match(service, /AssignRoleDataScopes/);
  assert.match(handler, /group\.PUT\("\/roles\/:id\/data-scopes", handler\.replaceRoleDataScopes\)/);
  assert.match(handler, /func \(h \*Handler\) replaceRoleDataScopes\(/);
  assert.match(service, /func \(s \*MemoryStore\) AssignRoleDataScopes\(/);
  assert.match(store, /func \(s \*GORMStore\) AssignRoleDataScopes\(/);
  assert.match(store, /iam_data_scopes/);
  assert.match(store, /WithinTransaction/);
  assert.match(generated, /replaceIAMRoleDataScopes: '\/admin\/v1\/iam\/roles\/\{id\}\/data-scopes'/);
  assert.doesNotMatch(store, /Table\("role_data_scopes"\)/);
});
