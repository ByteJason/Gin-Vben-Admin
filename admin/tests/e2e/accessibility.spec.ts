import AxeBuilder from '@axe-core/playwright';
import type { Page, Route } from '@playwright/test';
import { expect, test } from '@playwright/test';

const breakpoints = [320, 375, 768, 1024, 1440];

async function fulfillInstallerAPI(route: Route, installed = false) {
  const pathname = new URL(route.request().url()).pathname;
  const data = pathname.endsWith('/status')
    ? installed
      ? {
          installed: true,
          installerVersion: '0.4.0-dev',
          mode: 'dev',
          selectedUi: 'ele',
          state: 'installed',
        }
      : {
          installed: false,
          installerVersion: '0.4.0-dev',
          schemaVersion: 1,
          selectedUi: 'naive',
          state: 'ui_prepared',
        }
    : pathname.endsWith('/capabilities')
      ? {
          platform: { arch: 'test', os: 'test' },
          tools: [{ available: true, id: 'go', version: 'test' }],
        }
      : null;
  await route.fulfill({
    body: JSON.stringify({
      code: data ? 0 : 30000,
      data,
      message: data ? 'success' : 'not found',
    }),
    contentType: 'application/json',
    status: data ? 200 : 404,
  });
}

async function seriousAxeViolations(page: Page) {
  const audit = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'])
    .analyze();
  return audit.violations
    .filter(({ impact }) => impact === 'critical' || impact === 'serious')
    .map(({ help, id, impact, nodes }) => ({
      help,
      html: nodes.map((node) => node.html),
      id,
      impact,
      targets: nodes.map((node) => node.target),
    }));
}

test.beforeEach(async ({ page }) => {
  await page.route('**/api/**', async (route) => fulfillInstallerAPI(route));
});

test('critical page is keyboard reachable, responsive, and axe clean', async ({
  page,
}, testInfo) => {
  const installer = testInfo.project.name === 'installer';
  await page.goto(installer ? '/install' : '/auth/login', {
    waitUntil: 'networkidle',
  });
  if (installer) {
    await expect(page.locator('#status-title')).toHaveText('安装服务已就绪');
    await expect(page.locator('#selection-panel')).toBeVisible();
    await expect(page.locator('#selected-ui-summary')).toHaveText('Naive UI');
    await expect(page.locator('#ui-choice')).toHaveCount(0);
  } else {
    await expect(page.locator('input')).not.toHaveCount(0);
  }

  for (const width of breakpoints) {
    await page.setViewportSize({ height: 900, width });
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(
      overflow,
      `${testInfo.project.name} horizontal overflow at ${width}px`,
    ).toBeLessThanOrEqual(1);
    await page.screenshot({
      fullPage: true,
      path: testInfo.outputPath(`${width}.png`),
    });
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
      tag: active.tagName,
    };
  });
  expect(focus, `${testInfo.project.name} keyboard focus`).not.toBeNull();
  expect(
    focus?.outline !== 'none' || (focus?.shadow && focus.shadow !== 'none'),
    `${testInfo.project.name} visible focus`,
  ).toBeTruthy();

  expect(await seriousAxeViolations(page)).toEqual([]);
});

test('installed quick start stacks both terminal cards without overflow at 320px', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== 'installer');
  await page.unroute('**/api/**');
  await page.route('**/api/**', async (route) =>
    fulfillInstallerAPI(route, true),
  );
  await page.setViewportSize({ height: 900, width: 320 });
  await page.goto('/install', { waitUntil: 'networkidle' });

  await expect(page.locator('#next-steps')).toBeVisible();
  await expect(page.locator('.terminal-card')).toHaveCount(2);
  await expect(page.locator('.terminal-card').nth(0)).toContainText(
    'go run ./cmd/api/main.go',
  );
  await expect(page.locator('.terminal-card').nth(1)).toContainText(
    'pnpm install',
  );
  await expect(page.locator('.terminal-card').nth(1)).toContainText(
    'pnpm run dev',
  );

  const first = await page.locator('.terminal-card').nth(0).boundingBox();
  const second = await page.locator('.terminal-card').nth(1).boundingBox();
  expect(first).not.toBeNull();
  expect(second).not.toBeNull();
  expect(second!.y).toBeGreaterThanOrEqual(first!.y + first!.height - 1);

  const overflow = await page.evaluate(() => ({
    cards: [...document.querySelectorAll('.terminal-card')].map(
      (card) => card.scrollWidth - card.clientWidth,
    ),
    document: document.documentElement.scrollWidth - window.innerWidth,
  }));
  expect(overflow.document).toBeLessThanOrEqual(1);
  expect(overflow.cards.every((value) => value <= 1)).toBeTruthy();
  expect(await seriousAxeViolations(page)).toEqual([]);
});
