<script setup lang="ts">
import type {
  AuthApi,
  MonitorCapabilities,
  MonitorOverview,
  MonitorStatus,
} from '@vben/api-client';

import { computed, ref } from 'vue';

import { ManagementPage } from '@vben/common-ui';
import { useVisibilityPolling } from '@vben/hooks';

import { listSessionsApi } from '#/api/core/auth';
import { getMonitorOverviewApi } from '#/api/core/monitor';
import { $t } from '#/locales';

const overview = ref<MonitorOverview>();
const sessions = ref<AuthApi.SessionInfo[]>([]);
const loading = ref(false);
const monitorError = ref('');
const sessionsError = ref('');
const sampledAt = ref(Date.now());

const sessionTrend = computed(() => {
  const buckets = [
    {
      count: 0,
      label: String($t('page.monitor.minutes15')),
      maxAge: 15 * 60 * 1000,
    },
    {
      count: 0,
      label: String($t('page.monitor.minutes15to60')),
      maxAge: 60 * 60 * 1000,
    },
    {
      count: 0,
      label: String($t('page.monitor.hours1to24')),
      maxAge: 24 * 60 * 60 * 1000,
    },
    {
      count: 0,
      label: String($t('page.monitor.hours24Plus')),
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

const activeSessions = computed(
  () =>
    sessions.value.filter(
      (session) =>
        !session.revoked && Date.parse(session.expiresAt) > sampledAt.value,
    ).length,
);

function statusText(status: MonitorStatus | undefined) {
  if (status === 'ok') return String($t('page.monitor.statusOk'));
  if (status === 'degraded') return String($t('page.monitor.statusDegraded'));
  return String($t('page.monitor.statusUnavailable'));
}

function valueText(value: number | string | undefined, suffix = '') {
  return value === undefined || value === ''
    ? String($t('page.monitor.unavailable'))
    : `${value}${suffix}`;
}

function formatBytes(value?: number) {
  if (value === undefined) return String($t('page.monitor.unavailable'));
  if (value === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const exponent = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  return `${(value / 1024 ** exponent).toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

function formatPercent(value?: number) {
  return value === undefined
    ? String($t('page.monitor.unavailable'))
    : `${(value * 100).toFixed(1)}%`;
}

function formatDuration(seconds?: number) {
  if (seconds === undefined) return String($t('page.monitor.unavailable'));
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return String($t('page.monitor.duration', { days, hours, minutes }));
}

function capabilityEntries(capabilities: MonitorCapabilities) {
  return Object.entries(capabilities).sort(([left], [right]) =>
    left.localeCompare(right),
  );
}

async function refresh() {
  if (loading.value) return;
  loading.value = true;
  const [monitorResult, sessionsResult] = await Promise.allSettled([
    getMonitorOverviewApi(),
    listSessionsApi(),
  ]);
  sampledAt.value = Date.now();

  if (monitorResult.status === 'fulfilled') {
    overview.value = monitorResult.value;
    monitorError.value = '';
  } else {
    monitorError.value = overview.value
      ? String($t('page.monitor.loadErrorCached'))
      : String($t('page.monitor.loadError'));
  }
  if (sessionsResult.status === 'fulfilled') {
    sessions.value = sessionsResult.value;
    sessionsError.value = '';
  } else {
    sessionsError.value = String($t('page.monitor.sessionsError'));
  }
  loading.value = false;
}

useVisibilityPolling(refresh, 15_000);
</script>

<template>
  <ManagementPage
    class="monitor-page"
    :busy="loading"
    labelledby="monitor-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.monitor.eyebrow') }}</p>
        <h1 id="monitor-title">{{ $t('page.monitor.title') }}</h1>
        <p class="description">{{ $t('page.monitor.description') }}</p>
      </div>
      <div class="heading-actions">
        <span v-if="overview" class="updated">
          {{
            $t('page.monitor.collectedAt', {
              at: new Date(overview.collectedAt).toLocaleString(),
            })
          }}
        </span>
        <button type="button" :disabled="loading" @click="refresh">
          {{
            loading ? $t('page.monitor.refreshing') : $t('page.monitor.refresh')
          }}
        </button>
      </div>
    </header>

    <p v-if="monitorError" class="feedback error" role="alert">
      {{ monitorError }}
    </p>
    <p v-if="sessionsError" class="feedback warning" role="status">
      {{ sessionsError }}
    </p>
    <p v-if="loading && !overview" class="loading-state" role="status">
      {{ $t('page.monitor.loading') }}
    </p>

    <template v-if="overview">
      <section
        class="summary-grid"
        :aria-label="$t('page.monitor.summaryLabel')"
      >
        <article class="summary-card">
          <span>{{ $t('page.monitor.scope') }}</span
          ><strong>{{ overview.scope }}</strong>
        </article>
        <article class="summary-card">
          <span>{{ $t('page.monitor.uptime') }}</span
          ><strong>{{ formatDuration(overview.uptimeSeconds) }}</strong>
        </article>
        <article class="summary-card">
          <span>{{ $t('page.monitor.version') }}</span
          ><strong>{{
            overview.version || $t('page.monitor.unavailable')
          }}</strong>
        </article>
        <article class="summary-card">
          <span>{{ $t('page.monitor.activeSessions') }}</span
          ><strong>{{ activeSessions }} / {{ sessions.length }}</strong>
        </article>
      </section>

      <section class="panel" aria-labelledby="runtime-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.monitor.runtime') }}</p>
            <h2 id="runtime-title">{{ $t('page.monitor.runtime') }}</h2>
          </div>
          <span class="status" :class="[overview.runtime.status]">
            {{ statusText(overview.runtime.status) }}
          </span>
        </div>
        <dl class="detail-grid">
          <div>
            <dt>{{ $t('page.monitor.goVersion') }}</dt>
            <dd>{{ overview.runtime.goVersion }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.os') }}</dt>
            <dd>{{ overview.runtime.os }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.architecture') }}</dt>
            <dd>{{ overview.runtime.arch }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.applicationVersion') }}</dt>
            <dd>
              {{
                overview.runtime.applicationVersion ||
                $t('page.monitor.unavailable')
              }}
            </dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.commit') }}</dt>
            <dd class="code-value">
              {{ overview.runtime.commit || $t('page.monitor.unavailable') }}
            </dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.heapAlloc') }}</dt>
            <dd>{{ formatBytes(overview.runtime.heapAllocBytes) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.heapSys') }}</dt>
            <dd>{{ formatBytes(overview.runtime.heapSysBytes) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.heapInUse') }}</dt>
            <dd>{{ formatBytes(overview.runtime.heapInUseBytes) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.heapObjects') }}</dt>
            <dd>{{ valueText(overview.runtime.heapObjects) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.nextGc') }}</dt>
            <dd>{{ formatBytes(overview.runtime.nextGcBytes) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.gcCount') }}</dt>
            <dd>{{ valueText(overview.runtime.gcCount) }}</dd>
          </div>
          <div>
            <dt>{{ $t('page.monitor.lastGcPause') }}</dt>
            <dd>{{ valueText(overview.runtime.lastGcPauseNs, ' ns') }}</dd>
          </div>
        </dl>
        <details class="capabilities">
          <summary>{{ $t('page.monitor.capabilities') }}</summary>
          <ul>
            <li
              v-for="[name, capability] in capabilityEntries(
                overview.runtime.capabilities,
              )"
              :key="name"
            >
              <strong>{{ name }}</strong>
              <span
                >{{
                  capability.available
                    ? $t('page.monitor.available')
                    : $t('page.monitor.unavailable')
                }}
                · {{ capability.scope
                }}<template v-if="capability.source">
                  · {{ capability.source }}</template
                ></span
              >
            </li>
          </ul>
        </details>
      </section>

      <section
        class="resource-grid"
        :aria-label="$t('page.monitor.cpuMemoryDisk')"
      >
        <article
          v-for="resource in [
            { key: 'cpu', title: 'CPU', metric: overview.cpu },
            {
              key: 'memory',
              title: $t('page.monitor.memory'),
              metric: overview.memory,
            },
            {
              key: 'disk',
              title: $t('page.monitor.disk'),
              metric: overview.disk,
            },
          ]"
          :key="resource.key"
          class="panel resource-card"
        >
          <div class="section-heading">
            <h2>{{ resource.title }}</h2>
            <span class="status" :class="[resource.metric.status]">{{
              statusText(resource.metric.status)
            }}</span>
          </div>
          <dl class="metric-list">
            <div>
              <dt>{{ $t('page.monitor.logicalCores') }}</dt>
              <dd>{{ valueText(resource.metric.cores) }}</dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.loadAverage') }}</dt>
              <dd>
                {{ valueText(resource.metric.load1) }} /
                {{ valueText(resource.metric.load5) }} /
                {{ valueText(resource.metric.load15) }}
              </dd>
            </div>
            <div>
              <dt>RSS</dt>
              <dd>{{ formatBytes(resource.metric.rssBytes) }}</dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.used') }}</dt>
              <dd>{{ formatBytes(resource.metric.usedBytes) }}</dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.free') }}</dt>
              <dd>{{ formatBytes(resource.metric.freeBytes) }}</dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.total') }}</dt>
              <dd>{{ formatBytes(resource.metric.totalBytes) }}</dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.usage') }}</dt>
              <dd>{{ formatPercent(resource.metric.utilization) }}</dd>
            </div>
          </dl>
          <p v-if="resource.metric.message" class="metric-message">
            {{ resource.metric.message }}
          </p>
          <details class="capabilities">
            <summary>{{ $t('page.monitor.capabilities') }}</summary>
            <ul>
              <li
                v-for="[name, capability] in capabilityEntries(
                  resource.metric.capabilities,
                )"
                :key="name"
              >
                <strong>{{ name }}</strong
                ><span
                  >{{
                    capability.available
                      ? $t('page.monitor.available')
                      : $t('page.monitor.unavailable')
                  }}
                  · {{ capability.scope
                  }}<template v-if="capability.source">
                    · {{ capability.source }}</template
                  ></span
                >
              </li>
            </ul>
          </details>
        </article>
      </section>

      <section
        class="dependency-grid"
        :aria-label="$t('page.monitor.databaseRedis')"
      >
        <article class="panel">
          <div class="section-heading">
            <div>
              <p class="eyebrow">{{ $t('page.monitor.database') }}</p>
              <h2>{{ $t('page.monitor.database') }}</h2>
            </div>
            <span class="status" :class="[overview.database.status]">{{
              statusText(overview.database.status)
            }}</span>
          </div>
          <dl class="detail-grid compact">
            <div>
              <dt>{{ $t('page.monitor.driver') }}</dt>
              <dd>
                {{ overview.database.driver || $t('page.monitor.unavailable') }}
              </dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.mode') }}</dt>
              <dd>
                {{ overview.database.mode || $t('page.monitor.unavailable') }}
              </dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.latency') }}</dt>
              <dd>{{ valueText(overview.database.latencyMs, ' ms') }}</dd>
            </div>
            <template v-if="overview.database.pool">
              <div>
                <dt>{{ $t('page.monitor.open') }}</dt>
                <dd>{{ overview.database.pool.open }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.inUse') }}</dt>
                <dd>{{ overview.database.pool.inUse }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.idle') }}</dt>
                <dd>{{ overview.database.pool.idle }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.max') }}</dt>
                <dd>
                  {{
                    overview.database.pool.max === 0
                      ? $t('page.monitor.unlimited')
                      : overview.database.pool.max
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.waitCount') }}</dt>
                <dd>{{ overview.database.pool.waitCount }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.waitDuration') }}</dt>
                <dd>{{ overview.database.pool.waitDurationMs }} ms</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.maxIdleClosed') }}</dt>
                <dd>{{ overview.database.pool.maxIdleClosed }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.maxIdleTimeClosed') }}</dt>
                <dd>{{ overview.database.pool.maxIdleTimeClosed }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.maxLifetimeClosed') }}</dt>
                <dd>{{ overview.database.pool.maxLifetimeClosed }}</dd>
              </div>
            </template>
          </dl>
          <p v-if="overview.database.message" class="metric-message">
            {{ overview.database.message }}
          </p>
          <details class="capabilities">
            <summary>{{ $t('page.monitor.capabilities') }}</summary>
            <ul>
              <li
                v-for="[name, capability] in capabilityEntries(
                  overview.database.capabilities,
                )"
                :key="name"
              >
                <strong>{{ name }}</strong
                ><span
                  >{{
                    capability.available
                      ? $t('page.monitor.available')
                      : $t('page.monitor.unavailable')
                  }}
                  · {{ capability.scope
                  }}<template v-if="capability.source">
                    · {{ capability.source }}</template
                  ></span
                >
              </li>
            </ul>
          </details>
        </article>

        <article class="panel">
          <div class="section-heading">
            <div>
              <p class="eyebrow">REDIS</p>
              <h2>Redis</h2>
            </div>
            <span class="status" :class="[overview.redis.status]">{{
              statusText(overview.redis.status)
            }}</span>
          </div>
          <dl class="detail-grid compact">
            <div>
              <dt>{{ $t('page.monitor.mode') }}</dt>
              <dd>
                {{ overview.redis.mode || $t('page.monitor.unavailable') }}
              </dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.latency') }}</dt>
              <dd>{{ valueText(overview.redis.latencyMs, ' ms') }}</dd>
            </div>
            <div>
              <dt>{{ $t('page.monitor.keyspace') }}</dt>
              <dd>{{ valueText(overview.redis.keyspace) }}</dd>
            </div>
            <template v-if="overview.redis.pool">
              <div>
                <dt>{{ $t('page.monitor.max') }}</dt>
                <dd>{{ valueText(overview.redis.pool.max) }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.total') }}</dt>
                <dd>{{ overview.redis.pool.total }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.active') }}</dt>
                <dd>{{ overview.redis.pool.active }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.idle') }}</dt>
                <dd>{{ overview.redis.pool.idle }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.hits') }}</dt>
                <dd>{{ overview.redis.pool.hits }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.misses') }}</dt>
                <dd>{{ overview.redis.pool.misses }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.timeouts') }}</dt>
                <dd>{{ overview.redis.pool.timeouts }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.waitCount') }}</dt>
                <dd>{{ overview.redis.pool.waitCount }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.waitDuration') }}</dt>
                <dd>{{ overview.redis.pool.waitDurationMs }} ms</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.stale') }}</dt>
                <dd>{{ overview.redis.pool.stale }}</dd>
              </div>
              <div>
                <dt>{{ $t('page.monitor.pending') }}</dt>
                <dd>{{ overview.redis.pool.pending }}</dd>
              </div>
            </template>
          </dl>
          <p v-if="overview.redis.message" class="metric-message">
            {{ overview.redis.message }}
          </p>
          <details class="capabilities">
            <summary>{{ $t('page.monitor.capabilities') }}</summary>
            <ul>
              <li
                v-for="[name, capability] in capabilityEntries(
                  overview.redis.capabilities,
                )"
                :key="name"
              >
                <strong>{{ name }}</strong
                ><span
                  >{{
                    capability.available
                      ? $t('page.monitor.available')
                      : $t('page.monitor.unavailable')
                  }}
                  · {{ capability.scope
                  }}<template v-if="capability.source">
                    · {{ capability.source }}</template
                  ></span
                >
              </li>
            </ul>
          </details>
        </article>
      </section>

      <section class="panel" aria-labelledby="session-trend-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.monitor.sessionActivity') }}</p>
            <h2 id="session-trend-title">
              {{ $t('page.monitor.sessionActivity') }}
            </h2>
          </div>
          <span class="muted">{{
            $t('page.monitor.deviceSessionCount', { count: sessions.length })
          }}</span>
        </div>
        <div v-if="sessions.length" class="trend-list">
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
        <p v-else class="empty-state">{{ $t('page.monitor.emptySessions') }}</p>
      </section>
    </template>

    <p class="privacy-note">
      {{ $t('page.monitor.privacyNote') }}
    </p>
  </ManagementPage>
</template>

<style scoped>
.monitor-page {
  --monitor-accent: hsl(var(--primary));
  --monitor-line: hsl(var(--border));
  --monitor-muted: hsl(var(--muted-foreground));

  color: hsl(var(--foreground));
}

.page-heading,
.section-heading,
.heading-actions {
  display: flex;
  gap: 1rem;
  align-items: flex-start;
  justify-content: space-between;
}

.eyebrow {
  margin: 0 0 0.4rem;
  font-size: 0.72rem;
  font-weight: 800;
  color: var(--monitor-accent);
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
.muted,
.metric-message,
.privacy-note {
  color: var(--monitor-muted);
}

.description {
  max-inline-size: 72ch;
  margin: 0;
}

.heading-actions {
  align-items: center;
}

button {
  min-block-size: 2.5rem;
  padding: 0 0.9rem;
  color: inherit;
  cursor: pointer;
  background: hsl(var(--card));
  border: 1px solid var(--monitor-line);
  border-radius: 0.6rem;
}

button:focus-visible,
summary:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--monitor-accent) 30%, transparent);
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

.loading-state,
.empty-state {
  margin: 1.5rem 0;
  color: var(--monitor-muted);
}

.summary-grid,
.resource-grid,
.dependency-grid {
  display: grid;
  gap: 1rem;
  margin-block-start: 1.25rem;
}

.summary-grid {
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 12rem), 1fr));
}

.resource-grid {
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 20rem), 1fr));
}

.dependency-grid {
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 28rem), 1fr));
}

.summary-card,
.panel {
  min-inline-size: 0;
  background: hsl(var(--card));
  border: 1px solid var(--monitor-line);
  border-radius: 1rem;
  box-shadow: 0 0.5rem 1.5rem rgb(15 23 42 / 5%);
}

.summary-card {
  display: grid;
  gap: 0.4rem;
  padding: 1rem;
}

.summary-card span,
dt {
  font-size: 0.78rem;
  color: var(--monitor-muted);
}

.summary-card strong {
  font-size: 1.05rem;
}

.panel {
  padding: clamp(1rem, 1.5vw, 1.5rem);
  margin-block-start: 1rem;
}

.resource-grid .panel,
.dependency-grid .panel {
  margin-block-start: 0;
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

.detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 11rem), 1fr));
  gap: 0.85rem;
  margin: 1rem 0 0;
}

.detail-grid.compact {
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 9rem), 1fr));
}

.detail-grid div,
.metric-list div {
  min-inline-size: 0;
}

dt {
  margin-block-end: 0.25rem;
}

dd {
  margin: 0;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.code-value {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.82rem;
}

.metric-list {
  display: grid;
  gap: 0.65rem;
  margin: 1rem 0 0;
}

.metric-list div {
  display: flex;
  gap: 1rem;
  justify-content: space-between;
  padding-block-end: 0.55rem;
  border-block-end: 1px solid var(--monitor-line);
}

.metric-message {
  margin: 0.8rem 0 0;
  overflow-wrap: anywhere;
}

.capabilities {
  padding-block-start: 0.8rem;
  margin-block-start: 1rem;
  border-block-start: 1px solid var(--monitor-line);
}

.capabilities summary {
  font-weight: 750;
  cursor: pointer;
}

.capabilities ul {
  display: grid;
  gap: 0.45rem;
  padding: 0;
  margin: 0.7rem 0 0;
  list-style: none;
}

.capabilities li {
  display: flex;
  gap: 1rem;
  justify-content: space-between;
  font-size: 0.78rem;
}

.capabilities li span {
  color: var(--monitor-muted);
  text-align: end;
  overflow-wrap: anywhere;
}

.trend-list {
  display: grid;
  gap: 0.8rem;
  margin-block-start: 1rem;
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
  background: var(--monitor-accent);
  border-radius: inherit;
}

.privacy-note {
  margin: 1rem 0 0;
  font-size: 0.8rem;
}

@media (width <= 700px) {
  .page-heading,
  .heading-actions {
    flex-direction: column;
    align-items: stretch;
  }

  .capabilities li {
    flex-direction: column;
    gap: 0.2rem;
    align-items: flex-start;
  }

  .capabilities li span {
    text-align: start;
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
