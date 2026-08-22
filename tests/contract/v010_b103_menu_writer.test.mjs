import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");

test("B10.3 menu writer exposes bounded CRUD and reorder routes", () => {
  const openapi = read("contracts/openapi/admin-v1.yaml");
  const generated = read("admin/packages/api-client/src/generated/admin-v1.ts");
  const handler = read("server/internal/transport/http/iam/handler.go");
  const service = read("server/internal/application/iam/service.go");
  const model = read("server/internal/domain/iam/model.go");

  assert.match(openapi, /operationId: (create|save)IAMMenu/);
  assert.match(openapi, /operationId: getIAMMenu/);
  assert.match(openapi, /operationId: updateIAMMenu/);
  assert.match(openapi, /operationId: deleteIAMMenu/);
  assert.match(openapi, /operationId: reorderIAMMenus/);
  for (const field of ["type", "component", "redirect", "icon", "sort", "visible", "keepAlive", "external", "permission", "parentId"]) {
    assert.match(openapi, new RegExp(field));
    assert.match(handler, new RegExp(field));
    assert.match(model, new RegExp(field, "i"));
  }
  assert.match(openapi, /\/api\/admin\/v1\/menu\/all:/);
  assert.match(openapi, /operationId: (listIAMMenuRoutes|listVisibleMenus)/);
  assert.match(handler, /menuRoutes|listMenuRoutes/);
  assert.match(service, /CreateMenu|UpdateMenu|DeleteMenu|ReorderMenus/);
  assert.match(service, /BuildMenuRoutes|MenuRoute/);
  assert.match(generated, /createIAMMenu|updateIAMMenu|deleteIAMMenu|reorderIAMMenus/);
});

test("B10.3 menu metadata migration exists for both supported dialects", () => {
  for (const dialect of ["mysql", "postgres"]) {
    const up = `server/migrations/${dialect}/000010_menu_metadata.up.sql`;
    const down = `server/migrations/${dialect}/000010_menu_metadata.down.sql`;
    assert.equal(existsSync(new URL(up, root)), true, `${dialect} up`);
    assert.equal(existsSync(new URL(down, root)), true, `${dialect} down`);
    const sql = read(up);
    for (const column of ["menu_type", "component", "redirect", "icon", "permission", "keep_alive", "external"]) {
      assert.match(sql, new RegExp(column));
    }
  }
});

test("B10.3 route projection excludes button nodes and uses registry components", () => {
  const route = read("server/internal/application/iam/menu_routes.go");
  assert.match(route, /MenuRoute/);
  assert.match(route, /button|MenuTypeButton/);
  assert.match(route, /Component/);
  assert.match(route, /sort/);
  assert.match(route, /permission/);
});
