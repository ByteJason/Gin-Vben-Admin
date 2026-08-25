<script setup lang="ts">
import type { IAMPolicy } from '#/api/core/iam';

import { nextTick, onMounted, ref } from 'vue';

import { ManagementPage } from '@vben/common-ui';

import { listIAMPoliciesApi } from '#/api/core/iam';
import { $t } from '#/locales';

const policies = ref<IAMPolicy[]>([]);
const loading = ref(false);
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function loadPolicies() {
  loading.value = true;
  error.value = '';
  try {
    policies.value = await listIAMPoliciesApi();
  } catch {
    policies.value = [];
    error.value = String($t('page.iam.policiesLoadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

onMounted(loadPolicies);
</script>

<template>
  <ManagementPage
    class="iam-policies-page"
    :aria-busy="loading"
    aria-labelledby="iam-policies-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-policies-title">{{ $t('page.iam.policies') }}</h1>
        <p class="description">
          {{ $t('page.iam.policiesDescription') }}
        </p>
      </div>
      <span class="scope-chip">{{ $t('page.iam.readOnly') }}</span>
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
    <p class="sr-status" aria-live="polite">
      {{ loading ? $t('page.iam.policiesLoading') : '' }}
    </p>

    <section class="table-card" aria-labelledby="iam-policies-table-title">
      <div class="table-heading">
        <h2 id="iam-policies-table-title">
          {{ $t('page.iam.policies') }}
        </h2>
        <span class="result-count">{{ policies.length }}</span>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.iam.policiesTable')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.iam.policySubject') }}</th>
              <th scope="col">{{ $t('page.iam.policyRole') }}</th>
              <th scope="col">{{ $t('page.iam.policyDomain') }}</th>
              <th scope="col">{{ $t('page.iam.policyMethod') }}</th>
              <th scope="col">{{ $t('page.iam.policyPath') }}</th>
              <th scope="col">{{ $t('page.iam.policyEffect') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.policiesLoading') }}
              </td>
            </tr>
            <tr v-else-if="policies.length === 0">
              <td class="table-state" colspan="6">
                {{ $t('page.iam.policiesEmpty') }}
              </td>
            </tr>
            <tr
              v-for="policy in policies"
              v-else
              :key="`${policy.subject ?? ''}:${policy.roleId ?? ''}:${policy.domain ?? ''}:${policy.method}:${policy.path}`"
            >
              <th scope="row">
                <span class="primary-text">{{ policy.subject || '—' }}</span>
              </th>
              <td>{{ policy.roleId || '—' }}</td>
              <td>{{ policy.domain || '—' }}</td>
              <td>
                <code>{{ policy.method || '—' }}</code>
              </td>
              <td>
                <code>{{ policy.path || '—' }}</code>
              </td>
              <td>
                <span
                  class="status-pill"
                  :data-status="
                    policy.effect === 'allow' ? 'active' : 'disabled'
                  "
                >
                  {{
                    policy.effect === 'allow'
                      ? $t('page.iam.policyAllow')
                      : $t('page.iam.policyDeny')
                  }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </ManagementPage>
</template>

<style scoped>
.iam-policies-page {
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

.scope-chip,
.status-pill {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  font-size: 0.78rem;
  font-weight: 650;
  border-radius: 999px;
}

.scope-chip {
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 10%);
}

.status-pill[data-status='active'] {
  color: hsl(142deg 70% 30%);
  background: hsl(142deg 70% 45% / 14%);
}

.status-pill[data-status='disabled'] {
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
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
  min-width: 1080px;
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
