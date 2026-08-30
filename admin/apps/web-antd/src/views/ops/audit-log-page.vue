<script setup lang="ts">
import type { AuditCategory, AuditEvent, AuditPage } from '#/api/core/audit';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import { ManagementPage } from '@vben/common-ui';

import { queryLoginLogsApi, queryOperationHistoryApi } from '#/api/core/audit';
import { $t } from '#/locales';

const props = defineProps<{ mode: 'login' | 'operation' }>();

const state = reactive({
  actorId: '',
  from: '',
  limit: 20,
  offset: 0,
  outcome: '',
  requestId: '',
  to: '',
});
const page = ref<AuditPage>({ items: [], limit: 20, offset: 0, total: 0 });
const loading = ref(false);
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);

const namespace = computed(() =>
  props.mode === 'login' ? 'page.loginLogs' : 'page.operationHistory',
);
const currentPage = computed(() => Math.floor(state.offset / state.limit) + 1);
const totalPages = computed(() =>
  Math.max(1, Math.ceil(page.value.total / state.limit)),
);

function label(key: string, params?: Record<string, unknown>) {
  const path = `${namespace.value}.${key}`;
  return String(params ? $t(path, params) : $t(path));
}

function details(event: AuditEvent) {
  return event.details ?? {};
}

function detailValue(event: AuditEvent, ...keys: string[]): unknown {
  const source = details(event);
  for (const key of keys) {
    const value = source[key];
    if (value !== undefined && value !== null && value !== '') return value;
  }
  return undefined;
}

function display(value: unknown, fallback = '—') {
  if (value === undefined || value === null || value === '') return fallback;
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  return safeJSON(value);
}

function safeJSON(value: unknown) {
  const seen = new WeakSet<object>();
  const serialized = JSON.stringify(
    value,
    (key, item) => {
      if (/password|secret|token|authorization|api[_-]?key/i.test(key)) {
        return '[REDACTED]';
      }
      if (typeof item === 'object' && item !== null) {
        if (seen.has(item)) return '[Circular]';
        seen.add(item);
      }
      return item;
    },
    2,
  );
  return serialized || '—';
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

function requestPayload(event: AuditEvent) {
  return safeJSON(
    detailValue(event, 'request', 'requestBody', 'requestPayload') ?? {},
  );
}

function responsePayload(event: AuditEvent) {
  return safeJSON(
    detailValue(event, 'response', 'responseBody', 'responsePayload') ?? {},
  );
}

function loginDevice(event: AuditEvent) {
  const device = display(detailValue(event, 'deviceName'), '');
  const browser = display(detailValue(event, 'browser', 'userAgent'), '');
  return [device, browser].filter(Boolean).join(' · ') || '—';
}

function category(): AuditCategory {
  return props.mode;
}

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const query = {
      actorId: state.actorId.trim() || undefined,
      category: category(),
      from: state.from ? new Date(state.from).toISOString() : undefined,
      limit: state.limit,
      offset: state.offset,
      outcome: state.outcome.trim() || undefined,
      requestId: state.requestId.trim() || undefined,
      to: state.to ? new Date(state.to).toISOString() : undefined,
    };
    page.value =
      props.mode === 'login'
        ? await queryLoginLogsApi(query)
        : await queryOperationHistoryApi(query);
  } catch {
    error.value = label('loadError');
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
  state.actorId = '';
  state.from = '';
  state.offset = 0;
  state.outcome = '';
  state.requestId = '';
  state.to = '';
  void load();
}

function changePage(delta: number) {
  const next = currentPage.value + delta;
  if (next < 1 || next > totalPages.value || loading.value) return;
  state.offset = (next - 1) * state.limit;
  void load();
}

onMounted(load);
</script>

<template>
  <ManagementPage
    class="audit-log-page"
    :aria-busy="loading"
    :labelledby="`${mode}-log-title`"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ label('eyebrow') }}</p>
        <h1 :id="`${mode}-log-title`">{{ label('title') }}</h1>
        <p class="description">{{ label('description') }}</p>
      </div>
      <button type="button" :disabled="loading" @click="load">
        {{ loading ? label('loading') : label('refresh') }}
      </button>
    </header>

    <p
      v-if="error"
      ref="errorSummary"
      class="feedback error"
      role="alert"
      tabindex="-1"
    >
      {{ error }}
    </p>
    <p class="sr-status" aria-live="polite">
      {{ loading ? label('loading') : '' }}
    </p>

    <section class="filter-card" :aria-label="label('filters')">
      <label>
        <span>{{ label('actor') }}</span>
        <input
          v-model="state.actorId"
          :placeholder="label('actorPlaceholder')"
        />
      </label>
      <label>
        <span>{{ label('outcome') }}</span>
        <input
          v-model="state.outcome"
          :placeholder="label('outcomePlaceholder')"
        />
      </label>
      <label>
        <span>{{ label('requestId') }}</span>
        <input v-model="state.requestId" :placeholder="label('requestId')" />
      </label>
      <label>
        <span>{{ label('from') }}</span>
        <input v-model="state.from" type="datetime-local" />
      </label>
      <label>
        <span>{{ label('to') }}</span>
        <input v-model="state.to" type="datetime-local" />
      </label>
      <div class="filter-actions">
        <button type="button" :disabled="loading" @click="resetFilters">
          {{ label('reset') }}
        </button>
        <button
          class="primary"
          type="button"
          :disabled="loading"
          @click="applyFilters"
        >
          {{ label('search') }}
        </button>
      </div>
    </section>

    <section class="table-card" :aria-labelledby="`${mode}-table-title`">
      <div class="table-heading">
        <h2 :id="`${mode}-table-title`">{{ label('tableTitle') }}</h2>
        <span>{{ label('total', { count: page.total }) }}</span>
      </div>
      <div class="table-scroll">
        <table>
          <caption class="sr-only">
            {{
              label('tableLabel')
            }}
          </caption>
          <thead>
            <tr v-if="mode === 'operation'">
              <th scope="col">{{ label('id') }}</th>
              <th scope="col">{{ label('operator') }}</th>
              <th scope="col">{{ label('date') }}</th>
              <th scope="col">{{ label('statusCode') }}</th>
              <th scope="col">{{ label('requestIp') }}</th>
              <th scope="col">{{ label('requestId') }}</th>
              <th scope="col">{{ label('traceId') }}</th>
              <th scope="col">{{ label('deviceId') }}</th>
              <th scope="col">{{ label('fingerprint') }}</th>
              <th scope="col">{{ label('method') }}</th>
              <th scope="col">{{ label('path') }}</th>
              <th scope="col">{{ label('request') }}</th>
              <th scope="col">{{ label('response') }}</th>
              <th scope="col">{{ label('actions') }}</th>
            </tr>
            <tr v-else>
              <th scope="col">{{ label('id') }}</th>
              <th scope="col">{{ label('username') }}</th>
              <th scope="col">{{ label('loginIp') }}</th>
              <th scope="col">{{ label('status') }}</th>
              <th scope="col">{{ label('details') }}</th>
              <th scope="col">{{ label('browserDevice') }}</th>
              <th scope="col">{{ label('fingerprint') }}</th>
              <th scope="col">{{ label('loginAt') }}</th>
              <th scope="col">{{ label('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!loading && page.items.length === 0">
              <td :colspan="mode === 'operation' ? 14 : 9" class="empty-state">
                {{ label('empty') }}
              </td>
            </tr>
            <template v-for="event in page.items" :key="event.id">
              <tr v-if="mode === 'operation'">
                <td class="code">{{ event.id }}</td>
                <td>{{ event.actorId || '—' }}</td>
                <td>{{ formatDate(event.createdAt) }}</td>
                <td>
                  <span class="status" :class="event.outcome">
                    {{
                      display(
                        detailValue(event, 'statusCode', 'httpStatus'),
                        event.outcome,
                      )
                    }}
                  </span>
                </td>
                <td>
                  {{
                    display(detailValue(event, 'requestIp', 'ipAddress', 'ip'))
                  }}
                </td>
                <td class="code">{{ event.requestId || '—' }}</td>
                <td class="code">
                  {{
                    display(
                      detailValue(event, 'traceId'),
                      event.requestId || '—',
                    )
                  }}
                </td>
                <td class="code">
                  {{ display(detailValue(event, 'deviceId')) }}
                </td>
                <td class="code">
                  {{
                    display(detailValue(event, 'jsFingerprint', 'fingerprint'))
                  }}
                </td>
                <td>
                  {{ display(detailValue(event, 'method', 'requestMethod')) }}
                </td>
                <td class="path">
                  {{
                    display(
                      detailValue(event, 'path', 'requestPath'),
                      event.resource,
                    )
                  }}
                </td>
                <td>
                  <details>
                    <summary>{{ label('view') }}</summary>
                    <pre>{{ requestPayload(event) }}</pre>
                  </details>
                </td>
                <td>
                  <details>
                    <summary>{{ label('view') }}</summary>
                    <pre>{{ responsePayload(event) }}</pre>
                  </details>
                </td>
                <td>
                  <details>
                    <summary>{{ label('view') }}</summary>
                    <pre>{{ safeJSON(details(event)) }}</pre>
                  </details>
                </td>
              </tr>
              <tr v-else>
                <td class="code">{{ event.id }}</td>
                <td>
                  {{
                    display(
                      detailValue(event, 'username'),
                      event.actorId || '—',
                    )
                  }}
                </td>
                <td>
                  {{
                    display(detailValue(event, 'loginIp', 'ipAddress', 'ip'))
                  }}
                </td>
                <td>
                  <span class="status" :class="event.outcome">{{
                    event.outcome
                  }}</span>
                </td>
                <td>
                  {{
                    display(
                      detailValue(event, 'reason', 'message'),
                      `${event.resource}.${event.action}`,
                    )
                  }}
                </td>
                <td>{{ loginDevice(event) }}</td>
                <td class="code">
                  {{
                    display(detailValue(event, 'jsFingerprint', 'fingerprint'))
                  }}
                </td>
                <td>{{ formatDate(event.createdAt) }}</td>
                <td>
                  <details>
                    <summary>{{ label('view') }}</summary>
                    <pre>{{ safeJSON(details(event)) }}</pre>
                  </details>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <footer class="pagination">
        <button
          type="button"
          :disabled="currentPage <= 1 || loading"
          @click="changePage(-1)"
        >
          {{ label('previous') }}
        </button>
        <span>{{
          label('page', { current: currentPage, total: totalPages })
        }}</span>
        <button
          type="button"
          :disabled="currentPage >= totalPages || loading"
          @click="changePage(1)"
        >
          {{ label('next') }}
        </button>
      </footer>
    </section>
  </ManagementPage>
</template>

<style scoped>
.audit-log-page {
  --line: hsl(var(--border));
  --muted: hsl(var(--muted-foreground));

  color: hsl(var(--foreground));
}

.page-heading,
.table-heading,
.pagination,
.filter-actions {
  display: flex;
  gap: 1rem;
  align-items: center;
  justify-content: space-between;
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
.description {
  margin: 0;
}

h1 {
  font-size: clamp(1.7rem, 3vw, 2.4rem);
}

h2 {
  font-size: 1.08rem;
}

.description,
.table-heading span,
.pagination span {
  color: var(--muted);
}

.description {
  max-inline-size: 72ch;
  margin-block-start: 0.5rem;
}

.filter-card,
.table-card {
  margin-block-start: 1rem;
  background: hsl(var(--card));
  border: 1px solid var(--line);
  border-radius: 1rem;
}

.filter-card {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 11rem), 1fr));
  gap: 0.8rem;
  padding: 1rem;
}

label {
  display: grid;
  gap: 0.35rem;
  font-size: 0.78rem;
  color: var(--muted);
}

input,
button {
  min-block-size: 2.5rem;
  padding-inline: 0.75rem;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid var(--line);
  border-radius: 0.6rem;
}

button {
  cursor: pointer;
}

button.primary {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

button:focus-visible,
input:focus-visible,
summary:focus-visible {
  outline: 3px solid color-mix(in srgb, hsl(var(--primary)) 30%, transparent);
  outline-offset: 2px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.feedback {
  padding: 0.8rem 1rem;
  margin-block-start: 1rem;
  border-radius: 0.7rem;
}

.feedback.error {
  color: hsl(var(--destructive));
  background: color-mix(in srgb, hsl(var(--destructive)) 10%, hsl(var(--card)));
}

.table-heading,
.pagination {
  padding: 1rem;
}

.table-scroll {
  inline-size: 100%;
  overflow: auto;
  border-block: 1px solid var(--line);
}

table {
  inline-size: 100%;
  min-inline-size: 96rem;
  border-collapse: collapse;
}

th,
td {
  max-inline-size: 19rem;
  padding: 0.75rem;
  vertical-align: top;
  text-align: start;
  border-block-end: 1px solid var(--line);
}

th {
  font-size: 0.75rem;
  color: var(--muted);
  white-space: nowrap;
}

td {
  font-size: 0.8rem;
}

.code,
.path,
pre {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.path {
  overflow-wrap: anywhere;
}

.status {
  display: inline-flex;
  padding: 0.2rem 0.5rem;
  font-weight: 750;
  border-radius: 999px;
}

.status.success,
.status.ok {
  color: #166534;
  background: #dcfce7;
}

.status.failure,
.status.failed,
.status.error {
  color: #991b1b;
  background: #fee2e2;
}

details {
  min-inline-size: 4rem;
}

summary {
  color: hsl(var(--primary));
  cursor: pointer;
}

pre {
  min-inline-size: 20rem;
  max-block-size: 18rem;
  padding: 0.75rem;
  overflow: auto;
  color: #dbeafe;
  white-space: pre-wrap;
  background: #0f172a;
  border-radius: 0.5rem;
}

.empty-state {
  padding-block: 2rem;
  color: var(--muted);
  text-align: center;
}

@media (width <= 720px) {
  .page-heading {
    flex-direction: column;
    align-items: stretch;
  }

  .filter-actions {
    justify-content: stretch;
  }

  .filter-actions button {
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
  }
}
</style>
