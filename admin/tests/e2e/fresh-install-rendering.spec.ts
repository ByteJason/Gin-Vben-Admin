import { readFileSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';

// Use the actual fresh-install navigation catalog. API responses below are
// empty/error contract fixtures, not a substitute for backend integration tests.
const src = readFileSync(
  new URL(
    '../../../server/internal/application/iam/production_catalog.go',
    import.meta.url,
  ),
  'utf8',
);
const menus: any[] = [];
const byId = new Map<string, any>();
for (const line of src.split('\n')) {
  if (
    !line.includes('ID: "menu-') ||
    !line.includes('Active: true') ||
    !line.includes('Visible: true')
  )
    continue;
  const fields = Object.fromEntries(
    [...line.matchAll(/(\w+): "([^"]*)"/g)].map((x) => [x[1], x[2]]),
  );
  const row = {
    name: fields.ID,
    path: fields.Path,
    component: fields.Component,
    redirect: fields.Redirect,
    meta: {
      title: fields.Name,
      icon: fields.Icon,
      authority: fields.Permission ? [fields.Permission] : undefined,
    },
  };
  byId.set(fields.ID, row);
  if (fields.ParentID) {
    const parent = byId.get(fields.ParentID);
    if (parent) (parent.children ??= []).push(row);
  } else menus.push(row);
}
const codes = [...src.matchAll(/ID: "([\w-]+(?::[\w-]+)+)"/g)].map((m) => m[1]);

const paths = [
  '/dashboard',
  '/ops/server-status',
  '/iam/users',
  '/system/settings',
  '/iam/roles',
  '/iam/menus',
  '/iam/permissions',
  '/system/dictionary',
  '/system/mail',
  '/ops/tasks',
  '/ops/data-jobs',
  '/media/library',
  '/ops/operation-history',
  '/ops/login-logs',
];

async function mockFreshInstallation(page: Page) {
  await page.route('**/api/**', async (route) => {
    const p = new URL(route.request().url()).pathname;
    if (!p.startsWith('/api/')) return route.continue();
    let data;
    let status = 200;
    if (p.endsWith('/auth/login'))
      data = {
        accessToken: 'fixture-browser-token',
        expiresIn: 86400,
        tokenType: 'Bearer',
      };
    else if (p.endsWith('/iam/me'))
      data = {
        accessCodes: codes,
        homePath: '/dashboard',
        id: 'fixture-admin',
        realName: '验收用户',
        roles: ['super'],
        username: 'fixture-admin',
      };
    else if (p.endsWith('/menu/all')) data = menus;
    else if (p.endsWith('/auth/captcha')) data = { enabled: false };
    else if (
      p.endsWith('/auth/sessions') ||
      p.endsWith('/settings/modules') ||
      p.endsWith('/settings/definitions')
    )
      data = [];
    else if (p.endsWith('/install/status'))
      data = { installed: true, state: 'installed' };
    else if (p.includes('/settings/')) data = [];
    else if (p.endsWith('/ops/server-status') || p.endsWith('/ops/monitor')) {
      status = 503;
      data = null;
    } else if (p.endsWith('/dashboard/overview'))
      data = {
        announcements: [],
        cards: Object.fromEntries(
          [
            'averageOrderValue',
            'newUsers',
            'paymentAmount',
            'paymentOrders',
            'visitors',
          ].map((k) => [k, { status: 'ok', value: 0 }]),
        ),
        collectedAt: '2026-09-10T00:00:00Z',
        dataSource: 'live',
        distribution: [],
        isSynthetic: false,
        range: {
          from: '2026-09-10T00:00:00Z',
          to: '2026-09-11T00:00:00Z',
          granularity: 'hour',
          preset: 'today',
          timezone: 'UTC',
        },
        regions: [],
        topItems: [],
        trends: [],
      };
    else if (p.endsWith('/dashboard/summary'))
      data = {
        collectedAt: '2026-09-10T00:00:00Z',
        status: 'ok',
        counts: Object.fromEntries(
          [
            'auditEvents',
            'exportJobs',
            'files',
            'importJobs',
            'mailAccounts',
            'mailMessages',
            'roles',
            'tasks',
            'users',
          ].map((k) => [k, { status: 'ok', value: 0 }]),
        ),
        health: {
          database: { state: 'ok', status: 'ok' },
          redis: { state: 'ok', status: 'ok' },
          runtime: { state: 'running', status: 'ok' },
        },
        instance: {
          scope: 'process',
          state: 'running',
          status: 'ok',
          uptimeSeconds: 60,
          version: 'test',
        },
      };
    else if (
      p.endsWith('/iam/roles') ||
      p.endsWith('/iam/permissions') ||
      p.endsWith('/dictionary/types')
    )
      data = [];
    else if (p.endsWith('/iam/users'))
      data = { items: [], total: 0, page: 1, pageSize: 20 };
    else if (
      p.endsWith('/ops/operation-history') ||
      p.endsWith('/ops/login-logs')
    )
      data = { items: [], total: 0, limit: 20, offset: 0 };
    else {
      status = 503;
      data = null;
    }
    await route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify({
        code: status === 200 ? 0 : 40001,
        data,
        message: status === 200 ? 'success' : 'fixture dependency unavailable',
      }),
    });
  });
}

async function submitLogin(page: Page) {
  await page
    .getByRole('button', { name: /登.*录|^login$/i })
    .first()
    .click();
}

async function fillLogin(page: Page) {
  await page.goto('/auth/login', { waitUntil: 'networkidle' });
  await page.getByRole('textbox').first().fill('fixture-admin');
  await page.locator('input[type=password]').fill('fixture-password');
}

test('fresh installation renders every management page without i18n crashes', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name === 'installer');
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await mockFreshInstallation(page);
  await fillLogin(page);
  await submitLogin(page);
  await expect(page).toHaveURL(/\/dashboard$/);
  for (const path of paths) {
    await page.goto(path, { waitUntil: 'networkidle' });
    // A login redirect is a failed page test, never a successful fallback.
    await expect(page).toHaveURL(new RegExp(`${path}$`));
    const content = page.locator('.management-page').first();
    await expect(content).toBeVisible();
    await expect(content).not.toHaveText(/^\s*$/);
    expect(errors, `${testInfo.project.name} ${path}`).toEqual([]);
  }
});
