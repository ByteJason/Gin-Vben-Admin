<script setup lang="ts">
import type {
  SettingApplyMode,
  SettingDefinition,
  SettingModuleDefinition,
  SettingModuleSaveResult,
  SettingModuleView,
} from '#/api/core/settings';

import { computed, onMounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage, notify } from '@vben/common-ui';

import {
  getSettingApi,
  clearSettingModuleCredentialsApi,
  getSettingModuleApi,
  listSettingDefinitionsApi,
  listSettingModulesApi,
  resetSettingModuleApi,
  updateSettingModuleApi,
  validateSettingModuleApi,
} from '#/api/core/settings';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['system:settings:manage']));
const modules = ref<SettingModuleDefinition[]>([]);
const views = reactive<Record<string, SettingModuleView>>({});
const drafts = reactive<Record<string, Record<string, string>>>({});
const selectedModule = ref('');
const search = ref('');
const loading = ref(true);
const saving = ref('');
const validating = ref('');
const resetting = ref('');
const clearing = ref('');
const error = ref('');
const notice = ref('');

const activeView = computed(() => views[selectedModule.value]);
// Named grouping boundary for shared decorators and responsive card layouts.
const groupedDefinitions = computed(() => visibleDefinitions.value);
const visibleDefinitions = computed(() => {
  const view = activeView.value;
  const keyword = search.value.trim().toLocaleLowerCase();
  if (!view) return [];
  if (!keyword) return view.definitions;
  return view.definitions.filter((definition) =>
    [definition.displayName, definition.key, definition.description]
      .filter(Boolean)
      .some((item) => item!.toLocaleLowerCase().includes(keyword)),
  );
});
const draftCount = computed(() =>
  Object.values(drafts).reduce((count, values) => count + Object.keys(values).length, 0),
);

function t(key: string, fallback: string) {
  const value = String($t(key));
  return value === key ? fallback : value;
}

function applyModeLabel(mode: string) {
  return t(`page.settings.applyMode.${mode}`, mode);
}

function sourceLabel(source: string | undefined) {
  return t(`page.settings.source.${source ?? 'default'}`, source ?? 'default');
}

function statusLabel(status: string | undefined) {
  return t(`page.settings.status.${status ?? 'saved_and_applied'}`, status ?? 'saved');
}

function settingFor(view: SettingModuleView, key: string) {
  return view.settings.find((setting) => setting.key === key);
}

function definitionDefault(definition: SettingDefinition) {
  if (definition.sensitive) return '';
  return decodeRaw(definition.default, definition);
}

function decodeRaw(raw: string | undefined, definition: SettingDefinition) {
  if (raw === undefined || definition.sensitive) return '';
  try {
    const parsed = JSON.parse(raw);
    return definition.kind === 'json'
      ? JSON.stringify(parsed, null, 2)
      : parsed === null || parsed === undefined
        ? ''
        : String(parsed);
  } catch {
    return raw;
  }
}

function draftFor(module: string, key: string) {
  return drafts[module]?.[key];
}

function displayValue(view: SettingModuleView, definition: SettingDefinition) {
  const draft = draftFor(view.module, definition.key);
  if (draft !== undefined) return draft;
  return decodeRaw(settingFor(view, definition.key)?.value, definition) || definitionDefault(definition);
}

function setDraft(module: string, key: string, value: string) {
  if (!drafts[module]) drafts[module] = {};
  drafts[module][key] = value;
}

function boolValue(view: SettingModuleView, definition: SettingDefinition) {
  return displayValue(view, definition).toLowerCase() === 'true';
}

function toggle(view: SettingModuleView, definition: SettingDefinition) {
  if (!canManage.value || !definition.editable) return;
  setDraft(view.module, definition.key, boolValue(view, definition) ? 'false' : 'true');
}

function fieldId(module: string, key: string) {
  return `setting-${module}-${key}`.replace(/[^a-zA-Z0-9_-]/g, '-');
}

async function copyKey(key: string) {
  try {
    await navigator.clipboard?.writeText(key);
    notice.value = t('page.settings.copyKeyDone', '配置键已复制');
  } catch {
    notice.value = t('page.settings.copyKeyFailed', '复制失败，请手动选择');
  }
}

function parseValue(definition: SettingDefinition, value: string): unknown {
  if (definition.kind === 'secret' && value.trim() === '') return undefined;
  if (definition.kind === 'string' || definition.kind === 'secret') return value;
  if (definition.kind === 'number') {
    const number = Number(value);
    if (!Number.isFinite(number)) throw new Error(t('page.settings.invalidValue', '请输入有效数值'));
    return number;
  }
  try {
    return JSON.parse(value.trim());
  } catch {
    throw new Error(t('page.settings.invalidValue', '请输入有效值'));
  }
}

function changedValues(view: SettingModuleView) {
  const values: Record<string, unknown> = {};
  const moduleDrafts = drafts[view.module] ?? {};
  for (const definition of view.definitions) {
    if (!Object.prototype.hasOwnProperty.call(moduleDrafts, definition.key)) continue;
    const parsed = parseValue(definition, moduleDrafts[definition.key] ?? '');
    if (parsed !== undefined) values[definition.key] = parsed;
  }
  return values;
}

function discard(module: string) {
  delete drafts[module];
  notice.value = t('page.settings.discarded', '已放弃未保存修改');
}

function mergeSaveResult(view: SettingModuleView, result: SettingModuleSaveResult) {
  views[view.module] = {
    ...view,
    revision: result.revision,
    status: result.status,
    applyMode: result.applyMode,
    requiresRestart: result.requiresRestart,
    otherNodesPending: result.otherNodesPending,
    applyError: result.applyError,
    updatedAt: result.updatedAt,
    settings: result.settings?.length ? result.settings : view.settings,
  };
}

async function loadModule(module: string) {
  views[module] = await getSettingModuleApi(module);
}

function normalizeLegacyDefinition(
  input: Partial<SettingDefinition> & { key: string; label?: string },
): SettingDefinition {
  const kind = input.kind ?? 'string';
  const applyMode = input.applyMode ?? (input.restartRequired ? 'restart' : 'immediate');
  return {
    ...input,
    applyMode,
    category: input.category ?? 'other',
    displayName: input.displayName ?? input.label ?? input.key,
    editable: input.editable ?? true,
    group: input.group ?? input.category ?? 'other',
    key: input.key,
    kind,
    sensitive: input.sensitive ?? false,
  } as SettingDefinition;
}

async function loadLegacyModules() {
  const legacyDefinitions = (await listSettingDefinitionsApi()).map((definition) =>
    normalizeLegacyDefinition(definition),
  );
  const activeDefinitions = legacyDefinitions.filter(
    (definition) =>
      !definition.deprecated &&
      !/^((mail|email|smtp)(\.|$))/i.test(definition.key),
  );
  const grouped = new Map<string, SettingDefinition[]>();
  for (const definition of activeDefinitions) {
    const module = definition.group || definition.category || 'other';
    const items = grouped.get(module) ?? [];
    items.push(definition);
    grouped.set(module, items);
  }
  const moduleNames: Record<string, string> = {
    basic: '基础设置',
    captcha: '验证码',
    file: '文件与存储',
    i18n: '语言与区域',
    observability: '可观测性',
    other: '其他设置',
    security: '安全设置',
  };
  const fallbackModules: SettingModuleDefinition[] = [];
  for (const [module, definitions] of grouped) {
    const settings = await Promise.all(
      definitions.map(async (definition) => {
        try {
          return await getSettingApi(definition.key);
        } catch {
          return {
            category: definition.category,
            displayName: definition.displayName,
            editable: definition.editable,
            group: definition.group,
            key: definition.key,
            sensitive: definition.sensitive,
            source: 'default' as const,
            value: definition.default ?? 'null',
            version: 0,
          };
        }
      }),
    );
    const revision = settings.reduce(
      (max, setting) => Math.max(max, setting.version ?? 0),
      0,
    );
    const sourceSet = new Set(settings.map((setting) => setting.source).filter(Boolean));
    const source = sourceSet.size === 1 ? [...sourceSet][0] : undefined;
    const applyMode = definitions.reduce<SettingApplyMode>(
      (current, definition) => {
        const rank: Record<SettingApplyMode, number> = {
          immediate: 1,
          component_reload: 2,
          restart: 3,
          deployment: 4,
          migration: 5,
        };
        const mode = definition.applyMode ?? 'immediate';
        return rank[mode] > rank[current]
          ? mode
          : current;
      },
      'immediate',
    );
    const category = (module === 'observability'
      ? 'observability'
      : definitions[0]?.category ?? 'other') as SettingModuleDefinition['category'];
    const displayName = moduleNames[module] ?? module;
    fallbackModules.push({
      applyMode,
      category,
      description: '兼容旧服务端的系统设置模块',
      displayName,
      editable: definitions.some((definition) => definition.editable),
      group: module,
      id: module,
      keys: definitions.map((definition) => definition.key).sort(),
      name: displayName,
      scope: 'tenant',
    });
    views[module] = {
      applyMode,
      category,
      definitions: definitions.sort((a, b) => a.key.localeCompare(b.key)),
      description: '兼容旧服务端的系统设置模块',
      displayName,
      group: module,
      id: module,
      module,
      name: displayName,
      requiresRestart: ['deployment', 'migration', 'restart'].includes(applyMode),
      revision,
      settings,
      source,
      status: 'saved_and_applied',
      otherNodesPending: false,
    };
  }
  return fallbackModules.sort((a, b) => a.id.localeCompare(b.id));
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    try {
      modules.value = await listSettingModulesApi();
      await Promise.all(modules.value.map((module) => loadModule(module.id)));
    } catch (moduleError) {
      // A rolling deployment may briefly serve the pre-module API. This
      // compatibility reader remains read-only and filters retired keys.
      modules.value = await loadLegacyModules();
      if (!modules.value.length) throw moduleError;
    }
    selectedModule.value = modules.value[0]?.id ?? '';
  } catch {
    error.value = t('page.settings.loadError', '系统设置加载失败，请稍后重试');
    notify('error', error.value);
  } finally {
    loading.value = false;
  }
}

async function save(view: SettingModuleView) {
  if (!canManage.value || !view || !Object.keys(drafts[view.module] ?? {}).length) return;
  saving.value = view.module;
  error.value = '';
  notice.value = '';
  try {
    const result = await updateSettingModuleApi(view.module, {
      expectedRevision: view.revision,
      values: changedValues(view),
    });
    mergeSaveResult(view, result);
    delete drafts[view.module];
    notice.value = `${statusLabel(result.status)} · ${result.changedKeys.length} ${t('page.settings.changedKeys', '项')}`;
    notify(result.status === 'saved_apply_failed' ? 'warning' : 'success', notice.value);
  } catch {
    error.value = t('page.settings.saveError', '保存失败，请检查权限、校验结果或修订号');
    notify('error', error.value);
  } finally {
    saving.value = '';
  }
}

async function validate(view: SettingModuleView) {
  if (!view) return;
  validating.value = view.module;
  error.value = '';
  try {
    const result = await validateSettingModuleApi(view.module, {
      expectedRevision: view.revision,
      values: changedValues(view),
    });
    notice.value = result.valid
      ? t('page.settings.validationPassed', '配置校验通过')
      : t('page.settings.validationFailed', '配置校验未通过');
    notify(result.valid ? 'success' : 'warning', notice.value);
  } catch {
    error.value = t('page.settings.validationFailed', '配置校验未通过');
    notify('error', error.value);
  } finally {
    validating.value = '';
  }
}

async function restoreDefaults(view: SettingModuleView) {
  if (!canManage.value || !view) return;
  resetting.value = view.module;
  error.value = '';
  try {
    const result = await resetSettingModuleApi(view.module, { expectedRevision: view.revision });
    mergeSaveResult(view, result);
    delete drafts[view.module];
    notice.value = t('page.settings.defaultsRestored', '已移除数据库覆盖，恢复继承默认来源');
    notify('success', notice.value);
  } catch {
    error.value = t('page.settings.saveError', '保存失败，请稍后重试');
    notify('error', error.value);
  } finally {
    resetting.value = '';
  }
}

async function clearCredential(view: SettingModuleView, definition: SettingDefinition) {
  if (!canManage.value || !definition.sensitive || !definition.editable) return;
  const confirmation = t('page.settings.confirmClearCredential', '确认清除该凭据？清除后需要重新填写才会恢复。');
  if (typeof window !== 'undefined' && typeof window.confirm === 'function' && !window.confirm(confirmation)) return;
  const operation = `${view.module}:${definition.key}`;
  clearing.value = operation;
  error.value = '';
  try {
    const result = await clearSettingModuleCredentialsApi(view.module, {
      expectedRevision: view.revision,
      keys: [definition.key],
    });
    mergeSaveResult(view, result);
    const moduleDrafts = drafts[view.module];
    if (moduleDrafts) {
      delete moduleDrafts[definition.key];
      if (!Object.keys(moduleDrafts).length) delete drafts[view.module];
    }
    notice.value = t('page.settings.credentialCleared', '凭据已清除');
    notify(result.status === 'saved_apply_failed' ? 'warning' : 'success', notice.value);
  } catch {
    error.value = t('page.settings.clearCredentialError', '凭据清除失败，请检查权限或修订号');
    notify('error', error.value);
  } finally {
    clearing.value = '';
  }
}

onMounted(load);
</script>

<template>
  <ManagementPage class="settings-page" :aria-busy="loading" aria-labelledby="system-settings-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ t('page.settings.eyebrow', 'SYSTEM SETTINGS') }}</p>
        <h1 id="system-settings-title">{{ t('page.settings.title', '系统设置') }}</h1>
        <p class="description">{{ t('page.settings.description', '按业务模块统一校验、保存并应用运行时配置。') }}</p>
      </div>
      <div class="heading-status">
        <span class="status-pill">{{ draftCount ? `${draftCount} ${t('page.settings.unsaved', '项未保存')}` : t('page.settings.allSaved', '全部已保存') }}</span>
      </div>
    </header>

    <p v-if="error" class="alert" role="alert" tabindex="-1">{{ error }}</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>

    <div class="toolbar">
      <label class="search-box">
        <span class="sr-only">{{ t('page.settings.search', '搜索设置') }}</span>
        <input v-model="search" type="search" :placeholder="t('page.settings.searchPlaceholder', '搜索名称或配置键')" />
      </label>
      <span class="toolbar-hint">{{ t('page.settings.moduleHint', '每个模块一次保存并应用') }}</span>
    </div>

    <div v-if="loading" class="page-state">{{ t('page.settings.loading', '正在加载系统设置…') }}</div>
    <div v-else-if="!modules.length" class="page-state">{{ t('page.settings.empty', '暂无可管理的系统设置') }}</div>
    <div v-else class="settings-layout">
      <nav class="module-nav" :aria-label="t('page.settings.moduleLabel', '设置模块')">
        <button
          v-for="module in modules"
          :key="module.id"
          class="module-tab"
          :class="{ active: selectedModule === module.id }"
          type="button"
          :aria-pressed="selectedModule === module.id"
          @click="selectedModule = module.id"
        >
          <strong>{{ module.displayName || module.name }}</strong>
          <small>{{ applyModeLabel(module.applyMode) }}</small>
        </button>
      </nav>

      <section v-if="activeView" class="module-panel" :aria-labelledby="`module-${activeView.module}`">
        <header class="module-heading">
          <div>
            <p class="eyebrow">{{ activeView.category }}</p>
            <h2 :id="`module-${activeView.module}`">{{ activeView.displayName || activeView.name }}</h2>
            <p>{{ activeView.description }}</p>
          </div>
          <div class="module-meta">
            <span class="meta-pill">{{ applyModeLabel(activeView.applyMode) }}</span>
            <span class="meta-pill">{{ statusLabel(activeView.status) }}</span>
            <span v-if="activeView.source" class="meta-pill">{{ sourceLabel(activeView.source) }}</span>
            <span v-if="activeView.otherNodesPending" class="meta-pill warning">{{ t('page.settings.otherNodesPending', '其他节点待同步') }}</span>
            <span v-if="activeView.updatedAt" class="revision">{{ t('page.settings.updatedAt', '更新于') }} {{ activeView.updatedAt }}</span>
            <span v-if="activeView.updatedBy" class="revision">{{ t('page.settings.updatedBy', '操作人') }} {{ activeView.updatedBy }}</span>
          </div>
        </header>
        <p v-if="activeView.applyError" class="alert" role="alert">{{ t('page.settings.applyError', '运行时应用失败，当前仍使用上一份有效配置') }}：{{ activeView.applyError }}</p>

        <div class="settings-groups">
          <article v-for="definition in groupedDefinitions" :key="definition.key" class="category-panel">
            <div class="setting-copy">
              <div class="setting-title-row">
                <h3>{{ definition.displayName }}</h3>
                <span v-if="definition.sensitive" class="tag">{{ t('page.settings.sensitive', '敏感') }}</span>
                <span v-if="!definition.editable" class="tag locked">{{ t('page.settings.readOnly', '只读') }}</span>
              </div>
              <p>{{ definition.description }}</p>
              <div class="setting-meta">
                <span>{{ applyModeLabel(definition.applyMode) }}</span>
                <span>{{ sourceLabel(settingFor(activeView, definition.key)?.source) }}</span>
                <span v-if="definition.unit">{{ definition.unit }}</span>
              </div>
              <details class="technical-details">
                <summary>{{ t('page.settings.technicalDetails', '技术信息') }}</summary>
                <code class="key-tag">{{ definition.key }}</code>
                <button type="button" class="copy-key" @click="copyKey(definition.key)">{{ t('page.settings.copyKey', '复制配置键') }}</button>
              </details>
            </div>
            <div class="setting-editor">
              <button
                v-if="definition.kind === 'bool'"
                class="switch-control"
                :class="{ on: boolValue(activeView, definition) }"
                role="switch"
                type="button"
                :aria-checked="boolValue(activeView, definition)"
                :disabled="!canManage || !definition.editable || saving === activeView.module"
                @click="toggle(activeView, definition)"
              >
                {{ boolValue(activeView, definition) ? t('page.settings.enabled', '已启用') : t('page.settings.disabled', '未启用') }}
              </button>
              <select
                v-else-if="(definition.allowedValues ?? definition.allowed)?.length"
                :id="fieldId(activeView.module, definition.key)"
                :aria-label="definition.displayName"
                :value="displayValue(activeView, definition)"
                :disabled="!canManage || !definition.editable || saving === activeView.module"
                @change="setDraft(activeView.module, definition.key, ($event.target as HTMLSelectElement).value)"
              >
                <option v-for="option in definition.allowedValues ?? definition.allowed" :key="option" :value="option">{{ option }}</option>
              </select>
              <textarea
                v-else-if="definition.kind === 'json'"
                :id="fieldId(activeView.module, definition.key)"
                :aria-label="definition.displayName"
                :value="displayValue(activeView, definition)"
                :placeholder="definition.placeholder || definition.inputHint"
                :disabled="!canManage || !definition.editable || saving === activeView.module"
                rows="3"
                @input="setDraft(activeView.module, definition.key, ($event.target as HTMLTextAreaElement).value)"
              ></textarea>
              <input
                v-else
                :id="fieldId(activeView.module, definition.key)"
                :aria-label="definition.displayName"
                :value="displayValue(activeView, definition)"
                :type="definition.sensitive ? 'password' : definition.kind === 'number' ? 'number' : 'text'"
                :placeholder="definition.sensitive ? t('page.settings.secretPlaceholder', '留空表示不修改') : definition.placeholder || definition.inputHint"
                :disabled="!canManage || !definition.editable || saving === activeView.module"
                @input="setDraft(activeView.module, definition.key, ($event.target as HTMLInputElement).value)"
              />
              <button
                v-if="definition.sensitive && definition.editable"
                class="clear-credential"
                type="button"
                :disabled="!canManage || saving === activeView.module || clearing === `${activeView.module}:${definition.key}`"
                @click="clearCredential(activeView, definition)"
              >
                {{ clearing === `${activeView.module}:${definition.key}` ? t('page.settings.clearingCredential', '清除中…') : t('page.settings.clearCredential', '清除凭据') }}
              </button>
            </div>
          </article>
        </div>
        <p v-if="!visibleDefinitions.length" class="page-state">{{ t('page.settings.noMatch', '没有匹配的设置') }}</p>

        <footer class="module-actions">
          <button class="secondary" type="button" :disabled="saving === activeView.module || validating === activeView.module" @click="discard(activeView.module)">{{ t('page.settings.discard', '放弃修改') }}</button>
          <button class="secondary" type="button" :disabled="validating === activeView.module || !Object.keys(drafts[activeView.module] ?? {}).length" @click="validate(activeView)">{{ validating === activeView.module ? t('page.settings.validating', '校验中…') : t('page.settings.validate', '校验配置') }}</button>
          <button class="secondary" type="button" :disabled="resetting === activeView.module || !canManage" @click="restoreDefaults(activeView)">{{ resetting === activeView.module ? t('page.settings.restoring', '恢复中…') : t('page.settings.restoreDefaults', '恢复默认') }}</button>
          <button class="primary" type="button" :disabled="saving === activeView.module || !canManage || !Object.keys(drafts[activeView.module] ?? {}).length" @click="save(activeView)">{{ saving === activeView.module ? t('page.settings.saving', '保存中…') : t('page.settings.saveApply', '保存并应用') }}</button>
        </footer>
      </section>
    </div>
  </ManagementPage>
</template>

<style scoped>
.settings-page { --settings-border: color-mix(in srgb, currentColor 14%, transparent); --settings-muted: color-mix(in srgb, currentColor 62%, transparent); padding: 24px; }
.page-heading, .module-heading, .toolbar, .module-actions, .setting-title-row, .setting-meta, .module-meta { display: flex; align-items: center; gap: 12px; }
.page-heading, .module-heading, .toolbar, .module-actions { justify-content: space-between; }
.page-heading { margin-bottom: 20px; align-items: flex-start; }
.eyebrow { color: var(--settings-muted); font-size: 11px; font-weight: 700; letter-spacing: .12em; margin: 0 0 6px; text-transform: uppercase; }
h1, h2, h3, p { margin-top: 0; }
.description, .module-heading p, .setting-copy p { color: var(--settings-muted); }
.description { max-width: 760px; }
.status-pill, .meta-pill, .tag { border: 1px solid var(--settings-border); border-radius: 999px; font-size: 12px; padding: 4px 9px; white-space: nowrap; }
.meta-pill.warning { color: #b45309; border-color: #f59e0b; }
.alert, .notice { border-radius: 8px; margin: 12px 0; padding: 10px 12px; }
.alert { background: color-mix(in srgb, #ef4444 12%, transparent); }
.notice { background: color-mix(in srgb, #22c55e 12%, transparent); }
.toolbar { margin-bottom: 16px; }
.search-box { flex: 1; max-width: 480px; }
.search-box input { width: 100%; }
.toolbar-hint { color: var(--settings-muted); font-size: 13px; }
.settings-layout { display: grid; gap: 20px; grid-template-columns: minmax(180px, 230px) minmax(0, 1fr); }
.module-nav { display: flex; flex-direction: column; gap: 8px; }
.module-tab { background: transparent; border: 1px solid var(--settings-border); border-radius: 10px; cursor: pointer; display: flex; flex-direction: column; gap: 5px; padding: 12px; text-align: left; }
.module-tab small { color: var(--settings-muted); }
.module-tab.active { border-color: var(--color-primary, #1677ff); box-shadow: 0 0 0 1px var(--color-primary, #1677ff); }
.module-panel { min-width: 0; }
.module-heading { align-items: flex-start; border-bottom: 1px solid var(--settings-border); padding-bottom: 16px; }
.module-meta { flex-wrap: wrap; justify-content: flex-end; }
.revision { color: var(--settings-muted); font-family: ui-monospace, monospace; font-size: 12px; }
.settings-grid, .settings-groups { display: grid; gap: 20px; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); padding: 18px 0; }
.category-panel { border: 1px solid var(--settings-border); border-radius: 10px; display: flex; flex-direction: column; gap: 16px; justify-content: space-between; min-height: 190px; padding: 16px; }
.setting-title-row { justify-content: space-between; }
.setting-title-row h3 { margin-bottom: 0; }
.tag { font-size: 11px; }
.key-tag { color: var(--settings-muted); font-family: ui-monospace, monospace; font-size: 10px; max-width: 100%; overflow-wrap: anywhere; }
.tag.locked { color: var(--settings-muted); }
.setting-meta { color: var(--settings-muted); flex-wrap: wrap; font-size: 12px; }
.setting-editor input, .setting-editor select, .setting-editor textarea { box-sizing: border-box; min-height: 38px; width: 100%; }
.setting-editor textarea { resize: vertical; }
.clear-credential { border: 1px solid color-mix(in srgb, #ef4444 55%, var(--settings-border)); border-radius: 7px; color: #b91c1c; cursor: pointer; margin-top: 8px; padding: 7px 10px; }
.switch-control { border: 1px solid var(--settings-border); border-radius: 999px; cursor: pointer; padding: 8px 14px; }
.switch-control.on { background: var(--color-primary, #1677ff); color: white; }
.technical-details { color: var(--settings-muted); font-size: 12px; margin-top: 12px; }
.technical-details code { display: block; margin: 8px 0; overflow-wrap: anywhere; }
.copy-key, .secondary, .primary { border: 1px solid var(--settings-border); border-radius: 7px; cursor: pointer; padding: 8px 12px; }
.primary { background: var(--color-primary, #1677ff); border-color: transparent; color: white; }
button:disabled, input:disabled, select:disabled, textarea:disabled { cursor: not-allowed; opacity: .55; }
.module-actions { border-top: 1px solid var(--settings-border); flex-wrap: wrap; padding-top: 16px; }
.page-state { color: var(--settings-muted); padding: 40px 12px; text-align: center; }
.sr-only { height: 1px; margin: -1px; overflow: hidden; position: absolute; width: 1px; clip: rect(0, 0, 0, 0); }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: 0.01ms !important; animation-duration: 0.01ms !important; animation-iteration-count: 1 !important; } }
@media (max-width: 760px) { .settings-page { padding: 16px; } .settings-layout { grid-template-columns: 1fr; } .module-nav { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); } .module-heading { flex-direction: column; } .module-meta { justify-content: flex-start; } .toolbar { align-items: stretch; flex-direction: column; } .search-box { max-width: none; } }
</style>
