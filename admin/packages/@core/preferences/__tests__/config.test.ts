import { describe, expect, it } from 'vitest';

import { brandLogoUrl, defaultPreferences } from '../src/config';

describe('defaultPreferences immutability test', () => {
  it('uses the Gin Vben Admin product branding by default', () => {
    expect(defaultPreferences.app.name).toBe('Gin Vben Admin');
    expect(defaultPreferences.copyright.companyName).toBe('Gin Vben Admin');
    expect(brandLogoUrl).toBe(
      `${import.meta.env.BASE_URL}gin-vben-admin-logo.png`,
    );
    expect(defaultPreferences.logo.source).toBe(brandLogoUrl);
  });

  // 创建快照，确保默认配置对象不被修改
  it('should not modify the config object', () => {
    expect(defaultPreferences).toMatchSnapshot();
  });
});
