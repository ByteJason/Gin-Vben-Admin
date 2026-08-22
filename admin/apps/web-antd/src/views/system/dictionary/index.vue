<script setup lang="ts">
import type {
  DictionaryItem,
  DictionaryItemInput,
  DictionaryType,
  DictionaryTypeInput,
} from '#/api/core/dictionary';

import { computed, onMounted, reactive, ref } from 'vue';

import {
  deleteDictionaryApi,
  deleteDictionaryItemApi,
  importDictionaryItemsApi,
  listDictionariesApi,
  listDictionaryItemsApi,
  saveDictionaryApi,
  saveDictionaryItemApi,
} from '#/api/core/dictionary';
import { getSettingApi } from '#/api/core/settings';
import { $t } from '#/locales';

const emptyType = (): DictionaryTypeInput => ({
  code: '',
  nameZhCN: '',
  nameEnUS: '',
  description: '',
  status: 'active',
  sortOrder: 0,
});
const emptyItem = (): DictionaryItemInput => ({
  value: '',
  labelZhCN: '',
  labelEnUS: '',
  description: '',
  tag: '',
  status: 'active',
  sortOrder: 0,
  enabled: true,
});

const types = ref<DictionaryType[]>([]);
const items = ref<DictionaryItem[]>([]);
const selectedCode = ref('');
const selectedLocale = ref('zh-CN');
const localeMode = ref<'single' | 'multi'>('single');
const includeDisabled = ref(false);
const loading = ref(false);
const saving = ref(false);
const importing = ref(false);
const deleting = ref('');
const error = ref('');
const notice = ref('');
const typeForm = reactive<DictionaryTypeInput>(emptyType());
const itemForm = reactive<DictionaryItemInput>(emptyItem());
const editingType = ref('');
const editingItem = ref('');
const importText = ref('');
const hasSelectedType = computed(() => Boolean(selectedCode.value));
const selectedType = computed(() => types.value.find((item) => item.code === selectedCode.value));
const canEditType = computed(() => Boolean(selectedType.value && !selectedType.value.systemOwned));
const canEditItem = computed(() => {
  const current = items.value.find((item) => item.id === editingItem.value);
  return !current?.systemOwned;
});

function resetTypeForm() {
  Object.assign(typeForm, emptyType());
  editingType.value = '';
}
function resetItemForm() {
  Object.assign(itemForm, emptyItem());
  editingItem.value = '';
}
function selectType(type: DictionaryType) {
  selectedCode.value = type.code;
  resetItemForm();
  void loadItems();
}
function editType(type: DictionaryType) {
  editingType.value = type.systemOwned ? '' : type.code;
  Object.assign(typeForm, {
    code: type.code,
    nameZhCN: type.nameZhCN,
    nameEnUS: type.nameEnUS,
    description: type.description ?? '',
    status: type.status,
    sortOrder: type.sortOrder,
  });
}
function editItem(item: DictionaryItem) {
  if (item.systemOwned) return;
  editingItem.value = item.id;
  Object.assign(itemForm, {
    value: item.value,
    labelZhCN: item.labelZhCN,
    labelEnUS: item.labelEnUS,
    description: item.description ?? '',
    tag: item.tag ?? '',
    status: item.status,
    sortOrder: item.sortOrder,
    enabled: item.status === 'active',
  });
}
async function loadItems() {
  if (!selectedCode.value) {
    items.value = [];
    return;
  }
  try {
    items.value = await listDictionaryItemsApi(selectedCode.value, {
      locale: selectedLocale.value,
      includeDisabled: includeDisabled.value,
    });
  } catch {
    error.value = String($t('page.dictionary.itemsLoadError'));
  }
}
async function loadLocalePolicy() {
  try {
    const [mode, defaultLocale] = await Promise.all([
      getSettingApi('i18n.mode'),
      getSettingApi('i18n.default_locale'),
    ]);
    const parsedMode = JSON.parse(mode.value);
    const parsedLocale = JSON.parse(defaultLocale.value);
    if (parsedMode === 'single' || parsedMode === 'multi') localeMode.value = parsedMode;
    if (parsedLocale === 'zh-CN' || parsedLocale === 'en-US') selectedLocale.value = parsedLocale;
  } catch {
    // The dictionary remains usable with the compiled single-language default.
  }
}
async function loadTypes() {
  loading.value = true;
  error.value = '';
  try {
    types.value = await listDictionariesApi({ includeDisabled: includeDisabled.value });
    if (!types.value.some((item) => item.code === selectedCode.value)) {
      selectedCode.value = types.value[0]?.code ?? '';
    }
    await loadItems();
  } catch {
    error.value = String($t('page.dictionary.loadError'));
  } finally {
    loading.value = false;
  }
}
async function saveType() {
  if (!typeForm.code.trim() || (!typeForm.nameZhCN?.trim() && !typeForm.nameEnUS?.trim())) {
    error.value = String($t('page.dictionary.typeRequired'));
    return;
  }
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    await saveDictionaryApi({ ...typeForm }, editingType.value || undefined);
    notice.value = String($t('page.dictionary.typeSaved'));
    selectedCode.value = typeForm.code;
    resetTypeForm();
    await loadTypes();
  } catch {
    error.value = String($t('page.dictionary.saveError'));
  } finally {
    saving.value = false;
  }
}
async function saveItem() {
  if (!selectedCode.value || !itemForm.value.trim() || (!itemForm.labelZhCN?.trim() && !itemForm.labelEnUS?.trim())) {
    error.value = String($t('page.dictionary.itemRequired'));
    return;
  }
  saving.value = true;
  error.value = '';
  notice.value = '';
  try {
    await saveDictionaryItemApi(selectedCode.value, { ...itemForm }, editingItem.value || undefined);
    notice.value = String($t('page.dictionary.itemSaved'));
    resetItemForm();
    await loadItems();
    await loadTypes();
  } catch {
    error.value = String($t('page.dictionary.saveError'));
  } finally {
    saving.value = false;
  }
}
async function removeType(type: DictionaryType) {
  if (type.systemOwned || !window.confirm(String($t('page.dictionary.confirmTypeDelete')))) return;
  deleting.value = type.code;
  try {
    await deleteDictionaryApi(type.code);
    notice.value = String($t('page.dictionary.typeDeleted'));
    resetTypeForm();
    await loadTypes();
  } catch {
    error.value = String($t('page.dictionary.deleteError'));
  } finally {
    deleting.value = '';
  }
}
async function removeItem(item: DictionaryItem) {
  if (item.systemOwned || !window.confirm(String($t('page.dictionary.confirmItemDelete')))) return;
  deleting.value = item.id;
  try {
    await deleteDictionaryItemApi(item.typeCode, item.id);
    notice.value = String($t('page.dictionary.itemDeleted'));
    await loadItems();
  } catch {
    error.value = String($t('page.dictionary.deleteError'));
  } finally {
    deleting.value = '';
  }
}
async function importItems() {
  if (!selectedCode.value || !importText.value.trim()) return;
  let parsed: unknown;
  try {
    parsed = JSON.parse(importText.value);
  } catch {
    error.value = String($t('page.dictionary.importInvalid'));
    return;
  }
  const input = Array.isArray(parsed) ? parsed : (parsed as { items?: unknown })?.items;
  if (!Array.isArray(input)) {
    error.value = String($t('page.dictionary.importInvalid'));
    return;
  }
  importing.value = true;
  error.value = '';
  try {
    await importDictionaryItemsApi(selectedCode.value, input as DictionaryItemInput[]);
    notice.value = String($t('page.dictionary.imported'));
    importText.value = '';
    await loadItems();
  } catch {
    error.value = String($t('page.dictionary.importError'));
  } finally {
    importing.value = false;
  }
}
onMounted(async () => {
  await loadLocalePolicy();
  await loadTypes();
});
</script>

<template>
  <main class="dictionary-page" :aria-busy="loading || saving || importing" aria-labelledby="dictionary-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.dictionary.eyebrow') }}</p>
        <h1 id="dictionary-title">{{ $t('page.dictionary.title') }}</h1>
        <p class="description">{{ $t('page.dictionary.description') }}</p>
      </div>
      <div class="toolbar">
        <label v-if="localeMode === 'multi'" class="compact-field">
          <span>{{ $t('page.dictionary.locale') }}</span>
          <select v-model="selectedLocale" @change="loadItems">
            <option value="zh-CN">zh-CN</option>
            <option value="en-US">en-US</option>
          </select>
        </label>
        <button class="secondary" type="button" :disabled="loading" @click="loadTypes">
          {{ $t('page.dictionary.refresh') }}
        </button>
      </div>
    </header>

    <p v-if="error" class="feedback error" role="alert" tabindex="-1">{{ error }}</p>
    <p v-if="notice" class="feedback success" role="status">{{ notice }}</p>
    <p class="sr-status" aria-live="polite">{{ loading ? $t('page.dictionary.loading') : '' }}</p>

    <section class="workspace-grid">
      <article class="panel" aria-labelledby="dictionary-types-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.dictionary.typesEyebrow') }}</p>
            <h2 id="dictionary-types-title">{{ $t('page.dictionary.typesTitle') }}</h2>
          </div>
          <button class="secondary" type="button" @click="resetTypeForm">{{ $t('page.dictionary.newType') }}</button>
        </div>
        <form class="type-form" @submit.prevent="saveType">
          <label><span>{{ $t('page.dictionary.code') }}</span><input v-model="typeForm.code" :disabled="Boolean(editingType)" required /></label>
          <label><span>{{ $t('page.dictionary.nameZhCN') }}</span><input v-model="typeForm.nameZhCN" /></label>
          <label><span>{{ $t('page.dictionary.nameEnUS') }}</span><input v-model="typeForm.nameEnUS" /></label>
          <label><span>{{ $t('page.dictionary.sortOrder') }}</span><input v-model.number="typeForm.sortOrder" min="0" type="number" /></label>
          <label class="wide"><span>{{ $t('page.dictionary.descriptionField') }}</span><input v-model="typeForm.description" /></label>
          <div class="form-actions">
            <button v-if="canEditType || !editingType" class="primary" type="submit" :disabled="saving || Boolean(editingType && !canEditType)">
              {{ saving ? $t('page.dictionary.saving') : $t('page.dictionary.save') }}
            </button>
            <button v-if="editingType" class="secondary" type="button" @click="resetTypeForm">{{ $t('page.dictionary.cancel') }}</button>
          </div>
        </form>
        <div class="table-scroll">
          <table>
            <caption class="sr-only">{{ $t('page.dictionary.typesTableLabel') }}</caption>
            <thead><tr><th scope="col">{{ $t('page.dictionary.code') }}</th><th scope="col">{{ $t('page.dictionary.status') }}</th><th scope="col">{{ $t('page.dictionary.sortOrder') }}</th><th scope="col">{{ $t('page.dictionary.actions') }}</th></tr></thead>
            <tbody>
              <tr v-if="!loading && types.length === 0"><td class="table-state" colspan="4">{{ $t('page.dictionary.emptyTypes') }}</td></tr>
              <tr v-for="type in types" :key="type.id" :class="{ selected: selectedCode === type.code }">
                <th scope="row"><button class="link-button" type="button" @click="selectType(type)">{{ type.code }}</button><small>{{ selectedLocale === 'en-US' ? type.nameEnUS : type.nameZhCN }}<span v-if="type.systemOwned" class="system-badge">{{ $t('page.dictionary.system') }}</span></small></th>
                <td><span :class="['status-pill', type.status === 'active' ? 'ok' : 'off']">{{ type.status === 'active' ? $t('page.dictionary.active') : $t('page.dictionary.disabled') }}</span></td>
                <td>{{ type.sortOrder }}</td>
                <td class="actions"><button type="button" @click="editType(type)">{{ type.systemOwned ? $t('page.dictionary.view') : $t('page.dictionary.edit') }}</button><button v-if="!type.systemOwned" class="danger" type="button" :disabled="deleting === type.code" @click="removeType(type)">{{ $t('page.dictionary.delete') }}</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>

      <article class="panel" aria-labelledby="dictionary-items-title">
        <div class="section-heading">
          <div>
            <p class="eyebrow">{{ $t('page.dictionary.itemsEyebrow') }}</p>
            <h2 id="dictionary-items-title">{{ selectedCode || $t('page.dictionary.itemsTitle') }}</h2>
          </div>
          <label class="toggle"><input v-model="includeDisabled" type="checkbox" @change="loadTypes" /><span>{{ $t('page.dictionary.includeDisabled') }}</span></label>
        </div>
        <p v-if="!hasSelectedType" class="empty-state">{{ $t('page.dictionary.selectType') }}</p>
        <template v-else>
          <form class="item-form" @submit.prevent="saveItem">
            <label><span>{{ $t('page.dictionary.value') }}</span><input v-model="itemForm.value" :disabled="Boolean(editingItem && !canEditItem)" required /></label>
            <label><span>{{ $t('page.dictionary.labelZhCN') }}</span><input v-model="itemForm.labelZhCN" /></label>
            <label><span>{{ $t('page.dictionary.labelEnUS') }}</span><input v-model="itemForm.labelEnUS" /></label>
            <label><span>{{ $t('page.dictionary.tag') }}</span><input v-model="itemForm.tag" /></label>
            <label><span>{{ $t('page.dictionary.sortOrder') }}</span><input v-model.number="itemForm.sortOrder" min="0" type="number" /></label>
            <label class="toggle"><input v-model="itemForm.enabled" type="checkbox" /><span>{{ $t('page.dictionary.enabled') }}</span></label>
            <div class="form-actions"><button class="primary" type="submit" :disabled="saving || Boolean(editingItem && !canEditItem)">{{ saving ? $t('page.dictionary.saving') : $t('page.dictionary.saveItem') }}</button><button v-if="editingItem" class="secondary" type="button" @click="resetItemForm">{{ $t('page.dictionary.cancel') }}</button></div>
          </form>
          <div class="table-scroll">
            <table>
              <caption class="sr-only">{{ $t('page.dictionary.itemsTableLabel') }}</caption>
              <thead><tr><th scope="col">{{ $t('page.dictionary.value') }}</th><th scope="col">{{ $t('page.dictionary.localizedLabel') }}</th><th scope="col">{{ $t('page.dictionary.status') }}</th><th scope="col">{{ $t('page.dictionary.sortOrder') }}</th><th scope="col">{{ $t('page.dictionary.actions') }}</th></tr></thead>
              <tbody>
                <tr v-if="items.length === 0"><td class="table-state" colspan="5">{{ $t('page.dictionary.emptyItems') }}</td></tr>
                <tr v-for="item in items" :key="item.id">
                  <th scope="row"><span class="primary-text">{{ item.value }}</span><small>{{ item.systemOwned ? $t('page.dictionary.system') : $t('page.dictionary.tenantOverride') }}</small></th>
                  <td>{{ item.label }}</td>
                  <td><span :class="['status-pill', item.status === 'active' ? 'ok' : 'off']">{{ item.status === 'active' ? $t('page.dictionary.active') : $t('page.dictionary.disabled') }}</span></td>
                  <td>{{ item.sortOrder }}</td>
                  <td class="actions"><button type="button" :disabled="item.systemOwned" @click="editItem(item)">{{ item.systemOwned ? $t('page.dictionary.view') : $t('page.dictionary.edit') }}</button><button v-if="!item.systemOwned" class="danger" type="button" :disabled="deleting === item.id" @click="removeItem(item)">{{ $t('page.dictionary.delete') }}</button></td>
                </tr>
              </tbody>
            </table>
          </div>
          <details class="import-card">
            <summary>{{ $t('page.dictionary.importTitle') }}</summary>
            <label><span>{{ $t('page.dictionary.importHelp') }}</span><textarea v-model="importText" rows="5" :placeholder="$t('page.dictionary.importPlaceholder')" /></label>
            <button class="secondary" type="button" :disabled="importing" @click="importItems">{{ importing ? $t('page.dictionary.importing') : $t('page.dictionary.import') }}</button>
          </details>
        </template>
      </article>
    </section>
  </main>
</template>

<style scoped>
.dictionary-page { --ink:#172033; --muted:#64748b; --line:#dbe3ef; --accent:#2563eb; --ok:#15803d; --danger:#b42318; max-width:1600px; margin:0 auto; padding:32px; color:var(--ink); }
.page-heading,.section-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; }
.toolbar { display:flex; align-items:end; gap:12px; }.compact-field,.type-form label,.item-form label,.import-card label { display:grid; gap:7px; font-size:.82rem; font-weight:700; }.compact-field select,.type-form input,.item-form input,.import-card textarea { min-height:40px; border:1px solid #cbd5e1; border-radius:9px; padding:8px 10px; color:var(--ink); background:#fff; }.compact-field select:focus,.type-form input:focus,.item-form input:focus,.import-card textarea:focus,button:focus-visible { outline:3px solid rgb(37 99 235 / 25%); outline-offset:2px; }
.eyebrow { margin:0 0 6px; color:#5267d9; font-size:.72rem; font-weight:800; letter-spacing:.12em; } h1{margin:0 0 8px;font-size:clamp(1.7rem,4vw,2.5rem)} h2{margin:0;font-size:1.15rem}.description,.muted,small{color:var(--muted)}.workspace-grid{display:grid;grid-template-columns:minmax(360px,.85fr) minmax(480px,1.5fr);gap:24px;margin-top:24px}.panel{border:1px solid var(--line);border-radius:16px;background:color-mix(in srgb,#fff 94%,#dbeafe);padding:24px;box-shadow:0 10px 28px rgb(30 41 59 / 7%)}.type-form,.item-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px;margin:20px 0}.type-form .wide{grid-column:1/-1}.form-actions{display:flex;align-items:end;gap:8px}.primary,.secondary,.actions button{min-height:40px;border:1px solid #cbd5e1;border-radius:9px;padding:0 13px;cursor:pointer;background:#fff;transition:transform .18s ease,box-shadow .18s ease}.primary{border-color:var(--accent);background:var(--accent);color:#fff;font-weight:700}.primary:hover,.secondary:hover,.actions button:hover{transform:translateY(-1px);box-shadow:0 5px 14px rgb(15 23 42 / 12%)}.danger{color:var(--danger)}.feedback{margin:20px 0 0;border-radius:10px;padding:12px 14px}.error{color:#8b1e1e;background:#fef2f2}.success{color:#166534;background:#f0fdf4}.table-scroll{overflow-x:auto;margin-top:18px}table{width:100%;border-collapse:collapse;min-width:560px}th,td{border-bottom:1px solid var(--line);padding:11px 9px;text-align:left;vertical-align:middle}th{color:var(--muted);font-size:.74rem;letter-spacing:.04em;text-transform:uppercase}td small{display:block;margin-top:3px;font-weight:400}.actions{display:flex;gap:7px;flex-wrap:wrap}.link-button{border:0;background:transparent;color:var(--accent);font-weight:700;padding:0;cursor:pointer;text-align:left}.selected{background:#eff6ff}.system-badge{display:inline-flex;margin-left:5px;border-radius:999px;padding:2px 6px;background:#e0e7ff;color:#3730a3;font-size:.68rem}.status-pill{display:inline-flex;border-radius:999px;padding:4px 9px;font-size:.74rem;font-weight:800}.status-pill.ok{color:var(--ok);background:#dcfce7}.status-pill.off{color:#92400e;background:#fef3c7}.toggle{display:flex!important;align-items:center;gap:8px!important;padding-top:20px}.toggle input{width:18px;height:18px}.empty-state,.sr-status{color:var(--muted)}.table-state{text-align:center;color:var(--muted)}.import-card{margin-top:20px;border-top:1px solid var(--line);padding-top:16px}.import-card summary{cursor:pointer;font-weight:800;margin-bottom:12px}.import-card textarea{width:100%;resize:vertical;margin-bottom:10px}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
@media (max-width:1100px){.workspace-grid{grid-template-columns:1fr}.dictionary-page{padding:22px 16px}}@media (max-width:560px){.page-heading,.section-heading,.toolbar{flex-direction:column;align-items:stretch}.type-form,.item-form{grid-template-columns:1fr}.type-form .wide{grid-column:auto}.toggle{padding-top:0}.form-actions{align-items:stretch;flex-wrap:wrap}}@media (prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;transition-duration:.01ms!important;animation-duration:.01ms!important}}
</style>
