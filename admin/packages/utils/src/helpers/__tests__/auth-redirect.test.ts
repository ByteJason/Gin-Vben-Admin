import { describe, expect, it } from 'vitest';

import { resolveAuthRedirect } from '../auth-redirect';

describe('resolveAuthRedirect', () => {
  const options = {
    fallback: '/dashboard/analytics',
    loginPath: '/auth/login',
  };

  it('unwraps the repeatedly encoded login redirect reported after expiry', () => {
    expect(
      resolveAuthRedirect(
        '%252Fauth%252Flogin%253Fredirect%253D%2525252Fsystem%2525252Fmail',
        options,
      ),
    ).toBe('/system/mail');
  });

  it('keeps a local destination stable across repeated logout redirects', () => {
    expect(resolveAuthRedirect('/system/mail?tab=accounts', options)).toBe(
      '/system/mail?tab=accounts',
    );
    expect(
      resolveAuthRedirect(
        '/auth/login?redirect=%252Fsystem%252Fmail%253Ftab%253Daccounts',
        options,
      ),
    ).toBe('/system/mail?tab=accounts');
  });

  it('falls back for external, malformed, or login-only destinations', () => {
    expect(resolveAuthRedirect('https://example.test/steal', options)).toBe(
      options.fallback,
    );
    expect(resolveAuthRedirect('//example.test/steal', options)).toBe(
      options.fallback,
    );
    expect(resolveAuthRedirect('/\\example.test/steal', options)).toBe(
      options.fallback,
    );
    expect(resolveAuthRedirect('%E0%A4%A', options)).toBe(options.fallback);
    expect(resolveAuthRedirect('/auth/login', options)).toBe(options.fallback);
  });
});
