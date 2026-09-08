import type { Locale } from 'ant-design-vue/es/locale';

import type { App } from 'vue';

import type { LocaleSetupOptions } from '@vben/locales';

import { ref } from 'vue';

import {
  $t,
  setupI18n as coreSetup,
  loadLocalesMapFromDir,
} from '@vben/locales';

import antdDefaultLocale from 'ant-design-vue/es/locale/zh_CN';
import dayjs from 'dayjs';

const antdLocale = ref<Locale>(antdDefaultLocale);

const modules = import.meta.glob('./langs/**/*.json');

const localesMap = loadLocalesMapFromDir(
  /\.\/langs\/([^/]+)\/(.*)\.json$/,
  modules,
);
/**
 * 加载应用特有的语言包
 * 这里也可以改造为从服务端获取翻译数据
 * @param lang
 */
async function loadMessages() {
  const [appLocaleMessages] = await Promise.all([
    localesMap['zh-CN']?.(),
    loadThirdPartyMessage(),
  ]);
  return appLocaleMessages?.default;
}

/**
 * 加载第三方组件库的语言包
 * @param lang
 */
async function loadThirdPartyMessage() {
  await loadDayjsLocale();
}

/**
 * 加载dayjs的语言包
 * @param lang
 */
async function loadDayjsLocale() {
  const locale = await import('dayjs/locale/zh-cn');
  if (locale) {
    dayjs.locale(locale);
  } else {
    console.error('加载中文日期语言包失败');
  }
}

/**
 * 加载antd的语言包
 * @param lang
 */
async function setupI18n(app: App, options: LocaleSetupOptions = {}) {
  await coreSetup(app, {
    defaultLocale: 'zh-CN',
    loadMessages,
    missingWarn: !import.meta.env.PROD,
    ...options,
  });
}

export { $t, antdLocale, setupI18n };
