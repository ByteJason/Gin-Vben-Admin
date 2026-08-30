import { globSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, it } from 'vitest';

const appRoot = resolve(import.meta.dirname, '..');
const targetFiles = [
  ...globSync('apps/web-*/src/views/**/*.vue', { cwd: appRoot }),
  ...globSync('packages/effects/common-ui/src/{components,ui}/**/*.vue', {
    cwd: appRoot,
  }),
  ...globSync('packages/@core/ui-kit/form-ui/src/components/**/*.vue', {
    cwd: appRoot,
  }),
].filter((file) => !file.endsWith('authentication/icons/slogan.vue'));

function removeComments(source: string) {
  return source
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '');
}

function flatten(value: unknown, prefix = '', output = new Set<string>()) {
  if (value && typeof value === 'object') {
    if (prefix) output.add(prefix);
    for (const [key, child] of Object.entries(value)) {
      flatten(child, prefix ? `${prefix}.${key}` : key, output);
    }
  } else if (prefix) {
    output.add(prefix);
  }
  return output;
}

function localeKeys(language: string) {
  const keys = new Set<string>();
  for (const file of globSync(`packages/locales/src/langs/${language}/*.json`, {
    cwd: appRoot,
  })) {
    const prefix = file
      .split('/')
      .at(-1)!
      .replace(/\.json$/, '');
    const shared = new Set<string>();
    flatten(
      JSON.parse(readFileSync(resolve(appRoot, file), 'utf8')),
      '',
      shared,
    );
    for (const key of shared) keys.add(`${prefix}.${key}`);
  }
  for (const file of globSync(
    `apps/web-antd/src/locales/langs/${language}/*.json`,
    {
      cwd: appRoot,
    },
  )) {
    const prefix = file
      .split('/')
      .at(-1)!
      .replace(/\.json$/, '');
    const local = new Set<string>();
    flatten(
      JSON.parse(readFileSync(resolve(appRoot, file), 'utf8')),
      '',
      local,
    );
    for (const key of local) keys.add(`${prefix}.${key}`);
  }
  return keys;
}

describe('UI locale contract', () => {
  it('does not ship hardcoded CJK copy in operational views', () => {
    const violations = targetFiles.flatMap((file) => {
      const source = removeComments(
        readFileSync(resolve(appRoot, file), 'utf8'),
      );
      return /\p{Script=Han}/u.test(source) ? [file] : [];
    });

    expect(violations).toEqual([]);
  });

  it('defines every literal translation key in both supported languages', () => {
    const used = new Set<string>();
    for (const file of globSync('apps/web-*/src/**/*.{vue,ts,tsx}', {
      cwd: appRoot,
    })) {
      const source = removeComments(
        readFileSync(resolve(appRoot, file), 'utf8'),
      );
      for (const match of source.matchAll(/\$t\(\s*['"]([^'"]+)['"]/g)) {
        used.add(match[1]);
      }
    }
    const missing = [...used].flatMap((key) =>
      ['zh-CN', 'en-US']
        .filter((language) => !localeKeys(language).has(key))
        .map((language) => `${language}:${key}`),
    );
    expect(missing).toEqual([]);
  });
});
