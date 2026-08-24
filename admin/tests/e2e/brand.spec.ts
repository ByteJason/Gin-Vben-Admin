import { expect, test } from '@playwright/test';

const brandName = 'Gin Vben Admin';
const brandLogoSha256 =
  'a76a68003fdc33d7a112e9683cda3a74603362d372195421b2983e902d44ca07';

test('management login exposes the Gin Vben Admin name and supplied logo', async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name === 'installer');

  await page.route('**/api/**', async (route) => {
    await route.fulfill({
      body: JSON.stringify({
        code: 0,
        data: { enabled: false },
        message: 'success',
      }),
      contentType: 'application/json',
      status: 200,
    });
  });
  await page.goto('/auth/login', { waitUntil: 'networkidle' });

  await expect(page).toHaveTitle(new RegExp(`${brandName}$`));
  await expect(
    page.getByText(brandName, { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText('Vben Admin', { exact: true })).toHaveCount(0);

  const logo = page.getByRole('img', { exact: true, name: brandName }).first();
  await expect(logo).toBeVisible();
  await expect
    .poll(() =>
      logo.evaluate((node) => {
        const image = node as HTMLImageElement;
        return [image.complete, image.naturalWidth, image.naturalHeight];
      }),
    )
    .toEqual([true, 1254, 1254]);

  const digest = await logo.evaluate(async (node) => {
    const response = await fetch((node as HTMLImageElement).currentSrc);
    const bytes = await response.arrayBuffer();
    const hash = await crypto.subtle.digest('SHA-256', bytes);
    return [...new Uint8Array(hash)]
      .map((value) => value.toString(16).padStart(2, '0'))
      .join('');
  });
  expect(digest).toBe(brandLogoSha256);
});
