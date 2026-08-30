<script setup lang="ts">
import type {
  DashboardOverview,
  DashboardOverviewDistributionItem,
  DashboardOverviewPreset,
  DashboardOverviewTopItem,
} from '@vben/api-client';

import { computed, reactive, ref } from 'vue';

import { ManagementPage } from '@vben/common-ui';
import { useVisibilityPolling } from '@vben/hooks';

import {
  getDashboardOverviewApi,
  type DashboardOverviewQuery,
} from '#/api/core/dashboard';
import { $t } from '#/locales';

const presets: Array<{ key: DashboardOverviewPreset; label: string }> = [
  { key: 'today', label: 'today' },
  { key: 'yesterday', label: 'yesterday' },
  { key: '24h', label: '24h' },
  { key: '7d', label: '7d' },
  { key: '14d', label: '14d' },
  { key: '30d', label: '30d' },
  { key: 'this_month', label: 'thisMonth' },
  { key: 'last_month', label: 'lastMonth' },
];

type CardKey = keyof DashboardOverview['cards'];
type TrendKey = 'visitors' | 'newUsers' | 'amount';

const state = reactive<{
  from: string;
  granularity: 'hour' | 'day';
  preset: DashboardOverviewPreset;
  to: string;
}>({
  from: '',
  granularity: 'hour',
  preset: 'today',
  to: '',
});
const overview = ref<DashboardOverview>();
const loading = ref(false);
const error = ref('');
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC');

const cardDefinitions: Array<{
  key: CardKey;
  label: string;
  tone: string;
  currency?: boolean;
}> = [
  { key: 'visitors', label: 'visitors', tone: 'blue' },
  { key: 'newUsers', label: 'newUsers', tone: 'green' },
  {
    key: 'paymentAmount',
    label: 'paymentAmount',
    tone: 'amber',
    currency: true,
  },
  { key: 'paymentOrders', label: 'paymentOrders', tone: 'violet' },
  {
    key: 'averageOrderValue',
    label: 'averageOrderValue',
    tone: 'teal',
    currency: true,
  },
];

const cards = computed(() =>
  cardDefinitions.map((definition) => ({
    ...definition,
    metric: overview.value?.cards[definition.key],
  })),
);

const distributionStyle = computed(() => {
  const items = overview.value?.distribution ?? [];
  const total = items.reduce((sum, item) => sum + Math.max(0, item.value), 0);
  if (!total) return 'conic-gradient(#dbe3ef 0 100%)';
  let cursor = 0;
  const colors = ['#2563eb', '#10b981', '#f59e0b', '#ec4899', '#8b5cf6'];
  return `conic-gradient(${items
    .map((item, index) => {
      const start = cursor;
      cursor += (Math.max(0, item.value) / total) * 100;
      return `${colors[index % colors.length]} ${start}% ${cursor}%`;
    })
    .join(', ')})`;
});

const trendPoints = computed(() => overview.value?.trends ?? []);

function label(key: string, params?: Record<string, unknown>) {
  const path = `page.analyticsOverview.${key}`;
  return String(params ? $t(path, params) : $t(path));
}

function metricText(
  metric: { value?: number; status: string } | undefined,
  currency = false,
) {
  if (!metric || metric.value === undefined) return label('unavailable');
  const value = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: currency ? 2 : 0,
    minimumFractionDigits: currency ? 2 : 0,
  }).format(metric.value);
  return currency ? `¥ ${value}` : value;
}

function statusText(status?: string) {
  if (status === 'ok') return label('healthy');
  if (status === 'degraded') return label('degraded');
  return label('unavailable');
}

function formatDate(value?: string) {
  if (!value) return label('unavailable');
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? label('unavailable')
    : date.toLocaleString();
}

function trendPath(key: TrendKey) {
  const points = trendPoints.value;
  if (!points.length) return '';
  const values = points.map((point) => point[key]);
  const max = Math.max(1, ...values);
  const width = 640;
  const height = 190;
  return values
    .map((value, index) => {
      const x =
        points.length === 1 ? width / 2 : (index / (points.length - 1)) * width;
      const y = height - (value / max) * (height - 18) - 9;
      return `${index === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(' ');
}

function trendAreaPath(key: TrendKey) {
  const path = trendPath(key);
  if (!path) return '';
  return `${path} L 640 190 L 0 190 Z`;
}

function distributionPercent(item: DashboardOverviewDistributionItem) {
  const total = (overview.value?.distribution ?? []).reduce(
    (sum, entry) => sum + Math.max(0, entry.value),
    0,
  );
  return total ? `${((item.value / total) * 100).toFixed(1)}%` : '0%';
}

function topItemAmount(item: DashboardOverviewTopItem) {
  return metricText({ value: item.amount, status: 'ok' }, true);
}

function dateAfter(value: string) {
  const parsed = new Date(`${value}T00:00:00`);
  if (Number.isNaN(parsed.getTime())) return value;
  parsed.setDate(parsed.getDate() + 1);
  return parsed.toISOString();
}

function queryParams(): DashboardOverviewQuery | undefined {
  if (state.preset !== 'custom') {
    return {
      granularity: state.granularity,
      preset: state.preset,
      timezone: timezone.value,
    };
  }
  if (!state.from || !state.to) {
    error.value = label('customRangeRequired');
    return;
  }
  const from = new Date(`${state.from}T00:00:00`);
  if (Number.isNaN(from.getTime())) {
    error.value = label('customRangeInvalid');
    return;
  }
  return {
    from: from.toISOString(),
    granularity: state.granularity,
    preset: 'custom',
    timezone: timezone.value,
    to: dateAfter(state.to),
  };
}

async function refresh() {
  if (loading.value) return;
  const params = queryParams();
  if (!params) return;
  loading.value = true;
  error.value = '';
  try {
    overview.value = await getDashboardOverviewApi(params);
  } catch {
    error.value = label('loadError');
  } finally {
    loading.value = false;
  }
}

function selectPreset(preset: DashboardOverviewPreset) {
  state.preset = preset;
  if (preset !== 'custom') {
    state.from = '';
    state.to = '';
  }
  void refresh();
}

useVisibilityPolling(refresh, 30_000);
</script>

<template>
  <ManagementPage
    class="analytics-overview-page"
    :aria-busy="loading"
    aria-labelledby="analytics-overview-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ label('eyebrow') }}</p>
        <h1 id="analytics-overview-title">{{ label('title') }}</h1>
        <p class="description">{{ label('description') }}</p>
      </div>
      <div class="heading-meta">
        <span v-if="overview" class="source-badge" :class="overview.dataSource">
          <span class="status-dot" aria-hidden="true"></span>
          {{
            overview.dataSource === 'fixture' ? label('fixture') : label('live')
          }}
        </span>
        <span v-if="overview" class="updated">{{
          formatDate(overview.collectedAt)
        }}</span>
        <button type="button" :disabled="loading" @click="refresh">
          {{ loading ? label('refreshing') : label('refresh') }}
        </button>
      </div>
    </header>

    <section class="range-toolbar" :aria-label="label('rangeLabel')">
      <div class="preset-list" role="group" :aria-label="label('quickRanges')">
        <button
          v-for="item in presets"
          :key="item.key"
          type="button"
          :class="{ active: state.preset === item.key }"
          :aria-pressed="state.preset === item.key"
          @click="selectPreset(item.key)"
        >
          {{ label(item.label) }}
        </button>
        <button
          type="button"
          :class="{ active: state.preset === 'custom' }"
          :aria-pressed="state.preset === 'custom'"
          @click="selectPreset('custom')"
        >
          {{ label('custom') }}
        </button>
      </div>
      <div v-if="state.preset === 'custom'" class="date-fields">
        <label>
          <span>{{ label('from') }}</span>
          <input v-model="state.from" type="date" @change="refresh" />
        </label>
        <span class="date-separator" aria-hidden="true">→</span>
        <label>
          <span>{{ label('to') }}</span>
          <input v-model="state.to" type="date" @change="refresh" />
        </label>
      </div>
      <label class="granularity-field">
        <span>{{ label('granularity') }}</span>
        <select v-model="state.granularity" @change="refresh">
          <option value="hour">{{ label('hour') }}</option>
          <option value="day">{{ label('day') }}</option>
        </select>
      </label>
    </section>

    <p v-if="error" class="feedback error" role="alert">{{ error }}</p>
    <p v-if="loading && !overview" class="loading-state" role="status">
      {{ label('loading') }}
    </p>

    <template v-if="overview">
      <section class="metric-grid" :aria-label="label('metrics')">
        <article
          v-for="card in cards"
          :key="card.key"
          class="metric-card"
          :class="`tone-${card.tone}`"
        >
          <div class="metric-heading">
            <span>{{ label(card.label) }}</span>
            <span class="metric-status" :class="card.metric?.status">
              {{ statusText(card.metric?.status) }}
            </span>
          </div>
          <strong>{{ metricText(card.metric, card.currency) }}</strong>
          <small>{{
            label('rangeHint', {
              from: formatDate(overview.range.from),
              to: formatDate(overview.range.to),
            })
          }}</small>
        </article>
      </section>

      <section class="chart-grid" aria-label="Trends">
        <article class="panel chart-panel">
          <div class="panel-heading">
            <div>
              <p class="eyebrow">{{ label('trend') }}</p>
              <h2>{{ label('visitorsTrend') }}</h2>
            </div>
            <span class="legend blue">{{ label('visitors') }}</span>
          </div>
          <svg
            class="trend-chart"
            viewBox="0 0 640 190"
            role="img"
            :aria-label="label('visitorsTrend')"
          >
            <path class="area blue-fill" :d="trendAreaPath('visitors')" />
            <path class="line blue-line" :d="trendPath('visitors')" />
          </svg>
          <div class="axis-labels">
            <span>{{ formatDate(trendPoints[0]?.at) }}</span
            ><span>{{
              formatDate(trendPoints[trendPoints.length - 1]?.at)
            }}</span>
          </div>
        </article>
        <article class="panel chart-panel">
          <div class="panel-heading">
            <div>
              <p class="eyebrow">{{ label('trend') }}</p>
              <h2>{{ label('paymentTrend') }}</h2>
            </div>
            <span class="legend amber">{{ label('paymentAmount') }}</span>
          </div>
          <svg
            class="trend-chart"
            viewBox="0 0 640 190"
            role="img"
            :aria-label="label('paymentTrend')"
          >
            <path class="area amber-fill" :d="trendAreaPath('amount')" />
            <path class="line amber-line" :d="trendPath('amount')" />
          </svg>
          <div class="axis-labels">
            <span>{{ formatDate(trendPoints[0]?.at) }}</span
            ><span>{{
              formatDate(trendPoints[trendPoints.length - 1]?.at)
            }}</span>
          </div>
        </article>
        <article class="panel chart-panel compact-chart">
          <div class="panel-heading">
            <div>
              <p class="eyebrow">{{ label('trend') }}</p>
              <h2>{{ label('newUsersTrend') }}</h2>
            </div>
            <span class="legend green">{{ label('newUsers') }}</span>
          </div>
          <svg
            class="trend-chart"
            viewBox="0 0 640 190"
            role="img"
            :aria-label="label('newUsersTrend')"
          >
            <path class="area green-fill" :d="trendAreaPath('newUsers')" />
            <path class="line green-line" :d="trendPath('newUsers')" />
          </svg>
        </article>
        <article class="panel range-panel">
          <p class="eyebrow">{{ label('selectedRange') }}</p>
          <h2>
            {{ formatDate(overview.range.from) }} —
            {{ formatDate(overview.range.to) }}
          </h2>
          <dl>
            <div>
              <dt>{{ label('timezone') }}</dt>
              <dd>{{ overview.range.timezone }}</dd>
            </div>
            <div>
              <dt>{{ label('granularity') }}</dt>
              <dd>{{ overview.range.granularity }}</dd>
            </div>
            <div>
              <dt>{{ label('points') }}</dt>
              <dd>{{ overview.trends.length }}</dd>
            </div>
          </dl>
        </article>
      </section>

      <section class="lower-grid">
        <article class="panel distribution-panel">
          <div class="panel-heading">
            <h2>{{ label('channelDistribution') }}</h2>
            <span>{{ label('distributionHint') }}</span>
          </div>
          <div class="distribution-content">
            <div
              class="donut"
              :style="{ background: distributionStyle }"
              role="img"
              :aria-label="label('channelDistribution')"
            >
              <span>{{ metricText(overview.cards.visitors) }}</span
              ><small>{{ label('visitors') }}</small>
            </div>
            <ul class="legend-list">
              <li
                v-for="(item, index) in overview.distribution"
                :key="item.key"
              >
                <span
                  class="swatch"
                  :style="{
                    background: [
                      '#2563eb',
                      '#10b981',
                      '#f59e0b',
                      '#ec4899',
                      '#8b5cf6',
                    ][index % 5],
                  }"
                ></span
                ><span>{{ item.label }}</span
                ><strong>{{ distributionPercent(item) }}</strong>
              </li>
            </ul>
          </div>
        </article>
        <article class="panel top-items-panel">
          <div class="panel-heading">
            <h2>{{ label('topItems') }}</h2>
            <span>{{ label('amount') }}</span>
          </div>
          <ol class="top-items">
            <li v-for="item in overview.topItems" :key="item.id">
              <span class="rank">{{ item.rank }}</span
              ><span class="item-name">{{ item.name }}</span
              ><strong>{{ topItemAmount(item) }}</strong>
            </li>
            <li v-if="!overview.topItems.length" class="empty-inline">
              {{ label('empty') }}
            </li>
          </ol>
        </article>
        <article class="panel regions-panel">
          <div class="panel-heading">
            <h2>{{ label('regions') }}</h2>
            <span>{{ label('visitors') }}</span>
          </div>
          <table>
            <caption class="sr-only">
              {{
                label('regions')
              }}
            </caption>
            <thead>
              <tr>
                <th>{{ label('region') }}</th>
                <th>{{ label('value') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in overview.regions" :key="item.key">
                <td>{{ item.label }}</td>
                <td>{{ item.value }}</td>
              </tr>
              <tr v-if="!overview.regions.length">
                <td colspan="2" class="empty-inline">{{ label('empty') }}</td>
              </tr>
            </tbody>
          </table>
        </article>
        <article class="panel announcements-panel">
          <div class="panel-heading">
            <h2>{{ label('announcements') }}</h2>
            <span>{{ label('latest') }}</span>
          </div>
          <ul class="announcements">
            <li v-for="item in overview.announcements" :key="item.id">
              <span>{{ item.title }}</span
              ><time :datetime="item.publishedAt">{{
                formatDate(item.publishedAt)
              }}</time>
            </li>
            <li v-if="!overview.announcements.length" class="empty-inline">
              {{ label('empty') }}
            </li>
          </ul>
        </article>
      </section>
    </template>
  </ManagementPage>
</template>

<style scoped>
.analytics-overview-page {
  --line: hsl(var(--border));
  --muted: hsl(var(--muted-foreground));

  color: hsl(var(--foreground));
}

.page-heading,
.heading-meta,
.range-toolbar,
.preset-list,
.date-fields,
.panel-heading,
.axis-labels,
.distribution-content,
.legend-list li,
.top-items li,
.announcements li {
  display: flex;
  align-items: center;
}

.page-heading,
.range-toolbar,
.panel-heading {
  gap: 1rem;
  justify-content: space-between;
}

.page-heading {
  flex-wrap: wrap;
}

.heading-meta {
  flex-wrap: wrap;
  gap: 0.65rem;
  justify-content: flex-end;
}

.eyebrow {
  margin: 0 0 0.35rem;
  font-size: 0.72rem;
  font-weight: 800;
  color: hsl(var(--primary));
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

h1,
h2,
p {
  margin: 0;
}

h1 {
  font-size: clamp(1.7rem, 3vw, 2.5rem);
}

h2 {
  font-size: 1.08rem;
}

.description {
  max-inline-size: 70ch;
  margin-block-start: 0.5rem;
  color: var(--muted);
}

.updated,
.panel-heading span,
.axis-labels,
.metric-card small,
.range-panel dt {
  font-size: 0.78rem;
  color: var(--muted);
}

button,
input,
select {
  min-block-size: 2.45rem;
  padding: 0.45rem 0.75rem;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid var(--line);
  border-radius: 0.6rem;
}

button {
  cursor: pointer;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

button:focus-visible,
input:focus-visible,
select:focus-visible {
  outline: 3px solid color-mix(in srgb, hsl(var(--primary)) 30%, transparent);
  outline-offset: 2px;
}

.heading-meta button {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

.source-badge {
  display: inline-flex;
  gap: 0.4rem;
  align-items: center;
  padding: 0.35rem 0.6rem;
  font-size: 0.75rem;
  font-weight: 750;
  border-radius: 999px;
}

.source-badge.fixture {
  color: #92400e;
  background: #fef3c7;
}

.source-badge.live {
  color: #166534;
  background: #dcfce7;
}

.status-dot {
  inline-size: 0.5rem;
  block-size: 0.5rem;
  background: currentcolor;
  border-radius: 50%;
}

.range-toolbar {
  flex-wrap: wrap;
  padding: 1rem;
  margin-block-start: 1.25rem;
  background: hsl(var(--card));
  border: 1px solid var(--line);
  border-radius: 1rem;
}

.preset-list {
  flex-wrap: wrap;
  gap: 0.35rem;
}

.preset-list button {
  min-block-size: 2.2rem;
  border-radius: 0.45rem;
}

.preset-list button.active {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

.date-fields {
  gap: 0.5rem;
}

.date-fields label,
.granularity-field {
  display: grid;
  gap: 0.25rem;
  font-size: 0.75rem;
  color: var(--muted);
}

.date-separator {
  color: var(--muted);
}

.granularity-field {
  min-inline-size: 8rem;
}

.feedback {
  padding: 0.75rem 1rem;
  margin-block-start: 1rem;
  color: hsl(var(--destructive));
  background: color-mix(in srgb, hsl(var(--destructive)) 10%, hsl(var(--card)));
  border-radius: 0.7rem;
}

.loading-state {
  padding: 2rem;
  color: var(--muted);
  text-align: center;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 0.85rem;
  margin-block-start: 1rem;
}

.metric-card,
.panel {
  background: hsl(var(--card));
  border: 1px solid var(--line);
  border-radius: 1rem;
  box-shadow: 0 8px 24px rgb(15 23 42 / 4%);
}

.metric-card {
  position: relative;
  padding: 1rem;
  overflow: hidden;
}

.metric-card::before {
  position: absolute;
  inset-block-start: 0;
  inset-inline: 0;
  block-size: 3px;
  content: '';
  background: var(--tone);
}

.metric-card strong {
  display: block;
  margin-block: 0.75rem 0.3rem;
  font-size: clamp(1.35rem, 2.6vw, 2rem);
  letter-spacing: -0.03em;
}

.metric-heading {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  justify-content: space-between;
}

.metric-heading > span:first-child {
  font-size: 0.8rem;
  color: var(--muted);
}

.metric-status {
  font-size: 0.68rem;
  font-weight: 750;
}

.metric-status.ok {
  color: #15803d;
}

.metric-status.degraded {
  color: #b45309;
}

.metric-status.unavailable {
  color: #64748b;
}

.tone-blue {
  --tone: #2563eb;
}

.tone-green {
  --tone: #10b981;
}

.tone-amber {
  --tone: #f59e0b;
}

.tone-violet {
  --tone: #8b5cf6;
}

.tone-teal {
  --tone: #0d9488;
}

.chart-grid,
.lower-grid {
  display: grid;
  gap: 0.85rem;
  margin-block-start: 0.85rem;
}

.chart-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.lower-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.panel {
  padding: 1rem;
}

.chart-panel {
  min-inline-size: 0;
}

.chart-panel:nth-child(3),
.range-panel {
  grid-column: span 1;
}

.trend-chart {
  display: block;
  inline-size: 100%;
  block-size: 12rem;
  margin-block-start: 0.75rem;
  overflow: visible;
  background: linear-gradient(
    to bottom,
    transparent 24%,
    color-mix(in srgb, var(--line) 70%, transparent) 25%,
    transparent 26%,
    transparent 49%,
    color-mix(in srgb, var(--line) 70%, transparent) 50%,
    transparent 51%,
    transparent 74%,
    color-mix(in srgb, var(--line) 70%, transparent) 75%,
    transparent 76%
  );
  border-radius: 0.6rem;
}

.line {
  fill: none;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.area {
  opacity: 0.18;
}

.blue-line {
  stroke: #2563eb;
}

.blue-fill {
  fill: #2563eb;
}

.amber-line {
  stroke: #f59e0b;
}

.amber-fill {
  fill: #f59e0b;
}

.green-line {
  stroke: #10b981;
}

.green-fill {
  fill: #10b981;
}

.legend {
  display: inline-flex;
  gap: 0.35rem;
  align-items: center;
  font-size: 0.75rem;
}

.legend::before {
  inline-size: 0.55rem;
  block-size: 0.55rem;
  content: '';
  background: currentcolor;
  border-radius: 50%;
}

.legend.blue {
  color: #2563eb;
}

.legend.amber {
  color: #b45309;
}

.legend.green {
  color: #059669;
}

.axis-labels {
  gap: 1rem;
  justify-content: space-between;
  margin-block-start: 0.35rem;
}

.range-panel dl {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0.65rem;
  margin-block-start: 1.3rem;
}

.range-panel dd {
  margin: 0.25rem 0 0;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.distribution-content {
  gap: 1.25rem;
  align-items: center;
  margin-block-start: 1rem;
}

.donut {
  display: grid;
  flex: 0 0 10rem;
  place-content: center;
  aspect-ratio: 1;
  text-align: center;
  border-radius: 50%;
}

.donut::before {
  position: absolute;
  inline-size: 7rem;
  block-size: 7rem;
  content: '';
  background: hsl(var(--card));
  border-radius: 50%;
}

.donut span,
.donut small {
  position: relative;
  z-index: 1;
}

.donut span {
  font-size: 1.3rem;
  font-weight: 800;
}

.donut small {
  color: var(--muted);
}

.legend-list {
  display: grid;
  flex: 1;
  gap: 0.65rem;
  padding: 0;
  margin: 0;
  list-style: none;
}

.legend-list li {
  gap: 0.5rem;
}

.legend-list strong {
  margin-inline-start: auto;
}

.top-items,
.announcements {
  display: grid;
  gap: 0.2rem;
  padding: 0;
  margin: 1rem 0 0;
  list-style: none;
}

.top-items li,
.announcements li {
  gap: 0.65rem;
  padding: 0.65rem 0;
  border-block-end: 1px solid var(--line);
}

.rank {
  display: grid;
  place-content: center;
  inline-size: 1.5rem;
  block-size: 1.5rem;
  font-size: 0.75rem;
  font-weight: 800;
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-radius: 0.4rem;
}

.item-name {
  flex: 1;
}

.announcements time {
  margin-inline-start: auto;
  font-size: 0.75rem;
  color: var(--muted);
  white-space: nowrap;
}

.empty-inline {
  color: var(--muted);
  text-align: center;
}

table {
  inline-size: 100%;
  margin-block-start: 0.75rem;
  border-collapse: collapse;
}

th,
td {
  padding: 0.65rem 0.35rem;
  text-align: start;
  border-block-end: 1px solid var(--line);
}

th {
  font-size: 0.75rem;
  color: var(--muted);
}

.swatch {
  flex: 0 0 auto;
  inline-size: 0.65rem;
  block-size: 0.65rem;
  border-radius: 50%;
}

.sr-only {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip-path: inset(50%);
}

@media (width <= 1100px) {
  .metric-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (width <= 760px) {
  .metric-grid,
  .chart-grid,
  .lower-grid {
    grid-template-columns: 1fr;
  }

  .chart-panel:nth-child(3),
  .range-panel {
    grid-column: auto;
  }

  .range-toolbar,
  .heading-meta {
    flex-direction: column;
    align-items: stretch;
  }

  .date-fields {
    align-items: end;
  }

  .date-fields label,
  .granularity-field {
    inline-size: 100%;
  }

  .date-separator {
    display: none;
  }

  .distribution-content {
    flex-direction: column;
    align-items: flex-start;
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
