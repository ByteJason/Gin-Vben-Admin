import assert from 'node:assert/strict';
import test from 'node:test';

import { buildPostinstallArgs } from '../scripts/postinstall.mjs';

test('postinstall limits stub builds to the selected UI dependency closure', () => {
  assert.deepEqual(
    buildPostinstallArgs({
      mode: 'workspace',
      profile: { packageName: '@vben/web-antd' },
    }),
    ['--filter', '@vben/web-antd...', '-r', 'run', '--if-present', 'stub'],
  );
});

test('postinstall keeps full workspace stubs for an unprofiled bootstrap', () => {
  const expected = ['-r', 'run', '--if-present', 'stub'];
  assert.deepEqual(
    buildPostinstallArgs({ mode: 'workspace', profile: null }),
    expected,
  );
  assert.deepEqual(
    buildPostinstallArgs({
      mode: 'legacy',
      profile: { packageName: '@vben/web-antd' },
    }),
    expected,
  );
});
