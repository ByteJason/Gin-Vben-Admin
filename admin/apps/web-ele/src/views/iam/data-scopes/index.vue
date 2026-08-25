<script setup lang="ts">
import type { IAMDataScope, IAMDataScopeType } from '#/api/core/iam';

import { nextTick, onMounted, ref } from 'vue';

import { ManagementPage } from '@vben/common-ui';

import { listIAMDataScopesApi } from '#/api/core/iam';
import { $t } from '#/locales';

const dataScopes = ref<IAMDataScope[]>([]);
const dataScopesLoading = ref(false);
const dataScopesLoadError = ref('');
const dataScopesErrorSummary = ref<HTMLElement | null>(null);

async function focusError() {
  await nextTick();
  dataScopesErrorSummary.value?.focus();
}

async function loadDataScopes() {
  dataScopesLoading.value = true;
  dataScopesLoadError.value = '';
  try {
    dataScopes.value = await listIAMDataScopesApi();
  } catch {
    dataScopes.value = [];
    dataScopesLoadError.value = String($t('page.iam.dataScopesLoadError'));
    await focusError();
  } finally {
    dataScopesLoading.value = false;
  }
}

function scopeLabel(scope: IAMDataScopeType) {
  const labels: Record<IAMDataScopeType, string> = {
    all: 'page.iam.dataScopeAll',
    custom: 'page.iam.dataScopeCustom',
    org: 'page.iam.dataScopeOrg',
    own: 'page.iam.dataScopeOwn',
  };
  return String($t(labels[scope]));
}

onMounted(loadDataScopes);
</script>

<template>
  <ManagementPage
    class="iam-data-scopes-page"
    :aria-busy="dataScopesLoading"
    aria-labelledby="iam-data-scopes-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-data-scopes-title">{{ $t('page.iam.dataScopes') }}</h1>
        <p class="description">
          {{ $t('page.iam.dataScopesDescription') }}
        </p>
      </div>
      <span class="scope-chip">{{ $t('page.iam.readOnly') }}</span>
    </header>

    <p
      v-if="dataScopesLoadError"
      ref="dataScopesErrorSummary"
      class="feedback feedback-error"
      role="alert"
      tabindex="-1"
    >
      {{ dataScopesLoadError }}
    </p>
    <p class="sr-status" aria-live="polite">
      {{ dataScopesLoading ? $t('page.iam.dataScopesLoading') : '' }}
    </p>

    <section class="table-card" aria-labelledby="iam-data-scopes-table-title">
      <div class="table-heading">
        <h2 id="iam-data-scopes-table-title">
          {{ $t('page.iam.dataScopes') }}
        </h2>
        <span class="result-count">{{ dataScopes.length }}</span>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.iam.dataScopesTable')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.iam.dataScopeSubject') }}</th>
              <th scope="col">{{ $t('page.iam.dataScopeRole') }}</th>
              <th scope="col">{{ $t('page.iam.dataScopeDomain') }}</th>
              <th scope="col">{{ $t('page.iam.dataScopeResource') }}</th>
              <th scope="col">{{ $t('page.iam.dataScopeScope') }}</th>
              <th scope="col">{{ $t('page.iam.dataScopeIds') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="dataScopesLoading">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.dataScopesLoading') }}
              </td>
            </tr>
            <tr v-else-if="dataScopes.length === 0">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.dataScopesEmpty') }}
              </td>
            </tr>
            <tr
              v-for="dataScope in dataScopes"
              v-else
              :key="`${dataScope.subject ?? ''}:${dataScope.roleId ?? ''}:${dataScope.domain ?? ''}:${dataScope.resource}:${dataScope.scope}:${dataScope.ids.join(',')}`"
            >
              <th scope="row">
                <span class="primary-text">{{ dataScope.subject || '—' }}</span>
              </th>
              <td>{{ dataScope.roleId || '—' }}</td>
              <td>{{ dataScope.domain || '—' }}</td>
              <td>
                <code>{{ dataScope.resource || '—' }}</code>
              </td>
              <td>
                <span class="scope-value">{{
                  scopeLabel(dataScope.scope)
                }}</span>
              </td>
              <td>
                <code>{{
                  dataScope.ids.length ? dataScope.ids.join(', ') : '—'
                }}</code>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </ManagementPage>
</template>

<style scoped>
.iam-data-scopes-page {
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading {
  display: flex;
  align-items: center;
}

.page-heading {
  gap: 24px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}

.eyebrow {
  margin: 0;
  font-size: 0.75rem;
  font-weight: 700;
  color: hsl(var(--muted-foreground));
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

h2 {
  font-size: 1.05rem;
}

.description {
  max-width: 760px;
  margin: 0;
  color: hsl(var(--muted-foreground));
}

.scope-chip {
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

.feedback {
  padding: 10px 12px;
  margin: 12px 0;
  color: hsl(0deg 65% 36%);
  background: hsl(0deg 75% 55% / 10%);
  border-radius: 8px;
}

.sr-status,
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip-path: inset(50%);
}

.table-card {
  overflow: hidden;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 14px;
}

.table-heading {
  justify-content: space-between;
  padding: 16px 18px;
  border-bottom: 1px solid hsl(var(--border));
}

.result-count {
  font-size: 0.85rem;
  color: hsl(var(--muted-foreground));
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 1180px;
  border-collapse: collapse;
}

th,
td {
  padding: 13px 16px;
  text-align: left;
  border-bottom: 1px solid hsl(var(--border));
}

th {
  font-size: 0.78rem;
  font-weight: 700;
  color: hsl(var(--muted-foreground));
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

tbody th {
  color: hsl(var(--foreground));
  text-transform: none;
  letter-spacing: normal;
}

tbody tr:last-child th,
tbody tr:last-child td {
  border-bottom: 0;
}

.primary-text {
  font-weight: 650;
}

.scope-value {
  white-space: nowrap;
}

code {
  padding: 2px 5px;
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
  border-radius: 4px;
}

.table-state {
  padding: 28px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

@media (max-width: 720px) {
  .page-heading {
    display: grid;
  }
}
</style>
