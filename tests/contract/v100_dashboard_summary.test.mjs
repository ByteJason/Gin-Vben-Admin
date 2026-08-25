import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('dashboard summary is generated from the permission-aligned OpenAPI contract', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const generator = read('scripts/generate-openapi.mjs');
  const generated = read('admin/packages/api-client/src/generated/admin-v1.ts');
  const permissionCatalog = read('server/internal/application/iam/production_catalog.go');

  assert.match(openapi, /\/api\/admin\/v1\/dashboard\/summary:[\s\S]*operationId: getDashboardSummary/);
  for (const schema of ['DashboardSummary', 'DashboardCounts', 'DashboardInstanceMetric', 'DashboardHealth']) {
    assert.match(openapi, new RegExp(`    ${schema}:`));
  }
  assert.match(generator, /export interface DashboardSummary/);
  assert.match(generated, /getDashboardSummary: '\/admin\/v1\/dashboard\/summary'/);
  assert.match(generated, /export interface DashboardSummary/);
  assert.match(permissionCatalog, /ID: "dashboard:overview:read"[\s\S]*Path: "\/api\/admin\/v1\/dashboard\/summary"/);
});

test('self-service dashboard and monitor contracts declare their real error surface', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const pathBlock = (path) => {
    const marker = `  ${path}:`;
    const start = openapi.indexOf(marker);
    assert.notEqual(start, -1, `missing ${path}`);
    const following = openapi.slice(start + marker.length);
    const nextPath = following.search(/\n  \/[^\n]+:/);
    return nextPath === -1 ? following : following.slice(0, nextPath);
  };
  const expectResponses = (path, statuses) => {
    const block = pathBlock(path);
    for (const status of statuses) {
      assert.match(block, new RegExp(`\\n        '${status}':`), `${path} must declare ${status}`);
    }
  };

  expectResponses('/api/admin/v1/auth/codes', [400, 401, 403, 500, 503]);
  expectResponses('/api/admin/v1/iam/me', [400, 401, 403, 500, 503]);
  expectResponses('/api/admin/v1/menu/all', [400, 401, 403, 500, 503]);
  expectResponses('/api/admin/v1/dashboard/summary', [400, 401, 403, 503]);
  expectResponses('/api/admin/v1/ops/monitor', [400, 401, 403, 503]);
  assert.match(openapi, /    InternalServerError:[\s\S]*const: 50000/);
});
