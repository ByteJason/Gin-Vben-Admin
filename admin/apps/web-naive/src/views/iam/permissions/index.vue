<script setup lang="ts">
import type { IAMPermission } from '#/api/core/iam';

import { nextTick, onMounted, ref } from 'vue';

import { listIAMPermissionsApi } from '#/api/core/iam';
import { $t } from '#/locales';

const permissions = ref<IAMPermission[]>([]);
const loading = ref(false);
const error = ref('');
const errorSummary = ref<HTMLElement | null>(null);

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function loadPermissions() {
  loading.value = true;
  error.value = '';
  try {
    permissions.value = await listIAMPermissionsApi();
  } catch {
    permissions.value = [];
    error.value = String($t('page.iam.permissionsLoadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

onMounted(loadPermissions);
</script>

<template>
  <main
    class="iam-permissions-page"
    :aria-busy="loading"
    aria-labelledby="iam-permissions-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.iam.eyebrow') }}</p>
        <h1 id="iam-permissions-title">{{ $t('page.iam.permissions') }}</h1>
        <p class="description">
          {{ $t('page.iam.permissionsDescription') }}
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
      {{ loading ? $t('page.iam.permissionsLoading') : '' }}
    </p>

    <section class="table-card" aria-labelledby="iam-permissions-table-title">
      <div class="table-heading">
        <h2 id="iam-permissions-table-title">
          {{ $t('page.iam.permissions') }}
        </h2>
        <span class="result-count">{{ permissions.length }}</span>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.iam.permissionsTable')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.iam.permissionId') }}</th>
              <th scope="col">{{ $t('page.iam.permissionName') }}</th>
              <th scope="col">{{ $t('page.iam.permissionMethod') }}</th>
              <th scope="col">{{ $t('page.iam.permissionPath') }}</th>
              <th scope="col">{{ $t('page.iam.permissionActive') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="5">
                {{ $t('page.iam.permissionsLoading') }}
              </td>
            </tr>
            <tr v-else-if="permissions.length === 0">
              <td class="table-state" colspan="5">
                {{ $t('page.iam.permissionsEmpty') }}
              </td>
            </tr>
            <tr v-for="permission in permissions" v-else :key="permission.id">
              <th scope="row">
                <span class="primary-text">{{ permission.id }}</span>
              </th>
              <td>{{ permission.name }}</td>
              <td>
                <code>{{ permission.method || '—' }}</code>
              </td>
              <td>
                <code>{{ permission.path || '—' }}</code>
              </td>
              <td>
                <span
                  class="status-pill"
                  :data-status="permission.active ? 'active' : 'disabled'"
                >
                  {{
                    permission.active
                      ? $t('page.iam.permissionActiveYes')
                      : $t('page.iam.permissionActiveNo')
                  }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </main>
</template>

<style scoped>
.iam-permissions-page {
  max-width: 1320px;
  padding: 24px;
  margin: 0 auto;
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
  border-radius: 999px;
  font-size: 0.78rem;
  font-weight: 650;
}

.scope-chip {
  color: hsl(var(--primary));
  background: hsl(var(--primary) / 0.1);
}

.status-pill[data-status='active'] {
  color: hsl(142 70% 30%);
  background: hsl(142 70% 45% / 0.14);
}

.status-pill[data-status='disabled'] {
  color: hsl(var(--muted-foreground));
  background: hsl(var(--muted));
}

.feedback {
  padding: 10px 12px;
  margin: 12px 0;
  color: hsl(0 65% 36%);
  background: hsl(0 75% 55% / 0.1);
  border-radius: 8px;
}

.sr-status,
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
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
  color: hsl(var(--muted-foreground));
  font-size: 0.85rem;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 820px;
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
  .iam-permissions-page {
    padding: 16px;
  }

  .page-heading {
    display: grid;
  }
}
</style>
