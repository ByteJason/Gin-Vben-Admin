import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('scheduled tasks live under system management without changing permission or menu identity', () => {
  const catalog = read('server/internal/application/iam/production_catalog.go');
  assert.match(catalog, /ID: "menu-operations-tasks", ParentID: "menu-system-config", Name: "定时任务", Path: "\/system\/tasks"/);
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const routes = read(`admin/apps/${ui}/src/router/routes/modules/system.ts`);
    const systemSection = routes.slice(routes.indexOf("name: 'menu-system-config'"), routes.indexOf("name: 'menu-operations',"));
    assert.match(systemSection, /'menu-operations-tasks'[\s\S]*?'tasks'[\s\S]*?'ops:tasks:read'/);
    assert.match(routes, /\['\/ops\/tasks', '\/system\/tasks', 'LegacySystemTasks'\]/);
  }
});
