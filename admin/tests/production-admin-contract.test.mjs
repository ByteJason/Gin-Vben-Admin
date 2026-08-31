import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const root = new URL('../', import.meta.url);
const templates = ['web-antd', 'web-ele', 'web-naive'];

function read(path) {
  return readFileSync(new URL(path, root), 'utf8');
}

const managementPages = [
  ['dashboard/analytics', 'analytics-overview-page'],
  ['iam/users', 'iam-users-page'],
  ['iam/roles', 'iam-roles-page'],
  ['iam/menus', 'iam-menus-page'],
  ['iam/permissions', 'iam-permissions-page'],
  ['iam/policies', 'iam-policies-page'],
  ['iam/data-scopes', 'iam-data-scopes-page'],
  ['system/observability', 'observability-page'],
  ['system/settings', 'settings-page'],
  ['system/audit', 'audit-page'],
  ['system/files', 'media-library-page'],
  ['system/mail', 'mail-page'],
  ['system/monitor', 'monitor-page'],
  ['system/dictionary', 'dictionary-page'],
  ['system/tasks', 'tasks-page'],
  ['system/import-export', 'import-export-page'],
];

test('production preferences and dashboard routes have one canonical home', () => {
  const preferences = read('packages/@core/preferences/src/config.ts');
  assert.match(preferences, /accessMode:\s*'mixed'/);
  assert.match(preferences, /defaultHomePath:\s*'\/dashboard'/);

  for (const template of templates) {
    const dashboard = read(
      `apps/${template}/src/router/routes/modules/dashboard.ts`,
    );
    assert.match(dashboard, /path:\s*'\/dashboard'/);
    assert.match(
      dashboard,
      /component:\s*\(\)\s*=>\s*import\('#\/views\/dashboard\/analytics\/index\.vue'\)/,
    );
    assert.doesNotMatch(dashboard, /redirect:\s*'\/dashboard\/analytics'/);
    for (const legacy of ['/dashboard/analytics', '/analytics', '/workspace']) {
      assert.ok(
        dashboard.includes(`'${legacy}'`),
        `${template} is missing ${legacy} compatibility route`,
      );
    }
    assert.match(dashboard, /'\/workspace'[\s\S]*?hideInMenu:\s*true/);
    assert.match(dashboard, /'\/workspace'[\s\S]*?redirect:\s*'\/dashboard'/);

    const notFound = read(
      `apps/${template}/src/views/_core/fallback/not-found.vue`,
    );
    assert.match(notFound, /preferences\.app\.defaultHomePath/);
    assert.match(notFound, /userStore\.userInfo\?\.homePath/);
    assert.match(notFound, /:home-path="homePath"/);
  }
});

test('login bootstrap remains atomic and uses generated auth endpoints', () => {
  for (const template of templates) {
    const authStore = read(`apps/${template}/src/store/auth.ts`);
    const authApi = read(`apps/${template}/src/api/core/auth.ts`);

    assert.match(authStore, /fetchUserInfo:\s*getUserInfoApi/);
    assert.match(authStore, /rollback:\s*async\s*\(\)\s*=>/);
    assert.match(authStore, /await\s+logoutApi\(\)/);
    assert.match(authApi, /get<string\[\]>\(AUTH_ENDPOINTS\.codes\)/);

    const loginView = read(
      `apps/${template}/src/views/_core/authentication/login.vue`,
    );
    for (const prop of [
      'show-code-login',
      'show-forget-password',
      'show-qrcode-login',
      'show-register',
      'show-third-party-login',
    ]) {
      assert.ok(
        loginView.includes(`:${prop}="false"`),
        `${template} still exposes ${prop}`,
      );
    }

    const coreRoutes = read(`apps/${template}/src/router/routes/core.ts`);
    assert.match(coreRoutes, /import\.meta\.env\.DEV/);
    assert.match(coreRoutes, /redirect:\s*LOGIN_PATH/);
  }
});

test('three templates expose five production groups and isolate development routes', () => {
  for (const template of templates) {
    const dashboard = read(
      `apps/${template}/src/router/routes/modules/dashboard.ts`,
    );
    const iam = read(`apps/${template}/src/router/routes/modules/iam.ts`);
    const system = read(`apps/${template}/src/router/routes/modules/system.ts`);
    const demos = read(`apps/${template}/src/router/routes/modules/demos.ts`);
    const upstream = read(`apps/${template}/src/router/routes/modules/vben.ts`);
    const layout = read(`apps/${template}/src/layouts/basic.vue`);
    const menus = read(`apps/${template}/src/views/iam/menus/index.vue`);

    assert.match(dashboard, /name:\s*'menu-overview'/);
    assert.match(
      menus,
      /'menu-overview':\s*'page\.navigation\.dashboard'/,
      `${template} menu metadata must use the dashboard label`,
    );
    assert.doesNotMatch(dashboard, /menu-overview-runtime/);
    assert.match(iam, /name:\s*'menu-identity'/);
    assert.match(system, /name:\s*'menu-system-config'/);
    assert.match(system, /name:\s*'menu-operations'/);
    for (const key of ['settings', 'files', 'tasks', 'dataJobs']) {
      assert.ok(
        system.includes(`page.navigation.${key}`),
        `${template} menu does not use navigation.${key}`,
      );
    }
    for (const source of [dashboard, iam, system]) {
      assert.match(source, /authority:\s*\[/);
    }
    assert.match(demos, /export default \[\];/);
    assert.match(upstream, /import\.meta\.env\.DEV/);
    assert.match(upstream, /import\.meta\.env\.DEV/);
    assert.match(upstream, /name:\s*'Profile'[\s\S]*?hideInMenu:\s*true/);
    assert.match(layout, /router\.push\(\{\s*name:\s*'Profile'/);
  }
});

test('dashboard overview uses scoped overview data with a summary fallback', () => {
  for (const template of templates) {
    const analytics = read(
      `apps/${template}/src/views/dashboard/analytics/index.vue`,
    );
    assert.match(analytics, /getDashboardOverviewApi/);
    assert.match(analytics, /getDashboardSummaryApi/);
    assert.match(analytics, /summary-fallback/);
    assert.match(analytics, /requestInFlight/);
    assert.match(analytics, /chart-empty/);
    assert.match(analytics, /label\('trends'\)/);
    assert.match(analytics, /useVisibilityPolling/);
    assert.doesNotMatch(analytics, /ops:monitor:read/);
    assert.doesNotMatch(analytics, /getMonitorOverviewApi|listSessionsApi/);
    assert.doesNotMatch(analytics, /setInterval/);
    assert.doesNotMatch(analytics, /Math\.random|AnalyticsVisits|projectItems/);

    const dashboardApi = read(`apps/${template}/src/api/core/dashboard.ts`);
    assert.match(dashboardApi, /ADMIN_ENDPOINTS\.getDashboardSummary/);

    const monitor = read(`apps/${template}/src/views/system/monitor/index.vue`);
    assert.match(monitor, /useVisibilityPolling/);
    assert.doesNotMatch(monitor, /setInterval/);
    assert.match(monitor, /pool\.max === 0/);
    assert.match(monitor, /pool\.max === 0/);
    for (const field of [
      'cores',
      'load1',
      'load5',
      'load15',
      'rssBytes',
      'usedBytes',
      'freeBytes',
      'totalBytes',
      'utilization',
      'latencyMs',
      'goVersion',
      'heapAllocBytes',
      'waitCount',
      'waitDurationMs',
      'maxIdleClosed',
      'maxIdleTimeClosed',
      'maxLifetimeClosed',
      'inUse',
      'active',
      'hits',
      'misses',
      'timeouts',
      'stale',
      'pending',
      'keyspace',
      'message',
      'capabilities',
      'sessionTrend',
    ]) {
      assert.ok(monitor.includes(field), `${template} monitor omits ${field}`);
    }
  }
});

test('all management views use the shared full-width non-main shell', () => {
  const shell = read(
    'packages/effects/common-ui/src/components/page/management-page.vue',
  );
  assert.match(shell, /class="management-page"/);
  assert.match(shell, /inline-size:\s*100%/);
  assert.match(shell, /min-inline-size:\s*0/);
  assert.match(shell, /padding:\s*clamp\(/);

  for (const template of templates) {
    for (const [page, rootClass] of managementPages) {
      const source = read(`apps/${template}/src/views/${page}/index.vue`);
      assert.match(
        source,
        /<ManagementPage\b/,
        `${template}/${page} is not using ManagementPage`,
      );
      assert.doesNotMatch(
        source,
        /<main\b/i,
        `${template}/${page} nests a main landmark`,
      );
      const rootRule = new RegExp(
        `\\.${rootClass}\\s*\\{[^}]*?(?:max-width|margin\\s*:\\s*0\\s+auto|padding\\s*:)`,
        's',
      );
      assert.doesNotMatch(
        source,
        rootRule,
        `${template}/${page} still constrains the business root`,
      );
    }
  }
});

test('responsive acceptance covers wide desktop as well as mobile', () => {
  const e2e = read('tests/e2e/management-equivalence.spec.ts');
  assert.match(e2e, /\[375, 768, 1440, 1920, 2560\]/);
});
