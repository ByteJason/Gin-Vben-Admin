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

const overview = ref<MonitorOverview>();
const sessions = ref<AuthApi.SessionInfo[]>([]);
const loading = ref(false);
const monitorError = ref('');
const sessionsError = ref('');
const sampledAt = ref(Date.now());

const sessionTrend = computed(() => {
  const buckets = [
    { count: 0, label: '15 分钟内', maxAge: 15 * 60 * 1000 },
    { count: 0, label: '15–60 分钟', maxAge: 60 * 60 * 1000 },
    { count: 0, label: '1–24 小时', maxAge: 24 * 60 * 60 * 1000 },
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

const activeSessions = computed(
  () =>
    sessions.value.filter(
      (session) =>
        !session.revoked && Date.parse(session.expiresAt) > sampledAt.value,
    ).length,
);

function statusText(status: MonitorStatus | undefined) {
  if (status === 'ok') return '正常';
  if (status === 'degraded') return '局部降级';
  return '不可用';
}

function valueText(value: number | string | undefined, suffix = '') {
  return value === undefined || value === '' ? '不可用' : `${value}${suffix}`;
}

function formatBytes(value?: number) {
  if (value === undefined) return '不可用';
  if (value === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const exponent = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  return `${(value / 1024 ** exponent).toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

function formatPercent(value?: number) {
  return value === undefined ? '不可用' : `${(value * 100).toFixed(1)}%`;
}

function formatDuration(seconds?: number) {
  if (seconds === undefined) return '不可用';
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${days} 天 ${hours} 小时 ${minutes} 分钟`;
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
      ? '最新资源快照获取失败，当前显示上次成功数据。'
      : '资源快照暂时不可用，请稍后重试。';
  }
  if (sessionsResult.status === 'fulfilled') {
    sessions.value = sessionsResult.value;
    sessionsError.value = '';
  } else {
    sessionsError.value = '会话活跃度暂时不可用。';
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
        <p class="eyebrow">OPERATIONS / LIVE SNAPSHOT</p>
        <h1 id="monitor-title">资源监控</h1>
        <p class="description">
          展示服务端明确标注范围与采集能力的实时资源、运行时、依赖连接池和会话活跃度。
        </p>
      </div>
      <div class="heading-actions">
        <span v-if="overview" class="updated">
          采集于 {{ new Date(overview.collectedAt).toLocaleString() }} · 每 15
          秒
        </span>
        <button type="button" :disabled="loading" @click="refresh">
          {{ loading ? '刷新中…' : '立即刷新' }}
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
      正在读取资源快照…
    </p>

    <template v-if="overview">
      <section class="summary-grid" aria-label="实例摘要">
        <article class="summary-card">
          <span>采集范围</span><strong>{{ overview.scope }}</strong>
        </article>
        <article class="summary-card">
          <span>运行时长</span><strong>{{ formatDuration(overview.uptimeSeconds) }}</strong>
        </article>
        <article class="summary-card">
          <span>应用版本</span><strong>{{ overview.version || '不可用' }}</strong>
        </article>
        <article class="summary-card">
          <span>活动会话</span><strong>{{ activeSessions }} / {{ sessions.length }}</strong>
        </article>
      </section>

      <section class="panel" aria-labelledby="runtime-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">RUNTIME</p>
            <h2 id="runtime-title">Go 运行时</h2>
          </div>
          <span class="status" :class="[overview.runtime.status]">
            {{ statusText(overview.runtime.status) }}
          </span>
        </div>
        <dl class="detail-grid">
          <div>
            <dt>Go 版本</dt>
            <dd>{{ overview.runtime.goVersion }}</dd>
          </div>
          <div>
            <dt>操作系统</dt>
            <dd>{{ overview.runtime.os }}</dd>
          </div>
          <div>
            <dt>架构</dt>
            <dd>{{ overview.runtime.arch }}</dd>
          </div>
          <div>
            <dt>应用版本</dt>
            <dd>{{ overview.runtime.applicationVersion || '不可用' }}</dd>
          </div>
          <div>
            <dt>Commit</dt>
            <dd class="code-value">
              {{ overview.runtime.commit || '不可用' }}
            </dd>
          </div>
          <div>
            <dt>Heap Alloc</dt>
            <dd>{{ formatBytes(overview.runtime.heapAllocBytes) }}</dd>
          </div>
          <div>
            <dt>Heap Sys</dt>
            <dd>{{ formatBytes(overview.runtime.heapSysBytes) }}</dd>
          </div>
          <div>
            <dt>Heap In Use</dt>
            <dd>{{ formatBytes(overview.runtime.heapInUseBytes) }}</dd>
          </div>
          <div>
            <dt>Heap Objects</dt>
            <dd>{{ valueText(overview.runtime.heapObjects) }}</dd>
          </div>
          <div>
            <dt>Next GC</dt>
            <dd>{{ formatBytes(overview.runtime.nextGcBytes) }}</dd>
          </div>
          <div>
            <dt>GC 次数</dt>
            <dd>{{ valueText(overview.runtime.gcCount) }}</dd>
          </div>
          <div>
            <dt>最近 GC Pause</dt>
            <dd>{{ valueText(overview.runtime.lastGcPauseNs, ' ns') }}</dd>
          </div>
        </dl>
        <details class="capabilities">
          <summary>采集能力</summary>
          <ul>
            <li
              v-for="[name, capability] in capabilityEntries(
                overview.runtime.capabilities,
              )"
              :key="name"
            >
              <strong>{{ name }}</strong>
              <span>{{ capability.available ? '可用' : '不可用' }} ·
                {{ capability.scope
                }}<template v-if="capability.source">
                  · {{ capability.source }}</template></span>
            </li>
          </ul>
        </details>
      </section>

      <section class="resource-grid" aria-label="CPU、内存和文件系统">
        <article
          v-for="resource in [
            { key: 'cpu', title: 'CPU', metric: overview.cpu },
            { key: 'memory', title: '内存', metric: overview.memory },
            { key: 'disk', title: '文件系统', metric: overview.disk },
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
              <dt>逻辑核心</dt>
              <dd>{{ valueText(resource.metric.cores) }}</dd>
            </div>
            <div>
              <dt>Load 1 / 5 / 15</dt>
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
              <dt>已用</dt>
              <dd>{{ formatBytes(resource.metric.usedBytes) }}</dd>
            </div>
            <div>
              <dt>可用</dt>
              <dd>{{ formatBytes(resource.metric.freeBytes) }}</dd>
            </div>
            <div>
              <dt>总量</dt>
              <dd>{{ formatBytes(resource.metric.totalBytes) }}</dd>
            </div>
            <div>
              <dt>使用率</dt>
              <dd>{{ formatPercent(resource.metric.utilization) }}</dd>
            </div>
          </dl>
          <p v-if="resource.metric.message" class="metric-message">
            {{ resource.metric.message }}
          </p>
          <details class="capabilities">
            <summary>采集能力</summary>
            <ul>
              <li
                v-for="[name, capability] in capabilityEntries(
                  resource.metric.capabilities,
                )"
                :key="name"
              >
                <strong>{{ name }}</strong><span>{{ capability.available ? '可用' : '不可用' }} ·
                  {{ capability.scope
                  }}<template v-if="capability.source">
                    · {{ capability.source }}</template></span>
              </li>
            </ul>
          </details>
        </article>
      </section>

      <section class="dependency-grid" aria-label="数据库与 Redis">
        <article class="panel">
          <div class="section-heading">
            <div>
              <p class="eyebrow">DATABASE</p>
              <h2>数据库</h2>
            </div>
            <span class="status" :class="[overview.database.status]">{{
              statusText(overview.database.status)
            }}</span>
          </div>
          <dl class="detail-grid compact">
            <div>
              <dt>Driver</dt>
              <dd>{{ overview.database.driver || '不可用' }}</dd>
            </div>
            <div>
              <dt>模式</dt>
              <dd>{{ overview.database.mode || '不可用' }}</dd>
            </div>
            <div>
              <dt>延迟</dt>
              <dd>{{ valueText(overview.database.latencyMs, ' ms') }}</dd>
            </div>
            <template v-if="overview.database.pool">
              <div>
                <dt>Open</dt>
                <dd>{{ overview.database.pool.open }}</dd>
              </div>
              <div>
                <dt>In Use</dt>
                <dd>{{ overview.database.pool.inUse }}</dd>
              </div>
              <div>
                <dt>Idle</dt>
                <dd>{{ overview.database.pool.idle }}</dd>
              </div>
              <div>
                <dt>Max</dt>
                <dd>
                  {{
                    overview.database.pool.max === 0
                      ? '0（无限制）'
                      : overview.database.pool.max
                  }}
                </dd>
              </div>
              <div>
                <dt>Wait Count</dt>
                <dd>{{ overview.database.pool.waitCount }}</dd>
              </div>
              <div>
                <dt>Wait Duration</dt>
                <dd>{{ overview.database.pool.waitDurationMs }} ms</dd>
              </div>
              <div>
                <dt>Max Idle Closed</dt>
                <dd>{{ overview.database.pool.maxIdleClosed }}</dd>
              </div>
              <div>
                <dt>Max Idle Time Closed</dt>
                <dd>{{ overview.database.pool.maxIdleTimeClosed }}</dd>
              </div>
              <div>
                <dt>Max Lifetime Closed</dt>
                <dd>{{ overview.database.pool.maxLifetimeClosed }}</dd>
              </div>
            </template>
          </dl>
          <p v-if="overview.database.message" class="metric-message">
            {{ overview.database.message }}
          </p>
          <details class="capabilities">
            <summary>采集能力</summary>
            <ul>
              <li
                v-for="[name, capability] in capabilityEntries(
                  overview.database.capabilities,
                )"
                :key="name"
              >
                <strong>{{ name }}</strong><span>{{ capability.available ? '可用' : '不可用' }} ·
                  {{ capability.scope
                  }}<template v-if="capability.source">
                    · {{ capability.source }}</template></span>
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
              <dt>模式</dt>
              <dd>{{ overview.redis.mode || '不可用' }}</dd>
            </div>
            <div>
              <dt>延迟</dt>
              <dd>{{ valueText(overview.redis.latencyMs, ' ms') }}</dd>
            </div>
            <div>
              <dt>Keyspace</dt>
              <dd>{{ valueText(overview.redis.keyspace) }}</dd>
            </div>
            <template v-if="overview.redis.pool">
              <div>
                <dt>Max</dt>
                <dd>{{ valueText(overview.redis.pool.max) }}</dd>
              </div>
              <div>
                <dt>Total</dt>
                <dd>{{ overview.redis.pool.total }}</dd>
              </div>
              <div>
                <dt>Active</dt>
                <dd>{{ overview.redis.pool.active }}</dd>
              </div>
              <div>
                <dt>Idle</dt>
                <dd>{{ overview.redis.pool.idle }}</dd>
              </div>
              <div>
                <dt>Hits</dt>
                <dd>{{ overview.redis.pool.hits }}</dd>
              </div>
              <div>
                <dt>Misses</dt>
                <dd>{{ overview.redis.pool.misses }}</dd>
              </div>
              <div>
                <dt>Timeouts</dt>
                <dd>{{ overview.redis.pool.timeouts }}</dd>
              </div>
              <div>
                <dt>Wait Count</dt>
                <dd>{{ overview.redis.pool.waitCount }}</dd>
              </div>
              <div>
                <dt>Wait Duration</dt>
                <dd>{{ overview.redis.pool.waitDurationMs }} ms</dd>
              </div>
              <div>
                <dt>Stale</dt>
                <dd>{{ overview.redis.pool.stale }}</dd>
              </div>
              <div>
                <dt>Pending</dt>
                <dd>{{ overview.redis.pool.pending }}</dd>
              </div>
            </template>
          </dl>
          <p v-if="overview.redis.message" class="metric-message">
            {{ overview.redis.message }}
          </p>
          <details class="capabilities">
            <summary>采集能力</summary>
            <ul>
              <li
                v-for="[name, capability] in capabilityEntries(
                  overview.redis.capabilities,
                )"
                :key="name"
              >
                <strong>{{ name }}</strong><span>{{ capability.available ? '可用' : '不可用' }} ·
                  {{ capability.scope
                  }}<template v-if="capability.source">
                    · {{ capability.source }}</template></span>
              </li>
            </ul>
          </details>
        </article>
      </section>

      <section class="panel" aria-labelledby="session-trend-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">AUTH SESSIONS</p>
            <h2 id="session-trend-title">会话活跃度</h2>
          </div>
          <span class="muted">当前 {{ sessions.length }} 个设备会话</span>
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
        <p v-else class="empty-state">当前没有设备会话数据。</p>
      </section>
    </template>

    <p class="privacy-note">
      页面只展示非敏感运行指标；地址、DSN、密码、令牌、命令和本机目录不进入响应。
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
