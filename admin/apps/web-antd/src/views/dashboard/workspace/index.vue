<script lang="ts" setup>
import type {
  WorkbenchProjectItem,
  WorkbenchQuickNavItem,
  WorkbenchTodoItem,
  WorkbenchTrendItem,
} from '@vben/common-ui';

import { computed } from 'vue';
import { useRouter } from 'vue-router';

import {
  AnalysisChartCard,
  WorkbenchHeader,
  WorkbenchProject,
  WorkbenchQuickNav,
  WorkbenchTodo,
  WorkbenchTrends,
} from '@vben/common-ui';
import { preferences } from '@vben/preferences';
import { useUserStore } from '@vben/stores';
import { openWindow } from '@vben/utils';
import { $t } from '#/locales';

import AnalyticsVisitsSource from '../analytics/analytics-visits-source.vue';

const userStore = useUserStore();

// 这是一个示例数据，实际项目中需要根据实际情况进行调整
// url 也可以是内部路由，在 navTo 方法中识别处理，进行内部跳转
// 例如：url: /dashboard/workspace
const projectItems = computed<WorkbenchProjectItem[]>(() => [
  {
    color: '',
    content: String($t('page.workspace.projects.0.content')),
    date: '2021-04-01',
    group: String($t('page.workspace.projects.0.group')),
    icon: 'carbon:logo-github',
    title: 'Github',
    url: 'https://github.com',
  },
  {
    color: '#3fb27f',
    content: String($t('page.workspace.projects.1.content')),
    date: '2021-04-01',
    group: String($t('page.workspace.projects.1.group')),
    icon: 'ion:logo-vue',
    title: 'Vue',
    url: 'https://vuejs.org',
  },
  {
    color: '#e18525',
    content: String($t('page.workspace.projects.2.content')),
    date: '2021-04-01',
    group: String($t('page.workspace.projects.2.group')),
    icon: 'ion:logo-html5',
    title: 'Html5',
    url: 'https://developer.mozilla.org/zh-CN/docs/Web/HTML',
  },
  {
    color: '#bf0c2c',
    content: String($t('page.workspace.projects.3.content')),
    date: '2021-04-01',
    group: String($t('page.workspace.projects.3.group')),
    icon: 'ion:logo-angular',
    title: 'Angular',
    url: 'https://angular.io',
  },
  {
    color: '#00d8ff',
    content: String($t('page.workspace.projects.4.content')),
    date: '2021-04-01',
    group: String($t('page.workspace.projects.4.group')),
    icon: 'bx:bxl-react',
    title: 'React',
    url: 'https://reactjs.org',
  },
  {
    color: '#EBD94E',
    content: String($t('page.workspace.projects.5.content')),
    date: '2021-04-01',
    group: String($t('page.workspace.projects.5.group')),
    icon: 'ion:logo-javascript',
    title: 'Js',
    url: 'https://developer.mozilla.org/zh-CN/docs/Web/JavaScript',
  },
]);

// 同样，这里的 url 也可以使用以 http 开头的外部链接
const quickNavItems = computed<WorkbenchQuickNavItem[]>(() => [
  {
    color: '#1fdaca',
    icon: 'ion:home-outline',
    title: String($t('page.workspace.quickNav.home')),
    url: '/',
  },
  {
    color: '#bf0c2c',
    icon: 'ion:grid-outline',
    title: String($t('page.workspace.quickNav.dashboard')),
    url: '/dashboard',
  },
  {
    color: '#e18525',
    icon: 'ion:layers-outline',
    title: String($t('page.workspace.quickNav.components')),
    url: '/demos/features/icons',
  },
  {
    color: '#3fb27f',
    icon: 'ion:settings-outline',
    title: String($t('page.workspace.quickNav.system')),
    url: '/demos/features/login-expired', // 这里的 URL 是示例，实际项目中需要根据实际情况进行调整
  },
  {
    color: '#4daf1bc9',
    icon: 'ion:key-outline',
    title: String($t('page.workspace.quickNav.access')),
    url: '/demos/access/page-control',
  },
  {
    color: '#00d8ff',
    icon: 'ion:bar-chart-outline',
    title: String($t('page.workspace.quickNav.charts')),
    url: '/analytics',
  },
]);

const todoItems = computed<WorkbenchTodoItem[]>(() => [
  {
    completed: false,
    content: String($t('page.workspace.todos.0.content')),
    date: '2024-07-30 11:00:00',
    title: String($t('page.workspace.todos.0.title')),
  },
  {
    completed: true,
    content: String($t('page.workspace.todos.1.content')),
    date: '2024-07-30 11:00:00',
    title: String($t('page.workspace.todos.1.title')),
  },
  {
    completed: false,
    content: String($t('page.workspace.todos.2.content')),
    date: '2024-07-30 11:00:00',
    title: String($t('page.workspace.todos.2.title')),
  },
  {
    completed: false,
    content: String($t('page.workspace.todos.3.content')),
    date: '2024-07-30 11:00:00',
    title: String($t('page.workspace.todos.3.title')),
  },
  {
    completed: false,
    content: String($t('page.workspace.todos.4.content')),
    date: '2024-07-30 11:00:00',
    title: String($t('page.workspace.todos.4.title')),
  },
]);
const trendItems = computed<WorkbenchTrendItem[]>(() => [
  {
    avatar: 'svg:avatar-1',
    content: String($t('page.workspace.trends.0.content')),
    date: String($t('page.workspace.trends.0.date')),
    title: String($t('page.workspace.trends.0.title')),
  },
  {
    avatar: 'svg:avatar-2',
    content: String($t('page.workspace.trends.1.content')),
    date: String($t('page.workspace.trends.1.date')),
    title: String($t('page.workspace.trends.1.title')),
  },
  {
    avatar: 'svg:avatar-3',
    content: String($t('page.workspace.trends.2.content')),
    date: String($t('page.workspace.trends.2.date')),
    title: String($t('page.workspace.trends.2.title')),
  },
  {
    avatar: 'svg:avatar-4',
    content: String($t('page.workspace.trends.3.content')),
    date: String($t('page.workspace.trends.3.date')),
    title: String($t('page.workspace.trends.3.title')),
  },
  {
    avatar: 'svg:avatar-1',
    content: String($t('page.workspace.trends.4.content')),
    date: String($t('page.workspace.trends.4.date')),
    title: String($t('page.workspace.trends.4.title')),
  },
  {
    avatar: 'svg:avatar-2',
    content: String($t('page.workspace.trends.5.content')),
    date: String($t('page.workspace.trends.5.date')),
    title: String($t('page.workspace.trends.5.title')),
  },
  {
    avatar: 'svg:avatar-3',
    content: String($t('page.workspace.trends.6.content')),
    date: String($t('page.workspace.trends.6.date')),
    title: String($t('page.workspace.trends.6.title')),
  },
  {
    avatar: 'svg:avatar-4',
    content: String($t('page.workspace.trends.7.content')),
    date: String($t('page.workspace.trends.7.date')),
    title: String($t('page.workspace.trends.7.title')),
  },
  {
    avatar: 'svg:avatar-4',
    content: String($t('page.workspace.trends.8.content')),
    date: String($t('page.workspace.trends.8.date')),
    title: String($t('page.workspace.trends.8.title')),
  },
]);

const router = useRouter();

// 这是一个示例方法，实际项目中需要根据实际情况进行调整
// This is a sample method, adjust according to the actual project requirements
function navTo(nav: WorkbenchProjectItem | WorkbenchQuickNavItem) {
  if (nav.url?.startsWith('http')) {
    openWindow(nav.url);
    return;
  }
  if (nav.url?.startsWith('/')) {
    router.push(nav.url).catch((error) => {
      console.error('Navigation failed:', error);
    });
  } else {
    console.warn(`Unknown URL for navigation item: ${nav.title} -> ${nav.url}`);
  }
}
</script>

<template>
  <div class="p-5">
    <WorkbenchHeader
      :avatar="userStore.userInfo?.avatar || preferences.app.defaultAvatar"
    >
      <template #title>
        {{
          $t('page.workspace.greeting', {
            name: userStore.userInfo?.realName || '',
          })
        }}
      </template>
      <template #description>{{ $t('page.workspace.weather') }}</template>
      <template #actions>
        <div class="flex flex-col justify-center text-right">
          <span class="text-foreground/80">{{
            $t('page.workspace.stats.todo')
          }}</span>
          <span class="text-2xl">2/10</span>
        </div>
        <div class="mx-12 flex flex-col justify-center text-right md:mx-16">
          <span class="text-foreground/80">{{
            $t('page.workspace.stats.projects')
          }}</span>
          <span class="text-2xl">8</span>
        </div>
        <div class="mr-4 flex flex-col justify-center text-right md:mr-10">
          <span class="text-foreground/80">{{
            $t('page.workspace.stats.team')
          }}</span>
          <span class="text-2xl">300</span>
        </div>
      </template>
    </WorkbenchHeader>

    <div class="flex flex-col lg:flex-row">
      <div class="mr-4 w-full lg:w-3/5">
        <WorkbenchProject
          :items="projectItems"
          :title="$t('page.workspace.projectsTitle')"
          @click="navTo"
        />
        <WorkbenchTrends
          :items="trendItems"
          class="mt-5"
          :title="$t('page.workspace.trendsTitle')"
        />
      </div>
      <div class="w-full lg:w-2/5">
        <WorkbenchQuickNav
          :items="quickNavItems"
          class="lg:mt-0"
          :title="$t('page.workspace.quickNavTitle')"
          @click="navTo"
        />
        <WorkbenchTodo
          :items="todoItems"
          class="mt-5"
          :title="$t('page.workspace.todoTitle')"
        />
        <AnalysisChartCard
          class="mt-5"
          :title="$t('page.workspace.sourceTitle')"
        >
          <AnalyticsVisitsSource />
        </AnalysisChartCard>
      </div>
    </div>
  </div>
</template>
