import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (path) => readFile(new URL(`../../${path}`, import.meta.url), "utf8");

const [openapi, model, service, handler, memory, store, generated] =
  await Promise.all([
    read("contracts/openapi/admin-v1.yaml"),
    read("server/internal/domain/iam/model.go"),
    read("server/internal/application/iam/service.go"),
    read("server/internal/transport/http/iam/handler.go"),
    read("server/internal/application/iam/service.go"),
    read("server/internal/platform/iamplatform/gorm_store.go"),
    read("admin/packages/api-client/src/generated/admin-v1.ts"),
  ]);

test("B10.3 role-permission relation keeps a bounded atomic write seam", () => {
  assert.match(openapi, /\/api\/admin\/v1\/iam\/roles\/\{id\}\/permissions:/);
  assert.match(openapi, /operationId: replaceIAMRolePermissions/);
  assert.match(openapi, /IAMRolePermissionsReplaceRequest/);
  assert.match(openapi, /permissionIds:/);
  assert.match(openapi, /maxItems: 200/);
  assert.match(model, /type Role struct[\s\S]{0,260}PermissionIDs/);
  assert.match(service, /type RolePermissionsInput struct/);
  assert.match(service, /MaxRolePermissionBindings/);
  assert.match(service, /func \(s \*Service\) ReplaceRolePermissions\(/);
  assert.match(service, /AssignRolePermissions/);
  assert.match(handler, /group\.PUT\("\/roles\/:id\/permissions", handler\.replaceRolePermissions\)/);
  assert.match(handler, /func \(h \*Handler\) replaceRolePermissions\(/);
  assert.match(memory, /func \(s \*MemoryStore\) AssignRolePermissions\(/);
  assert.match(store, /func \(s \*GORMStore\) AssignRolePermissions\(/);
  assert.match(store, /iam_policies/);
  assert.match(store, /WithinTransaction/);
  assert.match(generated, /replaceIAMRolePermissions: '\/admin\/v1\/iam\/roles\/\{id\}\/permissions'/);
  assert.doesNotMatch(openapi, /role_permissions/);
  assert.doesNotMatch(store, /Table\("role_permissions"\)/);
});
