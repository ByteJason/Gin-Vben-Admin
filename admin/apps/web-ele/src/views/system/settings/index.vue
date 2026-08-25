<script setup lang="ts">
import type {
  SettingCategory,
  SettingData,
  SettingDefinition,
} from '#/api/core/settings';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';

import {
  getSettingApi,
  listSettingDefinitionsApi,
  listSettingHistoryApi,
  rollbackSettingApi,
  testSettingConnectionApi,
  updateSettingApi,
} from '#/api/core/settings';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['system:settings:manage']));

const categories: Array<'all' | SettingCategory> = [
  'all',
  'basic',
  'security',
  'mail',
  'file',
  'captcha',
  'i18n',
];
const definitions = ref<SettingDefinition[]>([]);
const values = reactive<Record<string, SettingData>>({});
const drafts = reactive<Record<string, string>>({});
const selectedCategory = ref<'all' | SettingCategory>('all');
const loading = ref(true);
const savingKey = ref('');
const testingKey = ref('');
const error = ref('');
const message = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const historyKey = ref('');
const history = ref<SettingData[]>([]);
const historyLoading = ref(false);
const historyError = ref('');

const visibleDefinitions = computed(() =>
  definitions.value.filter(
    (definition) =>
      selectedCategory.value === 'all' ||
      definition.category === selectedCategory.value,
  ),
);

function categoryLabel(category: 'all' | SettingCategory) {
  return $t(`page.settings.category.${category}`);
}

function sourceLabel(source: string) {
  return $t(`page.settings.source.${source}`);
}

function displayDraft(
  setting: SettingData | undefined,
  definition: SettingDefinition,
) {
  if (!setting) return '';
  if (drafts[definition.key] !== undefined) return drafts[definition.key];
  if (definition.sensitive) return '';
  return setting.value;
}

function parseDraft(definition: SettingDefinition): unknown {
  const draft = (drafts[definition.key] ?? '').trim();
  if (definition.sensitive && draft === '') return undefined;
  if (definition.kind === 'string' || definition.kind === 'secret') {
    return draft;
  }
  try {
    return JSON.parse(draft);
  } catch {
    throw new Error(String($t('page.settings.invalidValue')));
  }
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
    definitions.value = await listSettingDefinitionsApi();
    const entries = await Promise.all(
      definitions.value.map(async (definition) => {
        const setting = await getSettingApi(definition.key);
        return [definition.key, setting] as const;
      }),
    );
    for (const [key, setting] of entries) values[key] = setting;
  } catch {
    error.value = String($t('page.settings.loadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

async function save(definition: SettingDefinition) {
  if (!canManage.value) return;
  const current = values[definition.key];
  if (!current) return;
  error.value = '';
  message.value = '';
  let value: unknown;
  try {
    value = parseDraft(definition);
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : String(caught);
    await focusError();
    return;
  }
  if (value === undefined) return;
  savingKey.value = definition.key;
  try {
    values[definition.key] = await updateSettingApi(definition.key, {
      expectedVersion: current.version,
      value,
    });
    drafts[definition.key] = '';
    message.value = String($t('page.settings.saved'));
  } catch {
    error.value = String($t('page.settings.saveError'));
    await focusError();
  } finally {
    savingKey.value = '';
  }
}

async function testConnection(definition: SettingDefinition) {
  if (!canManage.value) return;
  testingKey.value = definition.key;
  error.value = '';
  message.value = '';
  try {
    let value: unknown;
    if (drafts[definition.key]?.trim()) value = parseDraft(definition);
    const result = await testSettingConnectionApi(definition.key, value);
    message.value = `${String($t('page.settings.connectionSuccess'))} (${result.requestId})`;
  } catch {
    error.value = String($t('page.settings.connectionError'));
    await focusError();
  } finally {
    testingKey.value = '';
  }
}

async function openHistory(definition: SettingDefinition) {
  historyKey.value = definition.key;
  historyLoading.value = true;
  historyError.value = '';
  try {
    history.value = await listSettingHistoryApi(definition.key);
  } catch {
    history.value = [];
    historyError.value = String($t('page.settings.historyError'));
  } finally {
    historyLoading.value = false;
  }
}

async function rollback(item: SettingData) {
  if (!canManage.value) return;
  const current = values[historyKey.value];
  if (!current) return;
  try {
    values[historyKey.value] = await rollbackSettingApi(historyKey.value, {
      expectedVersion: current.version,
      version: item.version,
    });
    message.value = String($t('page.settings.rollbackSuccess'));
    const definition = definitions.value.find(
      (candidate) => candidate.key === historyKey.value,
    );
    if (definition) await openHistory(definition);
  } catch {
    error.value = String($t('page.settings.rollbackError'));
    await focusError();
  }
}

onMounted(load);
</script>

<template>
  <ManagementPage
    class="settings-page"
    :aria-busy="loading"
    aria-labelledby="settings-center-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.settings.eyebrow') }}</p>
        <h1 id="settings-center-title">{{ $t('page.settings.title') }}</h1>
        <p class="description">{{ $t('page.settings.description') }}</p>
      </div>
      <span class="scope-chip">{{ $t('page.settings.restartRequired') }}</span>
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
      {{ loading ? $t('page.settings.loading') : '' }}
    </p>

    <nav class="category-tabs" :aria-label="$t('page.settings.categoryLabel')">
      <button
        v-for="category in categories"
        :key="category"
        :aria-pressed="selectedCategory === category"
        type="button"
        @click="selectedCategory = category"
      >
        {{ categoryLabel(category) }}
      </button>
    </nav>

    <section class="settings-card" aria-labelledby="settings-table-title">
      <div class="table-heading">
        <h2 id="settings-table-title">{{ $t('page.settings.schema') }}</h2>
        <span class="result-count">{{ visibleDefinitions.length }}</span>
      </div>
      <div class="table-wrap">
        <table>
          <caption class="sr-only">
            {{
              $t('page.settings.tableLabel')
            }}
          </caption>
          <thead>
            <tr>
              <th scope="col">{{ $t('page.settings.key') }}</th>
              <th scope="col">{{ $t('page.settings.value') }}</th>
              <th scope="col">{{ $t('page.settings.source') }}</th>
              <th scope="col">{{ $t('page.settings.version') }}</th>
              <th scope="col">{{ $t('page.settings.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td class="table-state" colspan="5">
                {{ $t('page.settings.loading') }}
              </td>
            </tr>
            <tr v-else-if="visibleDefinitions.length === 0">
              <td class="table-state" colspan="5">
                {{ $t('page.settings.empty') }}
              </td>
            </tr>
            <tr
              v-for="definition in visibleDefinitions"
              v-else
              :key="definition.key"
            >
              <th scope="row">
                <span class="primary-text">{{ definition.key }}</span>
                <small>{{ categoryLabel(definition.category) }}</small>
              </th>
              <td>
                <input
                  :aria-label="`${definition.key} ${$t('page.settings.value')}`"
                  :disabled="!canManage || savingKey === definition.key"
                  :placeholder="
                    definition.sensitive ? '••••••' : definition.default
                  "
                  :value="displayDraft(values[definition.key], definition)"
                  :type="definition.sensitive ? 'password' : 'text'"
                  @input="
                    drafts[definition.key] = (
                      $event.target as HTMLInputElement
                    ).value
                  "
                />
              </td>
              <td>
                <span class="source-pill">{{
                  sourceLabel(values[definition.key]?.source ?? 'default')
                }}</span>
              </td>
              <td>{{ values[definition.key]?.version ?? 0 }}</td>
              <td class="actions-cell">
                <button
                  v-if="canManage"
                  :disabled="
                    definition.sensitive && !drafts[definition.key]?.trim()
                  "
                  type="button"
                  @click="save(definition)"
                >
                  {{
                    savingKey === definition.key
                      ? $t('page.settings.saving')
                      : $t('page.settings.save')
                  }}
                </button>
                <button
                  v-if="canManage"
                  :disabled="testingKey === definition.key"
                  type="button"
                  @click="testConnection(definition)"
                >
                  {{
                    testingKey === definition.key
                      ? $t('page.settings.testing')
                      : $t('page.settings.connectionTest')
                  }}
                </button>
                <button type="button" @click="openHistory(definition)">
                  {{ $t('page.settings.history') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <aside
      v-if="historyKey"
      class="history-card"
      aria-labelledby="settings-history-title"
    >
      <div class="table-heading">
        <h2 id="settings-history-title">
          {{ $t('page.settings.history') }}: {{ historyKey }}
        </h2>
        <button type="button" @click="historyKey = ''">
          {{ $t('page.settings.close') }}
        </button>
      </div>
      <p v-if="historyError" class="feedback feedback-error">
        {{ historyError }}
      </p>
      <p v-else-if="historyLoading" class="sr-status" aria-live="polite">
        {{ $t('page.settings.historyLoading') }}
      </p>
      <ul v-else class="history-list">
        <li v-for="item in history" :key="`${item.key}-${item.version}`">
          <span>v{{ item.version }} · {{ item.updatedAt || '—' }}</span>
          <button
            v-if="canManage"
            :disabled="item.version === values[historyKey]?.version"
            type="button"
            @click="rollback(item)"
          >
            {{ $t('page.settings.rollback') }}
          </button>
        </li>
      </ul>
    </aside>
  </ManagementPage>
</template>

<style scoped>
.settings-page {
  color: hsl(var(--foreground));
}

.page-heading,
.table-heading {
  display: flex;
  gap: 20px;
  align-items: center;
  justify-content: space-between;
}

.page-heading {
  align-items: flex-start;
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

.scope-chip,
.source-pill {
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

.category-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
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

.settings-card,
.history-card {
  padding: 18px;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 12px;
  box-shadow: 0 8px 24px hsl(var(--foreground) / 4%);
}

.history-card {
  margin-top: 16px;
}

.table-wrap {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  min-width: 120px;
  padding: 12px 10px;
  vertical-align: middle;
  text-align: left;
  border-bottom: 1px solid hsl(var(--border));
}

th {
  font-size: 0.82rem;
}

td {
  font-size: 0.88rem;
}

.primary-text {
  display: block;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

small {
  display: block;
  margin-top: 4px;
  font-weight: 400;
  color: hsl(var(--muted-foreground));
}

input {
  width: min(280px, 100%);
  min-height: 40px;
  padding: 8px 10px;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
}

.actions-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 280px;
}

.feedback {
  padding: 10px 12px;
  margin: 12px 0;
  border-radius: 8px;
}

.feedback-error {
  color: hsl(0deg 65% 36%);
  background: hsl(0deg 75% 55% / 10%);
}

.feedback-success {
  color: hsl(142deg 70% 30%);
  background: hsl(142deg 70% 45% / 14%);
}

.table-state {
  padding: 32px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.result-count {
  color: hsl(var(--muted-foreground));
}

.history-list {
  display: grid;
  gap: 8px;
  padding: 0;
  margin: 0;
  list-style: none;
}

.history-list li {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
  border-bottom: 1px solid hsl(var(--border));
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

@media (max-width: 720px) {
  .page-heading {
    flex-direction: column;
  }

  th,
  td {
    min-width: 140px;
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
