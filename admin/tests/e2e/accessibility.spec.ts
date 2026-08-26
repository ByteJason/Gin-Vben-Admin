import AxeBuilder from '@axe-core/playwright';
import type { Page, Route } from '@playwright/test';
import { expect, test } from '@playwright/test';

const breakpoints = [320, 375, 768, 1024, 1440];

type InstallerScenario = 'installed' | 'pristine' | 'ui_prepared';

async function fulfillInstallerAPI(
  route: Route,
  scenario: InstallerScenario = 'pristine',
) {
  const pathname = new URL(route.request().url()).pathname;
  const data = pathname.endsWith('/status')
    ? scenario === 'installed'
      ? {
          installed: true,
          installerVersion: '0.4.0-dev',
          mode: 'dev',
          selectedUi: 'ele',
          state: 'installed',
        }
      : scenario === 'ui_prepared'
        ? {
            installed: false,
            installerVersion: '0.4.0-dev',
            schemaVersion: 1,
            selectedUi: 'naive',
            state: 'ui_prepared',
          }
        : {
            installed: false,
            installerVersion: '0.4.0-dev',
            schemaVersion: 1,
            state: 'pristine',
          }
    : pathname.endsWith('/capabilities')
      ? {
          platform: { arch: 'test', os: 'test' },
          tools: [
            { available: true, compatible: true, id: 'go', version: 'test' },
            { available: true, compatible: true, id: 'node', version: 'test' },
            { available: true, compatible: true, id: 'pnpm', version: 'test' },
          ],
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

async function fulfillJSON(route: Route, data: unknown, status = 200) {
  await route.fulfill({
    body: JSON.stringify({ code: 0, data, message: 'success' }),
    contentType: 'application/json',
    status,
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
    await expect(page.locator('#status-title')).toHaveText('选择管理界面');
    await expect(page.locator('#ui-prepare-panel')).toBeVisible();
    await expect(page.locator('#selection-panel')).toBeHidden();
    await expect(page.locator('input[name="selectedUi"]')).toHaveCount(3);
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
    fulfillInstallerAPI(route, 'installed'),
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

test('UI selection prepares one template, prevents duplicate work, and resets before install', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== 'installer');
  await page.unroute('**/api/**');

  let prepared = false;
  let preparePayload: Record<string, unknown> | null = null;
  let resetPayload: Record<string, unknown> | null = null;
  let releasePrepareProgress!: () => void;
  let releaseResetProgress!: () => void;
  const prepareProgressGate = new Promise<void>((resolve) => {
    releasePrepareProgress = resolve;
  });
  const resetProgressGate = new Promise<void>((resolve) => {
    releaseResetProgress = resolve;
  });

  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (pathname.endsWith('/status')) {
      await fulfillJSON(
        route,
        prepared
          ? {
              installed: false,
              installerVersion: '0.4.0-dev',
              schemaVersion: 1,
              selectedUi: 'ele',
              state: 'ui_prepared',
            }
          : {
              installed: false,
              installerVersion: '0.4.0-dev',
              schemaVersion: 1,
              state: 'pristine',
            },
      );
      return;
    }
    if (pathname.endsWith('/capabilities')) {
      await fulfillJSON(route, {
        platform: { arch: 'test', os: 'test' },
        tools: [
          { available: true, compatible: true, id: 'go', version: 'test' },
          { available: true, compatible: true, id: 'node', version: 'test' },
          { available: true, compatible: true, id: 'pnpm', version: 'test' },
        ],
      });
      return;
    }
    if (pathname.endsWith('/ui/prepare')) {
      preparePayload = request.postDataJSON() as Record<string, unknown>;
      await fulfillJSON(
        route,
        {
          action: 'prepare',
          currentStep: 'queued',
          id: 'ui-prepare-job',
          progress: 0,
          selectedUi: 'ele',
          state: 'queued',
        },
        202,
      );
      return;
    }
    if (pathname.endsWith('/ui/progress/ui-prepare-job')) {
      await prepareProgressGate;
      prepared = true;
      await fulfillJSON(route, {
        action: 'prepare',
        currentStep: 'complete',
        id: 'ui-prepare-job',
        progress: 100,
        selectedUi: 'ele',
        state: 'succeeded',
      });
      return;
    }
    if (pathname.endsWith('/ui/reset')) {
      resetPayload = request.postDataJSON() as Record<string, unknown>;
      await fulfillJSON(
        route,
        {
          action: 'reset',
          currentStep: 'queued',
          id: 'ui-reset-job',
          progress: 0,
          state: 'queued',
        },
        202,
      );
      return;
    }
    if (pathname.endsWith('/ui/progress/ui-reset-job')) {
      await resetProgressGate;
      prepared = false;
      await fulfillJSON(route, {
        action: 'reset',
        currentStep: 'complete',
        id: 'ui-reset-job',
        progress: 100,
        state: 'succeeded',
      });
      return;
    }
    await route.fulfill({ status: 404 });
  });

  await page.goto('/install', { waitUntil: 'networkidle' });
  await page.locator('input[value="ele"]').check();
  await page.locator('#confirm-cleanup').check();
  const prepareButton = page.locator('#prepare-ui-button');
  await expect(prepareButton).toBeEnabled();
  await prepareButton.click();
  await expect.poll(() => preparePayload?.selectedUi).toBe('ele');
  expect(preparePayload).toEqual({
    confirmCleanup: true,
    selectedUi: 'ele',
  });
  await expect(prepareButton).toBeDisabled();
  await expect(page.locator('#ui-prepare-result')).toContainText(
    '等待准备界面',
  );

  releasePrepareProgress();
  await expect(page.locator('#selection-panel')).toBeVisible();
  await expect(page.locator('#selected-ui-summary')).toHaveText('Element Plus');

  page.once('dialog', (dialog) => dialog.accept());
  const resetButton = page.locator('#reset-ui-button');
  await resetButton.click();
  await expect.poll(() => resetPayload?.confirmReset).toBe(true);
  expect(resetPayload).toEqual({ confirmReset: true });
  await expect(page.locator('#selection-panel')).toBeHidden();
  await expect(page.locator('#ui-prepare-panel')).toBeVisible();
  await expect(page.locator('#ui-prepare-form')).toBeHidden();
  await expect(page.locator('#ui-prepare-result')).toBeFocused();

  releaseResetProgress();
  await expect(page.locator('#status-title')).toHaveText('选择管理界面');
  await expect(page.locator('#ui-prepare-form')).toBeVisible();
  await expect(page.locator('input[name="selectedUi"]:checked')).toHaveCount(0);
});

test('missing Node.js or pnpm blocks UI preparation regardless of response order', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== 'installer');
  await page.unroute('**/api/**');
  let capabilityCalls = 0;
  await page.route('**/api/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    if (pathname.endsWith('/capabilities')) {
      capabilityCalls += 1;
      await fulfillJSON(route, {
        platform: { arch: 'test', os: 'test' },
        tools:
          capabilityCalls === 1
            ? [{ available: true, compatible: true, id: 'go', version: 'test' }]
            : [
                { available: true, compatible: true, id: 'node', version: 'test' },
                { available: true, compatible: true, id: 'pnpm', version: 'test' },
              ],
      });
      return;
    }
    if (pathname.endsWith('/status')) {
      await new Promise((resolve) => setTimeout(resolve, 80));
      await fulfillJSON(route, {
        installed: false,
        installerVersion: '0.4.0-dev',
        schemaVersion: 1,
        state: 'pristine',
      });
      return;
    }
    await route.fulfill({ status: 404 });
  });

  await page.goto('/install', { waitUntil: 'networkidle' });
  await page.locator('input[value="antd"]').check();
  await page.locator('#confirm-cleanup').check();
  await expect(page.locator('#prepare-ui-button')).toBeDisabled();
  await expect(page.locator('#ui-prepare-result')).toContainText(
    'Node.js ^22.18.0 或 ^24.12.0',
  );
  await expect(page.locator('#ui-prepare-result')).toContainText(
    'pnpm >=11.0.0',
  );
  const retryButton = page.locator('#retry-button');
  await expect(retryButton).toBeVisible();
  await retryButton.click();
  await expect.poll(() => capabilityCalls).toBe(2);
  await expect(page.locator('#status-title')).toHaveText('选择管理界面');
  await page.locator('input[value="antd"]').check();
  await page.locator('#confirm-cleanup').check();
  await expect(page.locator('#prepare-ui-button')).toBeEnabled();
  await expect(page.locator('#ui-prepare-result')).not.toContainText(
    'Node.js 与 pnpm',
  );
});

test('failed UI preflight exposes structured diagnostics without an unrelated log', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== 'installer');
  await page.unroute('**/api/**');
  await page.route('**/api/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    if (pathname.endsWith('/status')) {
      await fulfillJSON(route, {
        installed: false,
        installerVersion: '0.4.0-dev',
        schemaVersion: 1,
        state: 'pristine',
      });
      return;
    }
    if (pathname.endsWith('/capabilities')) {
      await fulfillJSON(route, {
        platform: { arch: 'test', os: 'test' },
        tools: [
          { available: true, compatible: true, id: 'node', version: 'test' },
          { available: true, compatible: true, id: 'pnpm', version: 'test' },
        ],
      });
      return;
    }
    if (pathname.endsWith('/ui/prepare')) {
      await fulfillJSON(
        route,
        {
          action: 'prepare',
          currentStep: 'queued',
          id: 'failed-ui-job',
          progress: 0,
          selectedUi: 'naive',
          state: 'queued',
        },
        202,
      );
      return;
    }
    if (pathname.endsWith('/ui/progress/failed-ui-job')) {
      await fulfillJSON(route, {
        action: 'prepare',
        currentStep: 'failed',
        errorKey: 'ui_preflight_failed',
        failureOperation: 'cross_directory_rename',
        failureReason: 'preflight_failed',
        failureScope: 'ui_backup',
        failureStep: 'preflight',
        id: 'failed-ui-job',
        progress: 10,
        selectedUi: 'naive',
        state: 'failed',
      });
      return;
    }
    await route.fulfill({ status: 404 });
  });

  await page.goto('/install', { waitUntil: 'networkidle' });
  await page.locator('input[value="naive"]').check();
  await page.locator('#confirm-cleanup').check();
  await page.locator('#prepare-ui-button').click();
  await expect(page.locator('#ui-prepare-diagnostics')).toBeVisible();
  await expect(page.locator('#ui-prepare-error-key')).toHaveText(
    'ui_preflight_failed',
  );
  await expect(page.locator('#ui-prepare-job-id')).toHaveText('failed-ui-job');
  await expect(page.locator('#ui-prepare-failure-step')).toHaveText(
    '检查模板与目录',
  );
  await expect(page.locator('#ui-prepare-failure-reason')).toHaveText(
    '目录或文件操作能力预检失败',
  );
  await expect(page.locator('#ui-prepare-failure-operation')).toContainText(
    '跨目录重命名',
  );
  await expect(page.locator('#ui-prepare-log-item')).toBeHidden();
  await expect(page.locator('#ui-prepare-result')).toContainText(
    '目录或文件操作能力预检失败',
  );
  await expect(page.locator('#ui-prepare-result')).not.toContainText(
    '尚未移动或修改管理界面模板',
  );
  await expect(page.locator('#prepare-ui-button')).toBeEnabled();
});

test('refresh resumes an interrupted reset without submitting prepare', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== 'installer');
  await page.unroute('**/api/**');
  let recovering = true;
  let prepareCalls = 0;
  let resetCalls = 0;
  let resetPayload: Record<string, unknown> | null = null;
  let statusCalls = 0;
  let releaseResetProgress!: () => void;
  const resetProgressGate = new Promise<void>((resolve) => {
    releaseResetProgress = resolve;
  });

  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (pathname.endsWith('/status')) {
      statusCalls += 1;
      await fulfillJSON(
        route,
        recovering
          ? {
              installed: false,
              installerVersion: '0.4.0-dev',
              phase: 'ui_prepare',
              schemaVersion: 1,
              selectedUi: 'ele',
              state: 'installing',
              uiAction: 'reset',
            }
          : {
              installed: false,
              installerVersion: '0.4.0-dev',
              schemaVersion: 1,
              state: 'pristine',
            },
      );
      return;
    }
    if (pathname.endsWith('/capabilities')) {
      await fulfillJSON(route, {
        platform: { arch: 'test', os: 'test' },
        tools: [
          { available: true, compatible: true, id: 'node', version: 'test' },
          { available: true, compatible: true, id: 'pnpm', version: 'test' },
        ],
      });
      return;
    }
    if (pathname.endsWith('/ui/prepare')) {
      prepareCalls += 1;
      await route.fulfill({ status: 500 });
      return;
    }
    if (pathname.endsWith('/ui/reset')) {
      resetCalls += 1;
      resetPayload = request.postDataJSON() as Record<string, unknown>;
      await fulfillJSON(
        route,
        {
          action: 'reset',
          currentStep: 'queued',
          id: 'resume-reset-job',
          progress: 0,
          state: 'queued',
        },
        202,
      );
      return;
    }
    if (pathname.endsWith('/ui/progress/resume-reset-job')) {
      await resetProgressGate;
      recovering = false;
      await fulfillJSON(route, {
        action: 'reset',
        currentStep: 'complete',
        id: 'resume-reset-job',
        progress: 100,
        state: 'succeeded',
      });
      return;
    }
    await route.fulfill({ status: 404 });
  });

  await page.goto('/install', { waitUntil: 'networkidle' });
  await expect(page.locator('#status-title')).toHaveText(
    '继续恢复三套界面模板',
  );
  await expect(page.locator('#ui-prepare-form')).toBeHidden();
  const resumeButton = page.locator('#resume-ui-reset-button');
  await expect(resumeButton).toBeVisible();
  await resumeButton.click();
  await expect.poll(() => resetPayload?.confirmReset).toBe(true);
  expect(resetPayload).toEqual({ confirmReset: true });
  expect(prepareCalls).toBe(0);
  expect(resetCalls).toBe(1);
  const retryButton = page.locator('#retry-button');
  await expect(retryButton).toBeDisabled();
  await expect(page.locator('#ui-prepare-result')).toBeFocused();
  await retryButton.dispatchEvent('click');
  await page.waitForTimeout(50);
  expect(statusCalls).toBe(1);
  expect(resetCalls).toBe(1);
  releaseResetProgress();
  await expect(page.locator('#status-title')).toHaveText('选择管理界面');
});
