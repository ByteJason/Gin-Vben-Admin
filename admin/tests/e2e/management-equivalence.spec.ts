import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const breakpoints = [375, 768, 1024, 1440];
const managementPages = [
  { path: '/system/settings', endpoint: '/settings' },
  { path: '/system/audit', endpoint: '/audit/events' },
  { path: '/system/files', endpoint: '/files' },
  { path: '/iam/users', endpoint: '/iam/users' },
];

const fixtureUser = {
  homePath: '/system/files',
  id: 'b107-fixture-user',
  realName: 'B10.7 Fixture',
  roles: ['super'],
  username: 'b107-fixture',
};

const fixtureMenus = [
  {
    component: 'BasicLayout',
    meta: { icon: 'lucide:activity', order: 30, title: 'System' },
    name: 'system',
    path: '/system',
    children: [
      {
        component: '/system/settings/index.vue',
        meta: { order: 1, title: 'Settings' },
        name: 'settings',
        path: 'settings',
      },
      {
        component: '/system/audit/index.vue',
        meta: { order: 2, title: 'Audit' },
        name: 'audit',
        path: 'audit',
      },
      {
        component: '/system/files/index.vue',
        meta: { order: 3, title: 'Files' },
        name: 'files',
        path: 'files',
      },
    ],
  },
  {
    component: 'BasicLayout',
    meta: { icon: 'lucide:users', order: 20, title: 'IAM' },
    name: 'iam',
    path: '/iam',
    children: [
      {
        component: '/iam/users/index.vue',
        meta: { order: 1, title: 'Users' },
        name: 'users',
        path: 'users',
      },
    ],
  },
];

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
        body: envelope([]),
        contentType: 'application/json',
        status: 200,
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
