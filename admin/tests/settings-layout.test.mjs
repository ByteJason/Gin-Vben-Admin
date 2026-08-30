import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test } from 'node:test';

const root = resolve(import.meta.dirname, '..');
const apps = ['web-antd', 'web-ele', 'web-naive'];

function settingsSource(app) {
  return readFileSync(
    resolve(root, 'apps', app, 'src/views/system/settings/index.vue'),
    'utf8',
  );
}

test('settings center uses grouped form cards instead of a data table', () => {
  for (const app of apps) {
    const source = settingsSource(app);
    assert.doesNotMatch(source, /<table(?:\s|>)/, `${app} table layout`);
    assert.match(source, /groupedDefinitions/, `${app} grouped data`);
    assert.match(source, /class="settings-groups"/, `${app} group spacing`);
    assert.match(source, /class="category-panel"/, `${app} category panel`);
    assert.match(source, /class="key-tag"/, `${app} key tag`);
    assert.match(source, /role="switch"/, `${app} boolean switch`);
  }
});

test('all categories remain separated and the three templates stay equivalent', () => {
  const sources = apps.map(settingsSource);
  assert.match(sources[0], /'other',/);
  assert.match(sources[0], /gap:\s*20px;/);
  assert.equal(sources[1], sources[0]);
  assert.equal(sources[2], sources[0]);
});
