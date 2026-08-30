<script setup lang="ts">
import type { AboutProps, DescriptionItem } from './about';

import { computed, h } from 'vue';

import {
  GIN_VBEN_ADMIN_GITHUB_URL,
  VBEN_DOC_URL,
  VBEN_GITHUB_URL,
  VBEN_PREVIEW_URL,
} from '@vben/constants';

import { VbenRenderContent } from '@vben-core/shadcn-ui';
import { $t } from '@vben/locales';

import { Page } from '../../components';

interface Props extends AboutProps {}

defineOptions({
  name: 'AboutUI',
});

const props = withDefaults(defineProps<Props>(), {
  description: '',
  name: 'Gin Vben Admin',
  title: '',
});

declare global {
  const __VBEN_ADMIN_METADATA__: {
    authorEmail: string;
    authorName: string;
    authorUrl: string;
    buildTime: string;
    dependencies: Record<string, string>;
    description: string;
    devDependencies: Record<string, string>;
    homepage: string;
    license: string;
    repositoryUrl: string;
    version: string;
  };
}

const renderLink = (href: string, text: string) =>
  h(
    'a',
    { href, target: '_blank', class: 'vben-link' },
    { default: () => text },
  );

const {
  buildTime,
  dependencies = {},
  devDependencies = {},
  license,
  version,
  // vite inject-metadata 插件注入的全局变量
} = __VBEN_ADMIN_METADATA__ || {};

const pageTitle = computed(() => props.title || String($t('ui.about.title')));
const pageDescription = computed(
  () => props.description || String($t('ui.about.description')),
);

const projectDescriptionItems = computed<DescriptionItem[]>(() => [
  { content: version, title: String($t('ui.about.version')) },
  { content: license, title: String($t('ui.about.license')) },
  { content: buildTime, title: String($t('ui.about.buildTime')) },
  {
    content: renderLink(GIN_VBEN_ADMIN_GITHUB_URL, 'Gin-Vben-Admin'),
    title: String($t('ui.about.repository')),
  },
  {
    content: renderLink(VBEN_GITHUB_URL, 'Vue Vben Admin'),
    title: String($t('ui.about.upstreamProject')),
  },
  {
    content: renderLink(VBEN_DOC_URL, 'Vue Vben Admin Docs'),
    title: String($t('ui.about.upstreamDocs')),
  },
  {
    content: renderLink(VBEN_PREVIEW_URL, 'Vue Vben Admin Preview'),
    title: String($t('ui.about.upstreamPreview')),
  },
]);

const dependenciesItems = Object.keys(dependencies).map((key) => ({
  content: dependencies[key],
  title: key,
}));

const devDependenciesItems = Object.keys(devDependencies).map((key) => ({
  content: devDependencies[key],
  title: key,
}));
</script>

<template>
  <Page :title="pageTitle">
    <template #description>
      <p class="mt-3 text-sm/6 text-foreground">
        <a :href="GIN_VBEN_ADMIN_GITHUB_URL" class="vben-link" target="_blank">
          {{ name }}
        </a>
        {{ pageDescription }}
      </p>
    </template>
    <div class="card-box p-5">
      <div>
        <h5 class="text-lg text-foreground">{{ $t('ui.about.basicInfo') }}</h5>
      </div>
      <div class="mt-4">
        <dl class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
          <template v-for="item in projectDescriptionItems" :key="item.title">
            <div class="border-t border-border px-4 py-6 sm:col-span-1 sm:px-0">
              <dt class="text-sm/6 font-medium text-foreground">
                {{ item.title }}
              </dt>
              <dd class="mt-1 text-sm/6 text-foreground sm:mt-2">
                <VbenRenderContent :content="item.content" />
              </dd>
            </div>
          </template>
        </dl>
      </div>
    </div>

    <div class="card-box mt-6 p-5">
      <div>
        <h5 class="text-lg text-foreground">
          {{ $t('ui.about.productionDependencies') }}
        </h5>
      </div>
      <div class="mt-4">
        <dl class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
          <template v-for="item in dependenciesItems" :key="item.title">
            <div class="border-t border-border px-4 py-3 sm:col-span-1 sm:px-0">
              <dt class="text-sm text-foreground">
                {{ item.title }}
              </dt>
              <dd class="mt-1 text-sm text-foreground/80 sm:mt-2">
                <VbenRenderContent :content="item.content" />
              </dd>
            </div>
          </template>
        </dl>
      </div>
    </div>
    <div class="card-box mt-6 p-5">
      <div>
        <h5 class="text-lg text-foreground">
          {{ $t('ui.about.developmentDependencies') }}
        </h5>
      </div>
      <div class="mt-4">
        <dl class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
          <template v-for="item in devDependenciesItems" :key="item.title">
            <div class="border-t border-border px-4 py-3 sm:col-span-1 sm:px-0">
              <dt class="text-sm text-foreground">
                {{ item.title }}
              </dt>
              <dd class="mt-1 text-sm text-foreground/80 sm:mt-2">
                <VbenRenderContent :content="item.content" />
              </dd>
            </div>
          </template>
        </dl>
      </div>
    </div>
  </Page>
</template>
