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
  'other',
];
const categoryOrder = categories.filter(
  (category): category is SettingCategory => category !== 'all',
);
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

const groupedDefinitions = computed(() => {
  const visibleCategories =
    selectedCategory.value === 'all' ? categoryOrder : [selectedCategory.value];

  return visibleCategories
    .map((category) => ({
      category,
      items: definitions.value.filter(
        (definition) => definition.category === category,
      ),
    }))
    .filter((group) => group.items.length > 0);
});

const visibleCount = computed(() =>
  groupedDefinitions.value.reduce(
    (total, group) => total + group.items.length,
    0,
  ),
);

function categoryLabel(category: 'all' | SettingCategory) {
  return $t(`page.settings.category.${category}`);
}

function categoryDescription(category: SettingCategory) {
  return $t(`page.settings.categoryDescription.${category}`);
}

function sourceLabel(source: string) {
  return $t(`page.settings.source.${source}`);
}

function hasDraft(definition: SettingDefinition) {
  return Object.prototype.hasOwnProperty.call(drafts, definition.key);
}

function decodeValue(raw: string | undefined, definition: SettingDefinition) {
  if (raw === undefined || definition.sensitive) return '';
  try {
    const parsed = JSON.parse(raw);
    if (definition.kind === 'json') return JSON.stringify(parsed, null, 2);
    if (parsed === null || parsed === undefined) return '';
    return String(parsed);
  } catch {
    return raw;
  }
}

function displayDraft(
  setting: SettingData | undefined,
  definition: SettingDefinition,
) {
  if (hasDraft(definition)) return drafts[definition.key] ?? '';
  return decodeValue(setting?.value, definition);
}

function placeholderValue(definition: SettingDefinition) {
  if (definition.sensitive) return '••••••';
  return decodeValue(definition.default, definition);
}

function booleanValue(definition: SettingDefinition) {
  return displayDraft(values[definition.key], definition) === 'true';
}

function updateDraft(definition: SettingDefinition, value: string) {
  drafts[definition.key] = value;
}

function toggleBoolean(definition: SettingDefinition) {
  if (!canManage.value || savingKey.value === definition.key) return;
  updateDraft(definition, booleanValue(definition) ? 'false' : 'true');
}

function fieldId(definition: SettingDefinition) {
  return `setting-${definition.key.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
}

function parseDraft(definition: SettingDefinition): unknown {
  if (!hasDraft(definition)) return undefined;
  const draft = drafts[definition.key] ?? '';
  if (definition.sensitive && draft.trim() === '') return undefined;
  if (definition.kind === 'string' || definition.kind === 'secret') {
    return draft;
  }
  try {
    return JSON.parse(draft.trim());
  } catch {
    throw new Error(String($t('page.settings.invalidValue')));
  }
}

function saveDisabled(definition: SettingDefinition) {
  return (
    !canManage.value ||
    savingKey.value === definition.key ||
    !hasDraft(definition) ||
    (definition.sensitive && !(drafts[definition.key] ?? '').trim())
  );
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
  if (saveDisabled(definition)) return;
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
    delete drafts[definition.key];
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
    const value = hasDraft(definition) ? parseDraft(definition) : undefined;
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
    delete drafts[historyKey.value];
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
        class="tab-button"
        type="button"
        @click="selectedCategory = category"
      >
        {{ categoryLabel(category) }}
      </button>
    </nav>

    <section class="settings-summary" aria-live="polite">
      <div>
        <strong>{{ categoryLabel(selectedCategory) }}</strong>
        <span>{{ $t('page.settings.formDescription') }}</span>
      </div>
      <span class="result-count">{{ visibleCount }}</span>
    </section>

    <div v-if="loading" class="page-state">
      {{ $t('page.settings.loading') }}
    </div>
    <div v-else-if="groupedDefinitions.length === 0" class="page-state">
      {{ $t('page.settings.empty') }}
    </div>
    <div v-else class="settings-groups">
      <section
        v-for="group in groupedDefinitions"
        :key="group.category"
        class="category-panel"
        :aria-labelledby="`settings-category-${group.category}`"
      >
        <header class="category-heading">
          <div>
            <h2 :id="`settings-category-${group.category}`">
              {{ categoryLabel(group.category) }}
            </h2>
            <p>{{ categoryDescription(group.category) }}</p>
          </div>
          <span class="category-count">{{ group.items.length }}</span>
        </header>

        <div class="settings-form">
          <article
            v-for="definition in group.items"
            :key="definition.key"
            class="setting-item"
          >
            <div class="setting-copy">
              <div class="setting-tags">
                <code class="key-tag">{{ definition.key }}</code>
                <span v-if="definition.restartRequired" class="policy-tag">
                  {{ $t('page.settings.restartRequired') }}
                </span>
                <span v-if="definition.sensitive" class="policy-tag sensitive">
                  {{ $t('page.settings.sensitive') }}
                </span>
              </div>
              <p class="setting-description">
                {{
                  definition.description ||
                  $t('page.settings.defaultItemDescription')
                }}
              </p>
              <dl class="setting-meta">
                <div>
                  <dt>{{ $t('page.settings.sourceLabel') }}</dt>
                  <dd>
                    <span class="source-pill">
                      {{
                        sourceLabel(values[definition.key]?.source ?? 'default')
                      }}
                    </span>
                  </dd>
                </div>
                <div>
                  <dt>{{ $t('page.settings.version') }}</dt>
                  <dd>v{{ values[definition.key]?.version ?? 0 }}</dd>
                </div>
                <div v-if="definition.envKey">
                  <dt>ENV</dt>
                  <dd class="mono">{{ definition.envKey }}</dd>
                </div>
              </dl>
            </div>

            <div class="setting-editor">
              <label
                v-if="definition.kind !== 'bool'"
                class="field-label"
                :for="fieldId(definition)"
              >
                {{ $t('page.settings.value') }}
              </label>

              <button
                v-if="definition.kind === 'bool'"
                :aria-checked="booleanValue(definition)"
                :aria-label="`${definition.key} ${$t('page.settings.value')}`"
                :disabled="!canManage || savingKey === definition.key"
                class="switch-control"
                role="switch"
                type="button"
                @click="toggleBoolean(definition)"
              >
                <span class="switch-track" aria-hidden="true">
                  <span class="switch-knob"></span>
                </span>
                <span>
                  {{
                    booleanValue(definition)
                      ? $t('page.settings.enabled')
                      : $t('page.settings.disabled')
                  }}
                </span>
              </button>

              <select
                v-else-if="definition.allowed?.length"
                :id="fieldId(definition)"
                :disabled="!canManage || savingKey === definition.key"
                :value="displayDraft(values[definition.key], definition)"
                @change="
                  updateDraft(
                    definition,
                    ($event.target as HTMLSelectElement).value,
                  )
                "
              >
                <option
                  v-for="option in definition.allowed"
                  :key="option"
                  :value="option"
                >
                  {{ option }}
                </option>
              </select>

              <textarea
                v-else-if="definition.kind === 'json'"
                :id="fieldId(definition)"
                :disabled="!canManage || savingKey === definition.key"
                :placeholder="placeholderValue(definition)"
                :value="displayDraft(values[definition.key], definition)"
                rows="4"
                spellcheck="false"
                @input="
                  updateDraft(
                    definition,
                    ($event.target as HTMLTextAreaElement).value,
                  )
                "
              ></textarea>

              <input
                v-else
                :id="fieldId(definition)"
                :disabled="!canManage || savingKey === definition.key"
                :placeholder="placeholderValue(definition)"
                :step="definition.kind === 'number' ? 'any' : undefined"
                :type="
                  definition.sensitive
                    ? 'password'
                    : definition.kind === 'number'
                      ? 'number'
                      : 'text'
                "
                :value="displayDraft(values[definition.key], definition)"
                @input="
                  updateDraft(
                    definition,
                    ($event.target as HTMLInputElement).value,
                  )
                "
              />

              <div class="actions-cell">
                <button
                  v-if="canManage"
                  :disabled="saveDisabled(definition)"
                  class="action-button primary"
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
                  class="action-button"
                  type="button"
                  @click="testConnection(definition)"
                >
                  {{
                    testingKey === definition.key
                      ? $t('page.settings.testing')
                      : $t('page.settings.connectionTest')
                  }}
                </button>
                <button
                  class="action-button subtle"
                  type="button"
                  @click="openHistory(definition)"
                >
                  {{ $t('page.settings.history') }}
                </button>
              </div>
            </div>
          </article>
        </div>
      </section>
    </div>

    <aside
      v-if="historyKey"
      class="history-card"
      aria-labelledby="settings-history-title"
    >
      <div class="history-heading">
        <div>
          <span class="history-eyebrow">{{ $t('page.settings.history') }}</span>
          <h2 id="settings-history-title">{{ historyKey }}</h2>
        </div>
        <button class="action-button" type="button" @click="historyKey = ''">
          {{ $t('page.settings.close') }}
        </button>
      </div>
      <p v-if="historyError" class="feedback feedback-error">
        {{ historyError }}
      </p>
      <p v-else-if="historyLoading" class="history-state" aria-live="polite">
        {{ $t('page.settings.historyLoading') }}
      </p>
      <ul v-else class="history-list">
        <li v-for="item in history" :key="`${item.key}-${item.version}`">
          <span>v{{ item.version }} · {{ item.updatedAt || '—' }}</span>
          <button
            v-if="canManage"
            :disabled="item.version === values[historyKey]?.version"
            class="action-button"
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
.category-heading,
.history-heading,
.settings-summary {
  display: flex;
  gap: 20px;
  align-items: center;
  justify-content: space-between;
}

.page-heading {
  align-items: flex-start;
  margin-bottom: 20px;
}

.eyebrow,
.history-eyebrow {
  margin: 0;
  font-size: 0.72rem;
  font-weight: 750;
  color: hsl(var(--primary));
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

h1,
h2,
p {
  margin-top: 0;
}

h1 {
  margin-bottom: 8px;
  font-size: clamp(1.5rem, 2vw, 2rem);
}

h2 {
  margin-bottom: 6px;
  font-size: 1rem;
}

.description {
  max-width: 860px;
  margin-bottom: 0;
  line-height: 1.6;
  color: hsl(var(--muted-foreground));
}

.scope-chip,
.source-pill,
.policy-tag,
.category-count,
.result-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 26px;
  padding: 3px 9px;
  font-size: 0.75rem;
  font-weight: 650;
  color: hsl(var(--primary));
  white-space: nowrap;
  background: hsl(var(--primary) / 9%);
  border: 1px solid hsl(var(--primary) / 18%);
  border-radius: 999px;
}

.category-tabs {
  display: flex;
  gap: 24px;
  padding: 0 2px;
  margin-bottom: 16px;
  overflow-x: auto;
  border-bottom: 1px solid hsl(var(--border));
}

.tab-button {
  position: relative;
  flex: 0 0 auto;
  min-height: 44px;
  padding: 4px 2px 10px;
  font-weight: 600;
  color: hsl(var(--muted-foreground));
  cursor: pointer;
  background: transparent;
  border: 0;
}

.tab-button::after {
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  content: '';
  background: transparent;
  border-radius: 999px;
}

.tab-button[aria-pressed='true'] {
  color: hsl(var(--primary));
}

.tab-button[aria-pressed='true']::after {
  background: hsl(var(--primary));
}

.settings-summary {
  padding: 12px 14px;
  margin-bottom: 16px;
  font-size: 0.84rem;
  background: hsl(var(--muted) / 42%);
  border: 1px solid hsl(var(--border));
  border-radius: 10px;
}

.settings-summary > div {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 12px;
  align-items: baseline;
}

.settings-summary span:not(.result-count) {
  color: hsl(var(--muted-foreground));
}

.settings-groups {
  display: grid;
  gap: 20px;
}

.category-panel,
.history-card,
.page-state {
  overflow: hidden;
  background: hsl(var(--card));
  border: 1px solid hsl(var(--border));
  border-radius: 12px;
  box-shadow: 0 8px 24px hsl(var(--foreground) / 4%);
}

.category-heading {
  padding: 16px 18px;
  background: hsl(var(--muted) / 30%);
  border-bottom: 1px solid hsl(var(--border));
}

.category-heading p {
  margin-bottom: 0;
  font-size: 0.82rem;
  color: hsl(var(--muted-foreground));
}

.settings-form {
  padding: 0 18px;
}

.setting-item {
  display: grid;
  grid-template-columns: minmax(260px, 0.9fr) minmax(320px, 1.25fr);
  gap: 28px;
  padding: 20px 0;
  border-bottom: 1px solid hsl(var(--border));
}

.setting-item:last-child {
  border-bottom: 0;
}

.setting-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  align-items: center;
}

.key-tag {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 9px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.78rem;
  font-weight: 650;
  color: hsl(var(--primary));
  overflow-wrap: anywhere;
  background: hsl(var(--primary) / 8%);
  border: 1px solid hsl(var(--primary) / 20%);
  border-radius: 6px;
}

.policy-tag {
  color: hsl(32deg 85% 32%);
  background: hsl(38deg 95% 55% / 12%);
  border-color: hsl(38deg 80% 45% / 24%);
}

.policy-tag.sensitive {
  color: hsl(350deg 70% 40%);
  background: hsl(350deg 75% 55% / 10%);
  border-color: hsl(350deg 70% 45% / 20%);
}

.setting-description {
  margin: 10px 0 12px;
  font-size: 0.84rem;
  line-height: 1.55;
  color: hsl(var(--muted-foreground));
}

.setting-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
  margin: 0;
  font-size: 0.75rem;
  color: hsl(var(--muted-foreground));
}

.setting-meta div {
  display: flex;
  gap: 6px;
  align-items: center;
}

.setting-meta dt,
.setting-meta dd {
  margin: 0;
}

.setting-meta dt {
  font-weight: 650;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow-wrap: anywhere;
}

.setting-editor {
  align-self: center;
  min-width: 0;
}

.field-label {
  display: block;
  margin-bottom: 7px;
  font-size: 0.78rem;
  font-weight: 650;
}

input,
select,
textarea {
  width: 100%;
  min-height: 40px;
  padding: 8px 10px;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 8px;
  outline: none;
}

textarea {
  min-height: 96px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  resize: vertical;
}

input:focus-visible,
select:focus-visible,
textarea:focus-visible,
button:focus-visible {
  border-color: hsl(var(--primary));
  outline: 2px solid hsl(var(--primary) / 28%);
  outline-offset: 2px;
}

.switch-control {
  display: inline-flex;
  gap: 10px;
  align-items: center;
  min-height: 40px;
  padding: 4px 0;
  color: hsl(var(--foreground));
  cursor: pointer;
  background: transparent;
  border: 0;
}

.switch-track {
  position: relative;
  display: inline-flex;
  width: 42px;
  height: 24px;
  background: hsl(var(--muted-foreground) / 28%);
  border-radius: 999px;
  transition: background-color 160ms ease;
}

.switch-knob {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  background: white;
  border-radius: 50%;
  box-shadow: 0 1px 3px hsl(0deg 0% 0% / 26%);
  transition: transform 160ms ease;
}

.switch-control[aria-checked='true'] .switch-track {
  background: hsl(var(--primary));
}

.switch-control[aria-checked='true'] .switch-knob {
  transform: translateX(18px);
}

.actions-cell {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.action-button {
  min-height: 36px;
  padding: 6px 11px;
  font-size: 0.8rem;
  font-weight: 600;
  color: hsl(var(--foreground));
  cursor: pointer;
  background: hsl(var(--background));
  border: 1px solid hsl(var(--border));
  border-radius: 7px;
}

.action-button.primary {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

.action-button.subtle {
  color: hsl(var(--primary));
  background: transparent;
  border-color: transparent;
}

button:disabled,
input:disabled,
select:disabled,
textarea:disabled {
  cursor: not-allowed;
  opacity: 0.55;
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

.page-state,
.history-state {
  padding: 36px 20px;
  color: hsl(var(--muted-foreground));
  text-align: center;
}

.history-card {
  padding: 18px;
  margin-top: 20px;
}

.history-heading h2 {
  margin: 4px 0 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow-wrap: anywhere;
}

.history-list {
  display: grid;
  gap: 8px;
  padding: 0;
  margin: 14px 0 0;
  list-style: none;
}

.history-list li {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 10px 0;
  border-bottom: 1px solid hsl(var(--border));
}

.sr-status {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  white-space: nowrap;
  border: 0;
  clip-path: inset(50%);
}

@media (max-width: 900px) {
  .setting-item {
    grid-template-columns: 1fr;
    gap: 16px;
  }
}

@media (max-width: 640px) {
  .page-heading,
  .category-heading,
  .history-heading {
    align-items: flex-start;
  }

  .page-heading {
    flex-direction: column;
  }

  .category-tabs {
    gap: 18px;
  }

  .settings-form,
  .category-heading {
    padding-right: 14px;
    padding-left: 14px;
  }

  .setting-meta,
  .actions-cell {
    align-items: stretch;
  }

  .actions-cell .action-button {
    flex: 1 1 auto;
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
