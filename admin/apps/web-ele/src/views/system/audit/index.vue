<script setup lang="ts">
import type {
  AuditCategory,
  AuditEvent,
  AuditPage,
  AuditQuery,
} from '#/api/core/audit';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import { ManagementPage } from '@vben/common-ui';

import {
  exportAuditEventsApi,
  queryAuditEventsApi,
  retentionDryRunApi,
} from '#/api/core/audit';
import { $t } from '#/locales';

const categories: Array<'' | AuditCategory> = [
  '',
  'login',
  'operation',
  'system',
];
const state = reactive({
  action: '',
  actorId: '',
  category: '' as '' | AuditCategory,
  from: '',
  limit: 50,
  offset: 0,
  outcome: '',
  requestId: '',
  resource: '',
  to: '',
});
const page = ref<AuditPage>({ items: [], limit: 50, offset: 0, total: 0 });
const loading = ref(false);
const exporting = ref('');
const error = ref('');
const message = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const retentionDays = ref(180);
const retentionLoading = ref(false);
const retentionReport = ref<{
  cutoff: string;
  matchingCount: number;
  retentionDays: number;
}>();

const currentPage = computed(() => Math.floor(state.offset / state.limit) + 1);
const totalPages = computed(() =>
  Math.max(1, Math.ceil(page.value.total / state.limit)),
);

function categoryLabel(category: '' | AuditCategory) {
  return $t(`page.audit.category.${category || 'all'}`);
}

function toQuery(): AuditQuery {
  return {
    action: state.action.trim() || undefined,
    actorId: state.actorId.trim() || undefined,
    category: state.category || undefined,
    from: state.from ? new Date(state.from).toISOString() : undefined,
    limit: state.limit,
    offset: state.offset,
    outcome: state.outcome.trim() || undefined,
    requestId: state.requestId.trim() || undefined,
    resource: state.resource.trim() || undefined,
    to: state.to ? new Date(state.to).toISOString() : undefined,
  };
}

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function load() {
  loading.value = true;
  error.value = '';
  message.value = '';
  try {
    page.value = await queryAuditEventsApi(toQuery());
  } catch {
    error.value = String($t('page.audit.loadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  state.offset = 0;
  void load();
}

function resetFilters() {
  state.action = '';
  state.actorId = '';
  state.category = '';
  state.from = '';
  state.offset = 0;
  state.outcome = '';
  state.requestId = '';
  state.resource = '';
  state.to = '';
  void load();
}

function changePage(delta: number) {
  const next = currentPage.value + delta;
  if (next < 1 || next > totalPages.value || loading.value) return;
  state.offset = (next - 1) * state.limit;
  void load();
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

function formatDetails(event: AuditEvent) {
  const details = event.details ?? {};
  const redacted = Object.fromEntries(
    Object.entries(details).map(([key, value]) =>
      /password|secret|token|authorization|api[_-]?key/i.test(key)
        ? [key, '[REDACTED]']
        : [key, value],
    ),
  );
  return JSON.stringify(redacted);
}

function downloadBlob(blob: Blob, format: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `audit-events.${format}`;
  anchor.click();
  URL.revokeObjectURL(url);
}

async function exportEvents(format: 'csv' | 'json') {
  exporting.value = format;
  error.value = '';
  message.value = '';
  try {
    const blob = await exportAuditEventsApi(toQuery(), format);
    downloadBlob(blob, format);
    message.value = String($t('page.audit.exported'));
  } catch {
    error.value = String($t('page.audit.exportError'));
    await focusError();
  } finally {
    exporting.value = '';
  }
}

async function runRetentionDryRun() {
  retentionLoading.value = true;
  error.value = '';
  try {
    retentionReport.value = await retentionDryRunApi(retentionDays.value);
    message.value = String($t('page.audit.retentionDone'));
  } catch {
    error.value = String($t('page.audit.retentionError'));
    await focusError();
  } finally {
    retentionLoading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <ManagementPage
    class="audit-page"
    :aria-busy="loading"
    aria-labelledby="audit-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.audit.eyebrow') }}</p>
        <h1 id="audit-title">{{ $t('page.audit.title') }}</h1>
        <p class="description">{{ $t('page.audit.description') }}</p>
      </div>
      <span class="retention-chip">{{ $t('page.audit.retentionPolicy') }}</span>
    </header>

    <p
      v-if="error"
      ref="errorSummary"
      class="feedback feedback-error"
      role="alert"
      tabindex="-1"
    >
      {{ error }}
    </p>
    <p v-if="message" class="feedback feedback-success" aria-live="polite">
      {{ message }}
    </p>
    <p class="sr-status" aria-live="polite">
      {{ loading ? $t('page.audit.loading') : '' }}
    </p>

    <section class="filter-card" aria-labelledby="audit-filter-title">
      <div class="table-heading">
        <h2 id="audit-filter-title">{{ $t('page.audit.filters') }}</h2>
        <div class="actions">
          <button type="button" :disabled="loading" @click="resetFilters">
            {{ $t('page.audit.reset') }}
          </button>
          <button
            class="primary"
            type="button"
            :disabled="loading"
            @click="applyFilters"
          >
            {{ $t('page.audit.apply') }}
          </button>
        </div>
      </div>
      <nav class="category-tabs" :aria-label="$t('page.audit.categoryLabel')">
        <button
          v-for="category in categories"
          :key="category || 'all'"
          :aria-pressed="state.category === category"
          type="button"
          @click="state.category = category"
        >
          {{ categoryLabel(category) }}
        </button>
      </nav>
      <div class="field-grid">
        <label class="field">
          <span>{{ $t('page.audit.actorId') }}</span>
          <input v-model.trim="state.actorId" type="text" />
        </label>
        <label class="field">
          <span>{{ $t('page.audit.action') }}</span>
          <input v-model.trim="state.action" type="text" />
        </label>
        <label class="field">
          <span>{{ $t('page.audit.resource') }}</span>
          <input v-model.trim="state.resource" type="text" />
        </label>
        <label class="field">
          <span>{{ $t('page.audit.outcome') }}</span>
          <input v-model.trim="state.outcome" type="text" />
        </label>
        <label class="field field-wide">
          <span>{{ $t('page.audit.requestId') }}</span>
          <input v-model.trim="state.requestId" type="text" />
        </label>
        <label class="field">
          <span>{{ $t('page.audit.from') }}</span>
          <input v-model="state.from" type="datetime-local" />
        </label>
        <label class="field">
          <span>{{ $t('page.audit.to') }}</span>
          <input v-model="state.to" type="datetime-local" />
        </label>
      </div>
    </section>

    <section class="table-card" aria-labelledby="audit-table-title">
      <div class="table-heading">
        <div>
          <h2 id="audit-table-title">{{ $t('page.audit.tableTitle') }}</h2>
          <span class="result-count">{{ page.total }}</span>
        </div>
        <div class="actions">
          <button
            type="button"
            :disabled="Boolean(exporting)"
            @click="exportEvents('csv')"
          >
            {{
              exporting === 'csv'
                ? $t('page.audit.exporting')
                : $t('page.audit.exportCSV')
            }}
          </button>
          <button
            type="button"
            :disabled="Boolean(exporting)"
            @click="exportEvents('json')"
          >
            {{
              exporting === 'json'
                ? $t('page.audit.exporting')
                : $t('page.audit.exportJSON')
            }}
          </button>
        </div>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.audit.tableLabel')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.audit.categoryHeader') }}</th>
              <th scope="col">{{ $t('page.audit.actorId') }}</th>
              <th scope="col">{{ $t('page.audit.resource') }}</th>
              <th scope="col">{{ $t('page.audit.action') }}</th>
              <th scope="col">{{ $t('page.audit.outcome') }}</th>
              <th scope="col">{{ $t('page.audit.requestId') }}</th>
              <th scope="col">{{ $t('page.audit.createdAt') }}</th>
              <th scope="col">{{ $t('page.audit.details') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="8">
                {{ $t('page.audit.loading') }}
              </td>
            </tr>
            <tr v-else-if="page.items.length === 0">
              <td class="table-state" colspan="8">
                {{ $t('page.audit.empty') }}
              </td>
            </tr>
            <tr v-for="event in page.items" v-else :key="event.id">
              <td>
                <span class="category-pill">{{
                  categoryLabel(event.category)
                }}</span>
              </td>
              <td>{{ event.actorId || '—' }}</td>
              <td>{{ event.resource }}</td>
              <td>{{ event.action }}</td>
              <td>{{ event.outcome }}</td>
              <td>
                <code>{{ event.requestId || '—' }}</code>
              </td>
              <td>{{ formatDate(event.createdAt) }}</td>
              <td>
                <code class="details">{{ formatDetails(event) }}</code>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="pagination" aria-label="Pagination">
        <button
          type="button"
          :disabled="loading || currentPage <= 1"
          @click="changePage(-1)"
        >
          {{ $t('page.audit.previous') }}
        </button>
        <span>{{ currentPage }} / {{ totalPages }}</span>
        <button
          type="button"
          :disabled="loading || currentPage >= totalPages"
          @click="changePage(1)"
        >
          {{ $t('page.audit.next') }}
        </button>
      </footer>
    </section>

    <aside
      class="retention-card"
      data-retention="dry-run"
      aria-labelledby="audit-retention-title"
    >
      <div>
        <h2 id="audit-retention-title">
          {{ $t('page.audit.retentionTitle') }}
        </h2>
        <p>{{ $t('page.audit.retentionDescription') }}</p>
      </div>
      <div class="retention-actions">
        <label class="field" for="retention-days">
          <span>{{ $t('page.audit.retentionDays') }}</span>
          <input
            id="retention-days"
            v-model.number="retentionDays"
            min="1"
            max="3650"
            type="number"
          />
        </label>
        <button
          type="button"
          :disabled="retentionLoading"
          @click="runRetentionDryRun"
        >
          {{
            retentionLoading
              ? $t('page.audit.retentionRunning')
              : $t('page.audit.retentionDryRun')
          }}
        </button>
      </div>
      <p v-if="retentionReport" class="retention-result" role="status">
        {{
          $t('page.audit.retentionResult', {
            count: retentionReport.matchingCount,
            cutoff: retentionReport.cutoff,
          })
        }}
      </p>
    </aside>
  </ManagementPage>
</template>

<style scoped>
.audit-page {
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  justify-content: space-between;
}

.page-heading {
  margin-bottom: 20px;
}

.eyebrow {
  margin: 0;
  font-size: 0.75rem;
  font-weight: 700;
  color: hsl(var(--primary));
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

h1,
h2 {
  margin: 4px 0 8px;
}

h1 {
  font-size: clamp(1.5rem, 2vw, 2rem);
}

.description {
  max-width: 860px;
  margin: 0;
  line-height: 1.6;
  color: hsl(var(--muted-foreground));
}

.retention-chip,
.category-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  font-size: 0.78rem;
  font-weight: 650;
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 10%);
  border-radius: 999px;
}

.filter-card,
.table-card,
.retention-card {
  padding: 18px;
  margin-top: 16px;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 12px;
  box-shadow: 0 8px 24px hsl(var(--foreground) / 4%);
}

.category-tabs,
.actions,
.pagination,
.retention-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.category-tabs {
  margin: 14px 0;
}

button {
  min-height: 40px;
  padding: 8px 12px;
  color: hsl(var(--foreground));
  cursor: pointer;
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

button[aria-pressed='true'],
button.primary {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.field {
  display: grid;
  gap: 4px;
  font-weight: 600;
}

.field-wide {
  grid-column: span 2;
}

.field input {
  width: 100%;
  min-height: 44px;
  padding: 9px 11px;
  font: inherit;
  font-weight: 400;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

.table-heading > div:first-child {
  display: flex;
  gap: 10px;
  align-items: center;
}

.result-count {
  color: hsl(var(--muted-foreground));
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 980px;
  border-collapse: collapse;
}

th,
td {
  padding: 12px 10px;
  vertical-align: top;
  text-align: left;
  border-top: 1px solid hsl(var(--border));
}

th {
  font-size: 0.8rem;
  color: hsl(var(--muted-foreground));
}

code.details {
  display: block;
  max-width: 280px;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.table-state {
  padding: 36px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.pagination {
  justify-content: center;
  margin-top: 16px;
}

.retention-card {
  display: flex;
  gap: 20px;
  align-items: flex-end;
  justify-content: space-between;
}

.retention-card p {
  margin: 0;
  color: hsl(var(--muted-foreground));
}

.retention-result {
  margin-top: 12px !important;
  color: hsl(var(--primary)) !important;
}

.feedback {
  padding: 10px 12px;
  border-radius: 8px;
}

.feedback-error {
  color: hsl(var(--destructive));
  background: hsl(var(--destructive) / 8%);
}

.feedback-success {
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 8%);
}

.sr-status,
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip-path: inset(50%);
}

@media (max-width: 960px) {
  .field-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .retention-card {
    flex-direction: column;
    align-items: stretch;
  }
}

@media (max-width: 600px) {
  .page-heading,
  .table-heading {
    flex-direction: column;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-wide {
    grid-column: auto;
  }

  .actions {
    width: 100%;
  }

  .actions button {
    flex: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
  }
}
</style>
