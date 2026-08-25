<script setup lang="ts">
import type {
  AuthApi,
  DashboardCountMetric,
  DashboardStatus,
  DashboardSummary,
} from '@vben/api-client';

import { computed, ref } from 'vue';
import { RouterLink } from 'vue-router';

import { ManagementPage } from '@vben/common-ui';
import { useVisibilityPolling } from '@vben/hooks';
import { useAccessStore } from '@vben/stores';

import { listSessionsApi } from '#/api/core/auth';
import { getDashboardSummaryApi } from '#/api/core/dashboard';

const accessStore = useAccessStore();
const summary = ref<DashboardSummary>();
const sessions = ref<AuthApi.SessionInfo[]>([]);
const loading = ref(false);
const summaryError = ref('');
const sessionsError = ref('');
const sampledAt = ref(Date.now());

const canReadMonitor = computed(() =>
  accessStore.accessCodes.includes('ops:monitor:read'),
);

const countCards = computed(() => {
  if (!summary.value) return [];
  return [
    { key: 'users', label: '用户', metric: summary.value.counts.users },
    { key: 'roles', label: '角色', metric: summary.value.counts.roles },
    { key: 'tasks', label: '任务', metric: summary.value.counts.tasks },
    {
      key: 'importJobs',
      label: '导入作业',
      metric: summary.value.counts.importJobs,
    },
    {
      key: 'exportJobs',
      label: '导出作业',
      metric: summary.value.counts.exportJobs,
    },
    { key: 'files', label: '文件', metric: summary.value.counts.files },
    {
      key: 'auditEvents',
      label: '审计事件',
      metric: summary.value.counts.auditEvents,
    },
    {
      key: 'mailAccounts',
      label: '邮件账户',
      metric: summary.value.counts.mailAccounts,
    },
    {
      key: 'mailMessages',
      label: '邮件消息',
      metric: summary.value.counts.mailMessages,
    },
  ];
});

const activeSessions = computed(
  () =>
    sessions.value.filter(
      (session) =>
        !session.revoked && Date.parse(session.expiresAt) > sampledAt.value,
    ).length,
);

const revokedSessions = computed(
  () => sessions.value.filter(({ revoked }) => revoked).length,
);

const expiringSessions = computed(
  () =>
    sessions.value.filter((session) => {
      if (session.revoked) return false;
      const remaining = Date.parse(session.expiresAt) - sampledAt.value;
      return remaining >= 0 && remaining <= 24 * 60 * 60 * 1000;
    }).length,
);

const sessionTrend = computed(() => {
  const buckets = [
    { count: 0, label: '1 小时内', maxAge: 60 * 60 * 1000 },
    { count: 0, label: '1–6 小时', maxAge: 6 * 60 * 60 * 1000 },
    { count: 0, label: '6–24 小时', maxAge: 24 * 60 * 60 * 1000 },
    { count: 0, label: '24 小时前', maxAge: Number.POSITIVE_INFINITY },
  ];
  for (const session of sessions.value) {
    const age = Math.max(0, sampledAt.value - Date.parse(session.lastSeenAt));
    const bucket = buckets.find(({ maxAge }) => age <= maxAge);
    if (bucket) bucket.count += 1;
  }
  const maximum = Math.max(1, ...buckets.map(({ count }) => count));
  return buckets.map((bucket) => ({
    ...bucket,
    width: `${(bucket.count / maximum) * 100}%`,
  }));
});

function statusText(status?: DashboardStatus) {
  if (status === 'ok') return '正常';
  if (status === 'degraded') return '局部降级';
  return '不可用';
}

function countText(metric: DashboardCountMetric) {
  return metric.value === undefined
    ? '不可用'
    : new Intl.NumberFormat().format(metric.value);
}

function formatDuration(seconds?: number) {
  if (seconds === undefined) return '不可用';
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${days} 天 ${hours} 小时 ${minutes} 分钟`;
}

async function refresh() {
  if (loading.value) return;
  loading.value = true;
  const [summaryResult, sessionsResult] = await Promise.allSettled([
    getDashboardSummaryApi(),
    listSessionsApi(),
  ]);
  sampledAt.value = Date.now();

  if (summaryResult.status === 'fulfilled') {
    summary.value = summaryResult.value;
    summaryError.value = '';
  } else {
    summaryError.value = summary.value
      ? '最新运行摘要获取失败，当前显示上次成功数据。'
      : '运行摘要暂时不可用，请稍后重试。';
  }
  if (sessionsResult.status === 'fulfilled') {
    sessions.value = sessionsResult.value;
    sessionsError.value = '';
  } else {
    sessionsError.value = '设备会话数据暂时不可用。';
  }
  loading.value = false;
}

useVisibilityPolling(refresh, 15_000);
</script>

<template>
  <ManagementPage
    class="operations-overview-page"
    :busy="loading"
    labelledby="operations-overview-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">DASHBOARD / CURRENT STATE</p>
        <h1 id="operations-overview-title">运行概览</h1>
        <p class="description">
          基于当前租户的真实聚合摘要与设备会话，快速确认实例、依赖和主要业务数据状态。
        </p>
      </div>
      <div class="heading-actions">
        <span v-if="summary" class="updated">
          采集于 {{ new Date(summary.collectedAt).toLocaleString() }} · 每 15 秒
        </span>
        <button type="button" :disabled="loading" @click="refresh">
          {{ loading ? '刷新中…' : '立即刷新' }}
        </button>
      </div>
    </header>

    <p v-if="summaryError" class="feedback error" role="alert">
      {{ summaryError }}
    </p>
    <p v-if="sessionsError" class="feedback warning" role="status">
      {{ sessionsError }}
    </p>
    <p v-if="loading && !summary" class="loading-state" role="status">
      正在读取运行摘要…
    </p>

    <template v-if="summary">
      <section class="instance-panel" aria-labelledby="instance-title">
        <div class="instance-copy">
          <div class="section-heading">
            <div>
              <p class="eyebrow">INSTANCE</p>
              <h2 id="instance-title">实例状态</h2>
            </div>
            <span class="status" :class="[summary.instance.status]">
              {{ statusText(summary.instance.status) }}
            </span>
          </div>
          <dl class="instance-grid">
            <div>
              <dt>运行状态</dt>
              <dd>{{ summary.instance.state || '不可用' }}</dd>
            </div>
            <div>
              <dt>采集范围</dt>
              <dd>{{ summary.instance.scope || '不可用' }}</dd>
            </div>
            <div>
              <dt>版本</dt>
              <dd>{{ summary.instance.version || '不可用' }}</dd>
            </div>
            <div>
              <dt>运行时长</dt>
              <dd>{{ formatDuration(summary.instance.uptimeSeconds) }}</dd>
            </div>
          </dl>
        </div>
        <div class="health-list" aria-label="依赖健康状态">
          <div
            v-for="health in [
              {
                key: 'runtime',
                label: 'Runtime',
                metric: summary.health.runtime,
              },
              {
                key: 'database',
                label: 'Database',
                metric: summary.health.database,
              },
              { key: 'redis', label: 'Redis', metric: summary.health.redis },
            ]"
            :key="health.key"
          >
            <span>{{ health.label }}</span>
            <strong class="health-state" :class="[health.metric.status]">{{
              health.metric.state || statusText(health.metric.status)
            }}</strong>
          </div>
        </div>
      </section>

      <section class="counts-section" aria-labelledby="counts-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">TENANT COUNTS</p>
            <h2 id="counts-title">业务数据</h2>
          </div>
          <span class="status" :class="[summary.status]">整体{{ statusText(summary.status) }}</span>
        </div>
        <div class="count-grid">
          <article
            v-for="card in countCards"
            :key="card.key"
            class="count-card"
          >
            <span>{{ card.label }}</span>
            <strong>{{ countText(card.metric) }}</strong>
            <small class="metric-status" :class="[card.metric.status]">{{
              statusText(card.metric.status)
            }}</small>
            <p v-if="card.metric.message">{{ card.metric.message }}</p>
          </article>
        </div>
      </section>

      <section class="sessions-panel" aria-labelledby="sessions-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">AUTH SESSIONS</p>
            <h2 id="sessions-title">设备会话</h2>
          </div>
          <span class="updated">当前浏览器采样</span>
        </div>
        <div class="session-summary">
          <div>
            <span>全部</span><strong>{{ sessions.length }}</strong>
          </div>
          <div>
            <span>活动</span><strong>{{ activeSessions }}</strong>
          </div>
          <div>
            <span>24h 内过期</span><strong>{{ expiringSessions }}</strong>
          </div>
          <div>
            <span>已撤销</span><strong>{{ revokedSessions }}</strong>
          </div>
        </div>
        <div
          v-if="sessions.length"
          class="trend-list"
          aria-label="按最近活动时间分组的会话趋势"
        >
          <div
            v-for="bucket in sessionTrend"
            :key="bucket.label"
            class="trend-row"
          >
            <span>{{ bucket.label }}</span>
            <div class="trend-track" aria-hidden="true">
              <i :style="{ width: bucket.width }"></i>
            </div>
            <strong>{{ bucket.count }}</strong>
          </div>
        </div>
        <p v-else class="empty-state">当前没有设备会话数据。</p>
      </section>

      <section class="monitor-entry" aria-labelledby="monitor-entry-title">
        <div>
          <p class="eyebrow">DEEP DIAGNOSTICS</p>
          <h2 id="monitor-entry-title">需要更完整的资源字段？</h2>
          <p>
            资源监控按能力展示 CPU、内存、文件系统、Go runtime
            与连接池，不在概览中扩大权限。
          </p>
        </div>
        <RouterLink
          v-if="canReadMonitor"
          class="monitor-link"
          to="/system/monitor"
          >
进入资源监控
</RouterLink>
        <span v-else class="permission-note">需要 ops:monitor:read 权限</span>
      </section>
    </template>
  </ManagementPage>
</template>

<style scoped>
.operations-overview-page {
  --overview-accent: hsl(var(--primary));
  --overview-line: hsl(var(--border));
  --overview-muted: hsl(var(--muted-foreground));

  color: hsl(var(--foreground));
}

.page-heading,
.section-heading,
.heading-actions,
.monitor-entry {
  display: flex;
  gap: 1rem;
  align-items: flex-start;
  justify-content: space-between;
}

.eyebrow {
  margin: 0 0 0.4rem;
  font-size: 0.72rem;
  font-weight: 800;
  color: var(--overview-accent);
  letter-spacing: 0.12em;
}

h1 {
  margin: 0 0 0.5rem;
  font-size: clamp(1.8rem, 3vw, 2.6rem);
}

h2 {
  margin: 0;
  font-size: 1.08rem;
}

.description,
.updated,
.permission-note,
.empty-state {
  color: var(--overview-muted);
}

.description {
  max-inline-size: 72ch;
  margin: 0;
}

.heading-actions {
  align-items: center;
}

button,
.monitor-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: 2.5rem;
  padding: 0 0.9rem;
  color: inherit;
  text-decoration: none;
  cursor: pointer;
  background: hsl(var(--card));
  border: 1px solid var(--overview-line);
  border-radius: 0.6rem;
}

button:focus-visible,
.monitor-link:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--overview-accent) 30%, transparent);
  outline-offset: 2px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.feedback {
  padding: 0.8rem 1rem;
  margin: 1rem 0 0;
  border-radius: 0.7rem;
}

.error {
  color: hsl(var(--destructive));
  background: color-mix(in srgb, hsl(var(--destructive)) 10%, hsl(var(--card)));
}

.warning {
  color: #92400e;
  background: color-mix(in srgb, #f59e0b 12%, hsl(var(--card)));
}

.loading-state {
  margin: 1.5rem 0;
  color: var(--overview-muted);
}

.instance-panel,
.counts-section,
.sessions-panel,
.monitor-entry {
  min-inline-size: 0;
  padding: clamp(1rem, 1.5vw, 1.5rem);
  margin-block-start: 1rem;
  background: hsl(var(--card));
  border: 1px solid var(--overview-line);
  border-radius: 1rem;
  box-shadow: 0 0.5rem 1.5rem rgb(15 23 42 / 5%);
}

.instance-panel {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(15rem, 0.6fr);
  gap: 1rem;
}

.instance-grid,
.count-grid,
.session-summary {
  display: grid;
  gap: 0.8rem;
}

.instance-grid {
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 10rem), 1fr));
  margin: 1rem 0 0;
}

.instance-grid dt,
.count-card > span,
.session-summary span {
  font-size: 0.78rem;
  color: var(--overview-muted);
}

.instance-grid dd {
  margin: 0.25rem 0 0;
  font-weight: 750;
  overflow-wrap: anywhere;
}

.health-list {
  display: grid;
  gap: 0.6rem;
  align-content: start;
}

.health-list > div {
  display: flex;
  gap: 1rem;
  justify-content: space-between;
  padding-block-end: 0.55rem;
  border-block-end: 1px solid var(--overview-line);
}

.health-state,
.metric-status {
  font-size: 0.74rem;
}

.health-state.ok,
.metric-status.ok {
  color: #15803d;
}

.health-state.degraded,
.metric-status.degraded {
  color: #a16207;
}

.health-state.unavailable,
.metric-status.unavailable {
  color: #b91c1c;
}

.status {
  flex: none;
  padding: 0.25rem 0.55rem;
  font-size: 0.72rem;
  font-weight: 800;
  border-radius: 999px;
}

.status.ok {
  color: #166534;
  background: #dcfce7;
}

.status.degraded {
  color: #92400e;
  background: #fef3c7;
}

.status.unavailable {
  color: #991b1b;
  background: #fee2e2;
}

.count-grid {
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 10.5rem), 1fr));
  margin-block-start: 1rem;
}

.count-card {
  display: grid;
  gap: 0.35rem;
  min-inline-size: 0;
  padding: 0.9rem;
  border: 1px solid var(--overview-line);
  border-radius: 0.75rem;
}

.count-card strong {
  font-size: 1.45rem;
}

.count-card p {
  margin: 0.2rem 0 0;
  font-size: 0.75rem;
  color: var(--overview-muted);
  overflow-wrap: anywhere;
}

.session-summary {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin-block-start: 1rem;
}

.session-summary > div {
  display: grid;
  gap: 0.35rem;
  padding-inline-start: 0.7rem;
  border-inline-start: 3px solid var(--overview-accent);
}

.session-summary strong {
  font-size: 1.2rem;
}

.trend-list {
  display: grid;
  gap: 0.8rem;
  margin-block-start: 1.2rem;
}

.trend-row {
  display: grid;
  grid-template-columns: minmax(6rem, 9rem) 1fr 2rem;
  gap: 0.75rem;
  align-items: center;
}

.trend-track {
  block-size: 0.55rem;
  overflow: hidden;
  background: hsl(var(--muted));
  border-radius: 999px;
}

.trend-track i {
  display: block;
  block-size: 100%;
  background: var(--overview-accent);
  border-radius: inherit;
}

.monitor-entry {
  align-items: center;
}

.monitor-entry p {
  max-inline-size: 70ch;
  margin: 0.5rem 0 0;
  color: var(--overview-muted);
}

.monitor-link {
  font-weight: 750;
  color: hsl(var(--primary-foreground));
  white-space: nowrap;
  background: var(--overview-accent);
  border-color: var(--overview-accent);
}

@media (width <= 800px) {
  .page-heading,
  .heading-actions,
  .monitor-entry {
    flex-direction: column;
    align-items: stretch;
  }

  .instance-panel {
    grid-template-columns: 1fr;
  }

  .session-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (width <= 480px) {
  .session-summary {
    grid-template-columns: 1fr;
  }

  .trend-row {
    grid-template-columns: 1fr 2rem;
  }

  .trend-track {
    grid-row: 2;
    grid-column: 1 / -1;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
  }
}
</style>
