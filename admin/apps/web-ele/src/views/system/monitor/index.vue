<script setup lang="ts">
import type { MonitorMetric, MonitorOverview } from '#/api/core/monitor';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { getMonitorOverviewApi } from '#/api/core/monitor';

const overview = ref<MonitorOverview>();
const loading = ref(false);
const error = ref('');
const lastUpdated = ref('');
let timer: number | undefined;

const cards = computed(() => {
  if (!overview.value) return [];
  return [
    { key: 'cpu', title: 'CPU', metric: overview.value.cpu, value: overview.value.cpu.cores ? `${overview.value.cpu.cores} cores` : '—' },
    { key: 'memory', title: 'Memory', metric: overview.value.memory, value: formatBytes(overview.value.memory.usedBytes) },
    { key: 'disk', title: 'Disk', metric: overview.value.disk, value: formatBytes(overview.value.disk.usedBytes) },
    { key: 'database', title: 'MySQL / PostgreSQL', metric: overview.value.database, value: formatLatency(overview.value.database) },
    { key: 'redis', title: 'Redis', metric: overview.value.redis, value: formatLatency(overview.value.redis) },
  ];
});

function formatBytes(value?: number) {
  if (value === undefined) return '—';
  if (value < 1024 * 1024) return `${Math.round(value / 1024)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}
function formatLatency(metric: MonitorMetric) {
  return metric.latencyMs === undefined ? '—' : `${metric.latencyMs.toFixed(1)} ms`;
}
function statusText(status: MonitorMetric['status']) {
  return status === 'ok' ? 'OK' : status === 'degraded' ? 'degraded' : 'unavailable';
}
async function refresh() {
  loading.value = true;
  error.value = '';
  try {
    overview.value = await getMonitorOverviewApi();
    lastUpdated.value = new Date().toLocaleTimeString();
  } catch {
    error.value = '监控快照暂时不可用，请稍后重试';
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  refresh();
  timer = window.setInterval(refresh, 15_000);
});
onBeforeUnmount(() => {
  if (timer !== undefined) window.clearInterval(timer);
});
</script>

<template>
  <main class="monitor-page" :aria-busy="loading" aria-labelledby="monitor-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">OPERATIONS / LIVE SNAPSHOT</p>
        <h1 id="monitor-title">运维监控</h1>
        <p class="description">当前进程/容器范围的 CPU、memory、disk、MySQL、PostgreSQL 与 Redis 状态。</p>
      </div>
      <div class="heading-actions"><span v-if="lastUpdated" class="updated">更新于 {{ lastUpdated }} · 15s</span><button class="refresh" type="button" :disabled="loading" @click="refresh">{{ loading ? '刷新中…' : '手动刷新' }}</button></div>
    </header>

    <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
    <section v-if="overview" class="summary" aria-label="运行摘要"><div><span>运行范围</span><strong>{{ overview.scope }}</strong></div><div><span>运行时长</span><strong>{{ Math.floor(overview.uptimeSeconds / 60) }} min</strong></div><div><span>版本</span><strong>{{ overview.version || '—' }}</strong></div></section>

    <section class="metric-grid" aria-label="系统指标">
      <article v-for="card in cards" :key="card.key" class="metric-card">
        <div class="metric-top"><span class="metric-title">{{ card.title }}</span><span :class="['status', card.metric.status]">{{ statusText(card.metric.status) }}</span></div>
        <strong class="metric-value">{{ card.value }}</strong>
        <div v-if="card.metric.utilization !== undefined" class="meter" aria-hidden="true"><span :style="{ width: `${Math.min(card.metric.utilization * 100, 100)}%` }" /></div>
        <p v-if="card.metric.message" class="metric-message">{{ card.metric.message }}</p>
      </article>
    </section>
    <p v-if="!overview" class="loading-state" role="status">正在读取运维快照…</p>

    <section v-if="overview" class="detail-card" aria-labelledby="monitor-detail-title">
      <h2 id="monitor-detail-title">依赖状态</h2>
      <dl><div><dt>Database</dt><dd>{{ statusText(overview.database.status) }} · {{ formatLatency(overview.database) }}</dd></div><div><dt>Redis</dt><dd>{{ statusText(overview.redis.status) }} · {{ formatLatency(overview.redis) }}</dd></div><div><dt>采集时间</dt><dd>{{ new Date(overview.collectedAt).toLocaleString() }}</dd></div></dl>
      <p class="privacy-note">仅展示非敏感快照；DSN、密码、token 和原始命令不会返回。</p>
    </section>
  </main>
</template>

<style scoped>
.monitor-page { --ink: #172033; --muted: #64748b; --line: #dbe3ef; --accent: #2563eb; max-width: 1440px; margin: 0 auto; padding: 32px; color: var(--ink); }.page-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; }.eyebrow { margin: 0 0 6px; color: #5267d9; font-size: .72rem; font-weight: 800; letter-spacing: .12em; }.page-heading h1 { margin: 0 0 8px; font-size: clamp(1.7rem, 4vw, 2.5rem); }.description, .updated, .metric-message, .privacy-note { color: var(--muted); }.heading-actions { display: flex; align-items: center; gap: 12px; }.refresh { min-height: 40px; border: 1px solid #cbd5e1; border-radius: 9px; padding: 0 14px; cursor: pointer; background: white; transition: transform .18s ease, box-shadow .18s ease; }.refresh:hover { transform: translateY(-1px); box-shadow: 0 5px 14px rgb(15 23 42 / 12%); }.refresh:focus-visible { outline: 3px solid rgb(37 99 235 / 25%); outline-offset: 2px; }.feedback { margin: 20px 0 0; border-radius: 10px; padding: 12px 14px; }.error { color: #8b1e1e; background: #fef2f2; }.summary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; margin-top: 26px; }.summary > div, .metric-card, .detail-card { border: 1px solid var(--line); border-radius: 16px; background: color-mix(in srgb, white 94%, #dbeafe); box-shadow: 0 10px 28px rgb(30 41 59 / 7%); }.summary > div { display: grid; gap: 7px; padding: 18px; }.summary span { color: var(--muted); font-size: .82rem; }.summary strong { font-size: 1.1rem; }.metric-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 14px; margin-top: 18px; }.metric-card { min-height: 150px; padding: 18px; }.metric-top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }.metric-title { color: var(--muted); font-size: .82rem; font-weight: 700; }.status { border-radius: 999px; padding: 4px 8px; font-size: .7rem; font-weight: 800; }.status.ok { color: #166534; background: #dcfce7; }.status.degraded { color: #92400e; background: #fef3c7; }.status.unavailable { color: #991b1b; background: #fee2e2; }.metric-value { display: block; margin-top: 24px; font-size: 1.45rem; }.meter { height: 7px; margin-top: 16px; overflow: hidden; border-radius: 99px; background: #e2e8f0; }.meter span { display: block; height: 100%; border-radius: inherit; background: var(--accent); }.metric-message { margin: 10px 0 0; font-size: .78rem; }.loading-state { margin-top: 26px; color: var(--muted); }.detail-card { margin-top: 18px; padding: 22px; }.detail-card h2 { margin: 0; font-size: 1.1rem; }.detail-card dl { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; margin: 18px 0; }.detail-card dl div { display: grid; gap: 5px; }.detail-card dt { color: var(--muted); font-size: .8rem; }.detail-card dd { margin: 0; font-weight: 700; }.privacy-note { margin: 0; font-size: .82rem; }
@media (max-width: 1000px) { .metric-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 700px) { .monitor-page { padding: 22px 16px; }.page-heading, .heading-actions { align-items: flex-start; flex-direction: column; }.summary, .detail-card dl { grid-template-columns: 1fr; }.metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 450px) { .metric-grid { grid-template-columns: 1fr; } }@media (prefers-reduced-motion: reduce) { *, *::before, *::after { transition-duration: .01ms !important; animation-duration: .01ms !important; } }
</style>
