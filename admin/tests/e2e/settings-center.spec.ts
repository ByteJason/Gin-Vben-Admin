import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const definitions = [
  {
    category: 'security',
    default: 'false',
    description: 'Cookie transport policy',
    envKey: 'AUTH_SECURE_COOKIE',
    key: 'security.secure_cookie',
    kind: 'bool',
    restartRequired: true,
    sensitive: false,
  },
  {
    category: 'security',
    default: '"30m"',
    description: 'Access token lifetime',
    key: 'security.access_ttl',
    kind: 'string',
    restartRequired: true,
    sensitive: false,
  },
  {
    allowed: ['local', 's3'],
    category: 'file',
    default: '"local"',
    description: 'Storage provider',
    key: 'file.provider',
    kind: 'string',
    restartRequired: true,
    sensitive: false,
  },
  {
    category: 'captcha',
    default: '3',
    description: 'Risk threshold',
    key: 'captcha.risk_threshold',
    kind: 'number',
    restartRequired: true,
    sensitive: false,
  },
];

const settingValues = new Map([
  ['security.secure_cookie', 'false'],
  ['security.access_ttl', '"30m"'],
  ['file.provider', '"local"'],
  ['captcha.risk_threshold', '3'],
]);

function envelope(data: unknown, code = 0, message = 'success') {
  return JSON.stringify({ code, data, message });
}

test.beforeEach(async ({ page }, testInfo) => {
  if (testInfo.project.name === 'installer') testInfo.skip();

  await page.route('**/api/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    if (pathname.endsWith('/admin/v1/auth/login')) {
      await route.fulfill({
        body: envelope({
          accessToken: 'settings-e2e-token',
          expiresIn: 3600,
          tokenType: 'Bearer',
        }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/iam/me')) {
      await route.fulfill({
        body: envelope({
          accessCodes: ['system:settings:read', 'system:settings:manage'],
          homePath: '/system/settings',
          id: 'settings-fixture-user',
          realName: 'Settings Fixture',
          roles: ['super'],
          username: 'settings-fixture',
        }),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    if (pathname.endsWith('/menu/all')) {
      await route.fulfill({
        body: envelope([]),
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
    if (pathname.endsWith('/admin/v1/settings')) {
      await route.fulfill({
        body: envelope(definitions),
        contentType: 'application/json',
        status: 200,
      });
      return;
    }
    const match = pathname.match(/\/admin\/v1\/settings\/([^/]+)$/);
    if (match) {
      const key = decodeURIComponent(match[1] ?? '');
      const definition = definitions.find((item) => item.key === key);
      await route.fulfill({
        body: envelope({
          category: definition?.category ?? 'other',
          key,
          restartRequired: definition?.restartRequired ?? false,
          sensitive: definition?.sensitive ?? false,
          source: 'default',
          value: settingValues.get(key) ?? 'null',
          version: 0,
        }),
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

  // Authenticate through the public UI instead of depending on the persisted
  // store's production encryption format. This also exercises session bootstrap.
  await page.goto('/auth/login');
  await page.getByRole('textbox').first().fill('settings-fixture');
  await page.locator('input[type="password"]').fill('fixture-password');
  await page.getByRole('button', { name: 'login' }).click();
  await expect(page).toHaveURL(/\/system\/settings$/);
});

test('settings use separated form groups, key tags, switches and responsive layout', async ({
  page,
}, testInfo) => {
  await page.goto('/system/settings', { waitUntil: 'networkidle' });

  await expect(page.locator('.settings-page')).toHaveCount(1);
  await expect(page.locator('table')).toHaveCount(0);
  await expect(page.locator('.category-panel')).toHaveCount(3);
  await expect(page.locator('.key-tag')).toHaveCount(4);
  await expect(page.getByRole('switch')).toHaveAttribute(
    'aria-checked',
    'false',
  );
  await expect(page.locator('.settings-groups')).toHaveCSS('gap', '20px');

  await page
    .getByRole('button', { name: /安全|Security/ })
    .first()
    .click();
  await expect(page.locator('.category-panel')).toHaveCount(1);
  await expect(page.locator('.key-tag')).toHaveCount(2);

  for (const width of [375, 768, 1440, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(
      overflow,
      `${testInfo.project.name} settings overflow at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }

  const audit = await new AxeBuilder({ page })
    .include('.settings-page')
    .withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'])
    .analyze();
  expect(
    audit.violations.filter(
      ({ impact }) => impact === 'critical' || impact === 'serious',
    ),
  ).toEqual([]);
});
