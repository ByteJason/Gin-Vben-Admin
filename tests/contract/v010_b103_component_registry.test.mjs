import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");

test("B10.3 component registry is a bounded, bearer-protected contract", () => {
  const openapi = read("contracts/openapi/admin-v1.yaml");
  const generated = read("admin/packages/api-client/src/generated/admin-v1.ts");
  const handler = read("server/internal/transport/http/iam/handler.go");
  const service = read("server/internal/application/iam/service.go");

  assert.match(openapi, /\/api\/admin\/v1\/iam\/components:/);
  assert.match(openapi, /operationId: listIAMComponents/);
  assert.match(openapi, /IAMComponent:/);
  assert.match(openapi, /组件注册表/);
  assert.match(generated, /listIAMComponents: '\/admin\/v1\/iam\/components'/);
  assert.match(handler, /GET\("\/components", handler\.listComponents\)/);
  assert.match(handler, /listComponents/);
  assert.match(service, /ListComponents/);
  assert.match(service, /ComponentRegistry/);
});
