import { describe, expect, it } from 'vitest';

import { getDefaultPwaOptions } from './options';

describe('getDefaultPwaOptions', () => {
  it('uses the Gin Vben Admin brand and local installable app icons', () => {
    expect(getDefaultPwaOptions('Gin Vben Admin').manifest).toMatchObject({
      description:
        'Gin Vben Admin is a modern admin dashboard based on Gin and Vue 3.',
      icons: [
        {
          sizes: '192x192',
          src: 'gin-vben-admin-logo-192.png',
          type: 'image/png',
        },
        {
          sizes: '512x512',
          src: 'gin-vben-admin-logo-512.png',
          type: 'image/png',
        },
      ],
    });
  });
});
