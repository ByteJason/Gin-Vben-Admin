import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const breakpoints = [375, 768, 1024, 1440];

test.beforeEach(async ({ page }) => {
  await page.route('**/api/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    const data = pathname.endsWith('/status')
      ? {
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
  });
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

  const audit = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'])
    .analyze();
  const serious = audit.violations
    .filter(({ impact }) => impact === 'critical' || impact === 'serious')
    .map(({ help, id, impact, nodes }) => ({
      help,
      html: nodes.map((node) => node.html),
      id,
      impact,
      targets: nodes.map((node) => node.target),
    }));
  expect(serious).toEqual([]);
});
