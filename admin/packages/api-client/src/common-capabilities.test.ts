import { describe, expect, it } from 'vitest';

import { normalizeMediaSelections } from './common-capabilities';

describe('normalizeMediaSelections', () => {
  it('keeps stable order for ties and rewrites contiguous positions', () => {
    expect(
      normalizeMediaSelections([
        { resourceId: ' gallery ', sortOrder: 4 },
        { resourceId: 'cover', sortOrder: 1, role: 'cover' },
        { resourceId: 'detail', sortOrder: 1 },
      ]),
    ).toEqual([
      { resourceId: 'cover', sortOrder: 0, role: 'cover' },
      { resourceId: 'detail', sortOrder: 1, role: 'gallery' },
      { resourceId: 'gallery', sortOrder: 2, role: 'gallery' },
    ]);
  });

  it.each([
    ['duplicate resource', [{ resourceId: 'res-1' }, { resourceId: 'res-1' }]],
    [
      'multiple covers',
      [
        { resourceId: 'res-1', role: 'cover' },
        { resourceId: 'res-2', role: 'cover' },
      ],
    ],
  ])('rejects %s', (_name, input) => {
    expect(() => normalizeMediaSelections(input)).toThrow();
  });
});
