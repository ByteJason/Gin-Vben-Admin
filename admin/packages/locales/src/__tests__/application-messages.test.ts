import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, it } from 'vitest';
import { createI18n } from 'vue-i18n';

const apps = resolve(import.meta.dirname, '../../../../apps');

function flatten(
  value: Record<string, unknown>,
  prefix = '',
): [string, string][] {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof child === 'string'
      ? [[path, child] as [string, string]]
      : flatten(child as Record<string, unknown>, path);
  });
}

describe.each(['web-antd', 'web-ele', 'web-naive'])(
  '%s application messages',
  (app) => {
    const directory = resolve(apps, app, 'src/locales/langs/zh-CN');
    const messages = Object.fromEntries(
      readdirSync(directory)
        .filter((file) => file.endsWith('.json'))
        .map((file) => [
          file.slice(0, -5),
          JSON.parse(readFileSync(resolve(directory, file), 'utf8')),
        ]),
    );
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': messages },
      warnHtmlMessage: false,
    });

    it.each(flatten(messages))(
      'renders %s without aborting the page',
      (key, message) => {
        const params = Object.fromEntries(
          [...message.matchAll(/\{(\w+)\}/g)].map((match) => [match[1], 2]),
        );
        expect(() => i18n.global.t(key, params)).not.toThrow();
      },
    );

    it('interpolates fresh empty-page counters without legacy braces', () => {
      for (const [key, message] of flatten(messages).filter(([, value]) =>
        value.includes('count'),
      )) {
        const value = i18n.global.t(key, {
          count: 0,
          bytes: '0 B',
          cutoff: '2026-09-10',
        });
        expect(value, key).not.toMatch(/\{\{?count\}\}?/);
        expect(value, `${key}: ${message}`).toContain('0');
      }
    });

    it('preserves literal JSON and mail-template examples', () => {
      expect(i18n.global.t('page.dictionary.importPlaceholder')).toBe(
        '[{"value":"example","labelZhCN":"示例","labelEnUS":"Example"}]',
      );
      expect(i18n.global.t('page.mail.templateBodyFormatHint')).toBe(
        'HTML 模式会按 HTML 邮件发送，变量仍使用 {{.variable}}。',
      );
    });
  },
);
