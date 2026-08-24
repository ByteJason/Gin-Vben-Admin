import { describe, expect, it } from 'vitest';

import { useAppConfig } from './use-app-config';

describe('useAppConfig', () => {
  it('uses the same-origin API proxy when a fresh clone has no runtime env yet', () => {
    expect(useAppConfig({}, false).apiURL).toBe('/api');
  });

  it('preserves an explicitly configured API URL', () => {
    expect(
      useAppConfig({ VITE_GLOB_API_URL: '/custom-api' }, false).apiURL,
    ).toBe('/custom-api');
  });

  it('uses the same-origin API proxy when production runtime config omits it', () => {
    window._VBEN_ADMIN_PRO_APP_CONF_ = {};

    expect(useAppConfig({}, true).apiURL).toBe('/api');
  });
});
