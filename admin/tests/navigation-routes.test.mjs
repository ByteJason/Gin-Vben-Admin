import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const apps = ['web-antd', 'web-ele', 'web-naive'];
const route = (app, file) =>
  fs.readFileSync(
    new URL(
      `../apps/${app}/src/router/routes/modules/${file}`,
      import.meta.url,
    ),
    'utf8',
  );

test('B1.7c exposes the same five production navigation groups in every UI', () => {
  for (const app of apps) {
    const modules = ['dashboard.ts', 'iam.ts', 'system.ts', 'media.ts']
      .map((f) => route(app, f))
      .join('\n');
    for (const path of ['/dashboard', '/ops', '/iam', '/system', '/media'])
      assert.match(
        modules,
        new RegExp(`path: ['"]${path}['"]`),
        `${app}: ${path}`,
      );
    const dashboardSource = route(app, 'dashboard.ts');
    assert.match(dashboardSource, /path:\s*['"]\/dashboard['"]/);
    assert.match(
      dashboardSource,
      /component:\s*\(\)\s*=>\s*import\(['"]#\/views\/dashboard\/analytics\/index\.vue['"]\)/,
    );
    assert.doesNotMatch(route(app, 'dashboard.ts'), /menu-overview-runtime/);
    assert.match(modules, /\/ops\/server-status|path: ['"]server-status['"]/);
    assert.match(
      modules,
      /\/ops\/operation-history|path: ['"]operation-history['"]/,
    );
    assert.match(modules, /\/ops\/login-logs|path: ['"]login-logs['"]/);
    assert.match(modules, /\/media\/library|path: ['"]library['"]/);
    assert.match(route(app, 'demos.ts'), /export default \[\];/);
    assert.doesNotMatch(
      route(app, 'vben.ts'),
      /VbenProject|VbenDocument|VbenGithub/,
    );
  }
});

test('B1.7c keeps legacy deep links as hidden redirects', () => {
  for (const app of apps) {
    const modules = ['dashboard.ts', 'system.ts', 'media.ts']
      .map((f) => route(app, f))
      .join('\n');
    for (const path of [
      '/analytics',
      '/dashboard/analytics',
      '/workspace',
      '/dashboard/workspace',
      '/system/monitor',
      '/system/audit',
      '/system/tasks',
      '/system/files',
    ])
      assert.match(
        modules,
        new RegExp(`['"]${path.replaceAll('/', '\\/')}['"]`),
        `${app}: ${path}`,
      );
  }
});
