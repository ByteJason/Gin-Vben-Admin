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
import { $t } from '#/locales';

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
    {
      key: 'users',
      label: String($t('page.analytics.users')),
      metric: summary.value.counts.users,
    },
    {
      key: 'roles',
      label: String($t('page.analytics.roles')),
      metric: summary.value.counts.roles,
    },
    {
      key: 'tasks',
      label: String($t('page.analytics.tasks')),
      metric: summary.value.counts.tasks,
    },
    {
      key: 'importJobs',
      label: String($t('page.analytics.imports')),
      metric: summary.value.counts.importJobs,
    },
    {
      key: 'exportJobs',
      label: String($t('page.analytics.exports')),
      metric: summary.value.counts.exportJobs,
    },
    {
      key: 'files',
      label: String($t('page.analytics.files')),
      metric: summary.value.counts.files,
    },
    {
      key: 'auditEvents',
      label: String($t('page.analytics.auditEvents')),
      metric: summary.value.counts.auditEvents,
    },
    {
      key: 'mailAccounts',
      label: String($t('page.analytics.mailAccounts')),
      metric: summary.value.counts.mailAccounts,
    },
    {
      key: 'mailMessages',
      label: String($t('page.analytics.mailMessages')),
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
    {
      count: 0,
      label: String($t('page.analytics.hour1')),
      maxAge: 60 * 60 * 1000,
    },
    {
      count: 0,
      label: String($t('page.analytics.hours1to6')),
      maxAge: 6 * 60 * 60 * 1000,
    },
    {
      count: 0,
      label: String($t('page.analytics.hours6to24')),
      maxAge: 24 * 60 * 60 * 1000,
    },
    {
      count: 0,
      label: String($t('page.analytics.hours24Plus')),
      maxAge: Number.POSITIVE_INFINITY,
    },
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
  if (status === 'ok') return String($t('page.analytics.statusOk'));
  if (status === 'degraded') return String($t('page.analytics.statusDegraded'));
  return String($t('page.analytics.statusUnavailable'));
}

function countText(metric: DashboardCountMetric) {
  return metric.value === undefined
    ? String($t('page.analytics.unavailable'))
    : new Intl.NumberFormat().format(metric.value);
}

function formatDuration(seconds?: number) {
  if (seconds === undefined) return String($t('page.analytics.unavailable'));
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return String($t('page.monitor.duration', { days, hours, minutes }));
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
      ? String($t('page.analytics.loadErrorCached'))
      : String($t('page.analytics.loadError'));
  }
  if (sessionsResult.status === 'fulfilled') {
    sessions.value = sessionsResult.value;
    sessionsError.value = '';
  } else {
    sessionsError.value = String($t('page.analytics.sessionsError'));
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
        <p class="eyebrow">{{ $t('page.analytics.eyebrow') }}</p>
        <h1 id="operations-overview-title">{{ $t('page.analytics.title') }}</h1>
        <p class="description">{{ $t('page.analytics.description') }}</p>
      </div>
      <div class="heading-actions">
        <span v-if="summary" class="updated">
          {{
            $t('page.monitor.collectedAt', {
              at: new Date(summary.collectedAt).toLocaleString(),
            })
          }}
        </span>
        <button type="button" :disabled="loading" @click="refresh">
          {{
            loading
              ? $t('page.analytics.refreshing')
              : $t('page.analytics.refresh')
          }}
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
      {{ $t('page.analytics.loading') }}
    </p>

    <template v-if="summary">
      <section class="instance-panel" aria-labelledby="instance-title">
        <div class="instance-copy">
          <div class="section-heading">
            <div>
              <p class="eyebrow">{{ $t('page.analytics.instance') }}</p>
              <h2 id="instance-title">
                {{ $t('page.analytics.instanceStatus') }}
              </h2>
            </div>
            <span class="status" :class="[summary.instance.status]">
              {{ statusText(summary.instance.status) }}
            </span>
          </div>
          <dl class="instance-grid">
            <div>
              <dt>{{ $t('page.analytics.runningStatus') }}</dt>
              <dd>
                {{ summary.instance.state || $t('page.analytics.unavailable') }}
              </dd>
            </div>
            <div>
              <dt>{{ $t('page.analytics.scope') }}</dt>
              <dd>
                {{ summary.instance.scope || $t('page.analytics.unavailable') }}
              </dd>
            </div>
            <div>
              <dt>{{ $t('page.analytics.version') }}</dt>
              <dd>
                {{
                  summary.instance.version || $t('page.analytics.unavailable')
                }}
              </dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.uptime') }}</dt>
              <dd>{{ formatDuration(summary.instance.uptimeSeconds) }}</dd>
            </div>
          </dl>
        </div>
        <div
          class="health-list"
          :aria-label="$t('page.analytics.dependencyHealth')"
        >
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
            <p class="eyebrow">{{ $t('page.analytics.tenantCounts') }}</p>
            <h2 id="counts-title">{{ $t('page.analytics.businessData') }}</h2>
          </div>
          <span class="status" :class="[summary.status]">{{
            $t('page.analytics.overallStatus', {
              status: statusText(summary.status),
            })
          }}</span>
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
            <p class="eyebrow">{{ $t('page.analytics.deviceSessions') }}</p>
            <h2 id="sessions-title">
              {{ $t('page.analytics.deviceSessions') }}
            </h2>
          </div>
          <span class="updated">{{ $t('page.analytics.browserSample') }}</span>
        </div>
        <div class="session-summary">
          <div>
            <span>{{ $t('page.analytics.all') }}</span
            ><strong>{{ sessions.length }}</strong>
          </div>
          <div>
            <span>{{ $t('page.analytics.active') }}</span
            ><strong>{{ activeSessions }}</strong>
          </div>
          <div>
            <span>{{ $t('page.analytics.expiring24h') }}</span
            ><strong>{{ expiringSessions }}</strong>
          </div>
          <div>
            <span>{{ $t('page.analytics.revoked') }}</span
            ><strong>{{ revokedSessions }}</strong>
          </div>
        </div>
        <div
          v-if="sessions.length"
          class="trend-list"
          :aria-label="$t('page.analytics.sessionTrend')"
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
        <p v-else class="empty-state">
          {{ $t('page.analytics.emptySessions') }}
        </p>
      </section>

      <section class="monitor-entry" aria-labelledby="monitor-entry-title">
        <div>
          <p class="eyebrow">{{ $t('page.monitor.eyebrow') }}</p>
          <h2 id="monitor-entry-title">
            {{ $t('page.analytics.monitorQuestion') }}
          </h2>
          <p>{{ $t('page.analytics.monitorDescription') }}</p>
        </div>
        <RouterLink
          v-if="canReadMonitor"
          class="monitor-link"
          to="/system/monitor"
        >
          {{ $t('page.analytics.goToMonitor') }}
        </RouterLink>
        <span v-else class="permission-note">{{
          $t('page.analytics.monitorPermission')
        }}</span>
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
