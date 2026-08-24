import { describe, expect, it } from 'vitest';

import { withViteAppTitleDefault } from './env';

describe('withViteAppTitleDefault', () => {
  it('publishes a stable title for Vite HTML and import.meta.env when files omit it', () => {
    const runtimeEnv: Record<string, string | undefined> = {};

    expect(withViteAppTitleDefault({}, runtimeEnv)).toEqual({
      VITE_APP_TITLE: 'Gin Vben Admin',
    });
    expect(runtimeEnv.VITE_APP_TITLE).toBe('Gin Vben Admin');
  });

  it('preserves a title configured in a local env file', () => {
    const runtimeEnv: Record<string, string | undefined> = {};

    expect(
      withViteAppTitleDefault({ VITE_APP_TITLE: 'Local Admin' }, runtimeEnv),
    ).toEqual({ VITE_APP_TITLE: 'Local Admin' });
  });

  it('gives an explicitly exported process value highest precedence', () => {
    const runtimeEnv = { VITE_APP_TITLE: 'Process Admin' };

    expect(
      withViteAppTitleDefault({ VITE_APP_TITLE: 'Local Admin' }, runtimeEnv),
    ).toEqual({ VITE_APP_TITLE: 'Process Admin' });
  });

  it('does not let an injected fallback mask a later local env edit', () => {
    const runtimeEnv: Record<string, string | undefined> = {};

    expect(withViteAppTitleDefault({}, runtimeEnv).VITE_APP_TITLE).toBe(
      'Gin Vben Admin',
    );
    expect(
      withViteAppTitleDefault(
        { VITE_APP_TITLE: 'Edited Local Admin' },
        runtimeEnv,
      ).VITE_APP_TITLE,
    ).toBe('Edited Local Admin');
  });
});
