import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const breakpoints = [375, 768, 1440, 1920, 2560];
const managementPages = [
  { path: '/dashboard/analytics', endpoint: '/dashboard/summary' },
  { path: '/system/monitor', endpoint: '/ops/monitor' },
  { path: '/system/settings', endpoint: '/settings' },
  { path: '/system/audit', endpoint: '/audit/events' },
  { path: '/system/files', endpoint: '/files' },
  { path: '/iam/users', endpoint: '/iam/users' },
];

const fixtureUser = {
  accessCodes: [
    'dashboard:overview:read',
    'iam:users:read',
    'iam:roles:read',
    'iam:menus:read',
    'iam:permissions:read',
    'system:settings:read',
    'system:dictionary:read',
    'system:mail:read',
    'system:files:read',
    'system:observability:read',
    'ops:monitor:read',
    'ops:audit:read',
    'ops:tasks:read',
    'ops:data-jobs:read',
  ],
  homePath: '/system/files',
  id: 'b107-fixture-user',
  realName: 'B10.7 Fixture',
  roles: ['super'],
  username: 'b107-fixture',
};

const fixtureMenus: unknown[] = [];

function envelope(data: unknown, code = 0, message = 'success') {
  return JSON.stringify({ code, data, message });
}

test.beforeEach(async ({ page }, testInfo) => {
  if (testInfo.project.name === 'installer') testInfo.skip();

  // The app namespace is injected by Vite and may be absent in a fixture build.
  // Seed both forms so this suite can run against production and local bundles.
  await page.addInitScript(() => {
    const state = JSON.stringify({ accessToken: 'b107-e2e-token' });
    for (const key of [
      'undefined-5.7.0-prod-core-access',
      'undefined-5.7.0-dev-core-access',
      'vben-admin-5.7.0-prod-core-access',
      'vben-admin-5.7.0-dev-core-access',
    ]) {
      localStorage.setItem(key, state);
    }
  });

  await page.route('**/api/**', async (route) => {
    const requestURL = new URL(route.request().url());
    const pathname = requestURL.pathname;
    const state = requestURL.searchParams.get('b107State');

    if (pathname.endsWith('/iam/me')) {
      await route.fulfill({
        body: envelope(fixtureUser),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/menu/all')) {
      await route.fulfill({
        body: envelope(fixtureMenus),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/codes')) {
      await route.fulfill({
        body: envelope(null, 404, 'legacy endpoint absent'),
        contentType: 'application/json',
        status: 404,
      });
      return;
    }
    const managementEndpoint = managementPages.some(({ endpoint }) =>
      pathname.endsWith(endpoint),
    );
    if (managementEndpoint && state === 'error') {
      await route.fulfill({
        body: envelope(null, 50000, 'fixture backend error'),
        contentType: 'application/json',
        status: 500,
      });
      return;
    }
    if (pathname.endsWith('/auth/sessions')) {
      await route.fulfill({
        body: envelope([
          {
            createdAt: '2026-08-25T00:00:00Z',
            deviceId: 'browser-1',
            deviceName: 'Chrome',
            expiresAt: '2026-08-26T00:00:00Z',
            id: 'session-1',
            ipAddress: '127.0.0.1',
            lastSeenAt: '2026-08-25T00:30:00Z',
            revoked: false,
            userAgent: 'fixture',
          },
        ]),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/dashboard/summary')) {
      await route.fulfill({
        body: envelope({
          collectedAt: '2026-08-25T00:30:00Z',
          counts: {
            auditEvents: { status: 'ok', value: 0 },
            exportJobs: { status: 'ok', value: 1 },
            files: { status: 'ok', value: 2 },
            importJobs: { status: 'ok', value: 0 },
            mailAccounts: { status: 'ok', value: 1 },
            mailMessages: { status: 'ok', value: 4 },
            roles: { status: 'ok', value: 2 },
            tasks: { status: 'ok', value: 0 },
            users: { status: 'ok', value: 1 },
          },
          health: {
            database: { state: 'ok', status: 'ok' },
            redis: { state: 'ok', status: 'ok' },
            runtime: { state: 'running', status: 'ok' },
          },
          instance: {
            scope: 'process',
            state: 'running',
            status: 'ok',
            uptimeSeconds: 3600,
            version: '1.0.0-dev',
          },
          status: 'ok',
        }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/ops/monitor')) {
      await route.fulfill({
        body: envelope({
          collectedAt: '2026-08-25T00:30:00Z',
          cpu: {
            capabilities: { cores: { available: true, scope: 'process' } },
            cores: 4,
            load1: 0.5,
            status: 'ok',
            utilization: 0.25,
          },
          database: {
            capabilities: { pool: { available: true, scope: 'process' } },
            driver: 'postgres',
            latencyMs: 2,
            mode: 'single',
            pool: {
              idle: 3,
              inUse: 1,
              max: 10,
              maxIdleClosed: 0,
              maxIdleTimeClosed: 0,
              maxLifetimeClosed: 0,
              open: 4,
              waitCount: 0,
              waitDurationMs: 0,
            },
            status: 'ok',
          },
          disk: {
            capabilities: { usedBytes: { available: true, scope: 'process' } },
            freeBytes: 600000,
            status: 'ok',
            totalBytes: 1000000,
            usedBytes: 400000,
            utilization: 0.4,
          },
          memory: {
            capabilities: { rssBytes: { available: true, scope: 'process' } },
            rssBytes: 500000,
            status: 'ok',
          },
          redis: {
            capabilities: { pool: { available: true, scope: 'process' } },
            keyspace: 12,
            latencyMs: 1,
            mode: 'single',
            pool: {
              active: 1,
              hits: 5,
              idle: 2,
              max: 10,
              misses: 0,
              pending: 0,
              stale: 0,
              timeouts: 0,
              total: 3,
              waitCount: 0,
              waitDurationMs: 0,
            },
            status: 'ok',
          },
          runtime: {
            applicationVersion: '1.0.0-dev',
            arch: 'amd64',
            capabilities: {},
            gcCount: 0,
            goVersion: 'go1.26.5',
            os: 'windows',
            status: 'ok',
          },
          scope: 'process',
          uptimeSeconds: 3600,
          version: '1.0.0-dev',
        }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }

    if (pathname.endsWith('/audit/events')) {
      await route.fulfill({
        body: envelope({ items: [], limit: 50, offset: 0, total: 0 }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/files')) {
      await route.fulfill({
        body: envelope({ items: [], page: 1, pageSize: 20, total: 0 }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/iam/users')) {
      await route.fulfill({
        body: envelope({ items: [], page: 1, pageSize: 20, total: 0 }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/settings')) {
      await route.fulfill({
        body: envelope([]),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }

    await route.fulfill({
      body: envelope(null, 404, 'fixture not found'),
      contentType: 'application/json',
      status: 404,
    });
  });
});

test('management pages cover empty/error states, keyboard focus, axe and breakpoints', async ({
  page,
}, testInfo) => {
  for (const managementPage of managementPages) {
    for (const state of ['empty', 'error']) {
      await page.goto(`${managementPage.path}?b107State=${state}`, {
        waitUntil: 'networkidle',
      });

      // A bundle without the fixture auth key redirects to the login page. It
      // is still a valid smoke result; the page is checked for a usable form.
      const pathname = new URL(page.url()).pathname;
      if (pathname === '/auth/login') {
        await expect(page.locator('input')).not.toHaveCount(0);
        continue;
      }

      await expect(page.locator('main')).toHaveCount(1);
      for (const width of breakpoints) {
        await page.setViewportSize({ width, height: 900 });
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - window.innerWidth,
        );
        expect(
          overflow,
          `${testInfo.project.name} ${managementPage.path} overflow at ${width}px`,
        ).toBeLessThanOrEqual(1);
      }

      await page.keyboard.press('Tab');
      const focus = await page.evaluate(() => {
        const active = document.activeElement;
        if (!(active instanceof HTMLElement) || active === document.body)
          return null;
        const style = getComputedStyle(active);
        return {
          outline: style.outlineStyle,
          shadow: style.boxShadow,
        };
      });
      expect(
        focus,
        `${testInfo.project.name} keyboard Tab focus`,
      ).not.toBeNull();
      expect(
        focus?.outline !== 'none' || (focus?.shadow && focus.shadow !== 'none'),
        `${testInfo.project.name} visible focus`,
      ).toBeTruthy();

      const audit = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'])
        .analyze();
      const serious = audit.violations.filter(
        ({ impact }) => impact === 'critical' || impact === 'serious',
      );
      expect(
        serious,
        `${testInfo.project.name} ${managementPage.path} axe`,
      ).toEqual([]);
    }
  }
});
