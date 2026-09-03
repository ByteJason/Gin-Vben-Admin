<script setup lang="ts">
import type {
  FileACL,
  FileCategory,
  FileCategoryInput,
  FileObject,
  FilePage,
  MediaResource,
  MediaUsage,
} from '#/api/core/files';

import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
} from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';
import { preferences } from '@vben/preferences';
import { commonCapabilitiesGuide } from '@vben/types';

import {
	attachMediaUsageApi,
	cleanupDryRunApi,
  createFileCategoryApi,
	deleteFileApi,
	deleteFileCategoryApi,
	deleteMediaResourceApi,
	detachMediaUsageApi,
	downloadFileApi,
	getBrandingSettingsApi,
	getMediaResourceApi,
  listFileCategoriesApi,
  listFilesApi,
	listMediaResourcesApi,
  listMediaUsagesApi,
  openMediaResourceApi,
  signedFileUrlApi,
  updateBrandingSettingsApi,
  updateFileCategoryApi,
  uploadFileApi,
  uploadMediaResourceApi,
} from '#/api/core/files';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(
  () =>
    hasAccessByCodes(['media:library:manage']) ||
    hasAccessByCodes(['system:files:manage']),
);
const categories = ref<FileCategory[]>([]);
const selectedCategoryId = ref('');
const categoryName = ref('');
const categoryParentId = ref('');
const categoryBusy = ref(false);
const categoryError = ref('');
const page = ref<FilePage>({ items: [], limit: 50, offset: 0, total: 0 });
const selectedFile = ref<File | null>(null);
const selectedAsset = ref<FileObject | null>(null);
const logoAsset = ref<MediaResource | null>(null);
const logoPickerOpen = ref(false);
const logoAssets = ref<MediaResource[]>([]);
const logoUploadInput = ref<HTMLInputElement | null>(null);
const logoDialog = ref<HTMLElement | null>(null);
let logoReturnFocus: HTMLElement | null = null;
const logoStorageKey = 'media-library:logo-resource-id';
const logoBusy = ref(false);
const previewURLs = ref<Record<string, string>>({});
const previewLoading = new Set<string>();
const acl = ref<FileACL>('private');
const loading = ref(false);
const uploading = ref(false);
const actionId = ref('');
const error = ref('');
const guideOpen = ref(false);
const guideDrawer = ref<HTMLElement | null>(null);
let guideReturnFocus: HTMLElement | null = null;
const guide = computed(
  () =>
    commonCapabilitiesGuide.files.locales?.[
      preferences.app.locale === 'zh-CN' ? 'zh-CN' : 'en-US'
    ] ?? commonCapabilitiesGuide.files,
);
async function openGuide() {
  guideReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  guideOpen.value = true;
  await nextTick();
  guideDrawer.value?.focus();
}
function closeGuide() {
  guideOpen.value = false;
  const target = guideReturnFocus;
  guideReturnFocus = null;
  void nextTick(() => target?.focus());
}
const message = ref('');
const errorSummary = ref<HTMLElement | null>(null);
const cleanupAge = ref(180 * 24 * 60 * 60);
const cleanupLoading = ref(false);
const cleanupReport = ref<{
  bytes: number;
  cutoff: string;
  matchingCount: number;
}>();

const categoryRows = computed(() => {
  const byParent = new Map<string, FileCategory[]>();
  for (const category of categories.value) {
    const key = category.parentId || '';
    const list = byParent.get(key) ?? [];
    list.push(category);
    byParent.set(key, list);
  }
  for (const list of byParent.values())
    list.sort((a, b) => a.name.localeCompare(b.name));
  const rows: Array<FileCategory & { depth: number }> = [];
  const visit = (parentId: string, depth: number, path: Set<string>) => {
    for (const category of byParent.get(parentId) ?? []) {
      if (path.has(category.id)) continue;
      rows.push({ ...category, depth });
      const nextPath = new Set(path);
      nextPath.add(category.id);
      visit(category.id, depth + 1, nextPath);
    }
  };
  visit('', 0, new Set());
  // Keep an orphan visible rather than silently dropping a malformed branch.
  for (const category of categories.value) {
    if (!rows.some((row) => row.id === category.id))
      rows.push({ ...category, depth: 0 });
  }
  return rows;
});

const selectedCategoryName = computed(() => {
  if (!selectedCategoryId.value) return String($t('page.files.allCategories'));
  return (
    categories.value.find(
      (category) => category.id === selectedCategoryId.value,
    )?.name ?? String($t('page.files.allCategories'))
  );
});

const hasFiles = computed(() => page.value.items.length > 0);
const accept =
  '.jpg,.jpeg,.png,.gif,.webp,.svg,.mp3,.wav,.ogg,.mp4,.avi,.mov,.xlsx,.xls,.csv,.zip,.rar,.7z,.md,.txt,.pdf';

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function loadCategories() {
  if (!canManage.value) return;
  categoryError.value = '';
  try {
    categories.value = await listFileCategoriesApi();
  } catch {
    categoryError.value = String($t('page.files.categoryLoadError'));
  }
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    page.value = await listFilesApi({
      categoryId: selectedCategoryId.value || undefined,
      limit: 50,
      offset: 0,
    });
  } catch {
    error.value = String($t('page.files.loadError'));
    await focusError();
  } finally {
    loading.value = false;
  }
}

function selectCategory(id: string) {
  selectedCategoryId.value = id;
  void load();
}

async function createCategory() {
  if (!canManage.value || !categoryName.value.trim()) return;
  categoryBusy.value = true;
  categoryError.value = '';
  try {
    const input: FileCategoryInput = {
      name: categoryName.value.trim(),
      parentId: categoryParentId.value || undefined,
    };
    const created = await createFileCategoryApi(input);
    categoryName.value = '';
    categoryParentId.value = '';
    await loadCategories();
    selectedCategoryId.value = created.id;
    await load();
    message.value = String($t('page.files.categoryCreated'));
  } catch {
    categoryError.value = String($t('page.files.categorySaveError'));
  } finally {
    categoryBusy.value = false;
  }
}

async function editCategory(category: FileCategory) {
  if (!canManage.value) return;
  const name = window
    .prompt(String($t('page.files.renameCategory')), category.name)
    ?.trim();
  if (!name || name === category.name) return;
  categoryBusy.value = true;
  categoryError.value = '';
  try {
    await updateFileCategoryApi(category.id, {
      name,
      parentId: category.parentId || undefined,
    });
    await loadCategories();
    message.value = String($t('page.files.categoryUpdated'));
  } catch {
    categoryError.value = String($t('page.files.categorySaveError'));
  } finally {
    categoryBusy.value = false;
  }
}

async function removeCategory(category: FileCategory) {
  if (
    !canManage.value ||
    !window.confirm(String($t('page.files.confirmCategoryDelete')))
  )
    return;
  categoryBusy.value = true;
  categoryError.value = '';
  try {
    await deleteFileCategoryApi(category.id);
    if (selectedCategoryId.value === category.id) selectedCategoryId.value = '';
    await loadCategories();
    await load();
    message.value = String($t('page.files.categoryDeleted'));
  } catch {
    categoryError.value = String($t('page.files.categoryDeleteError'));
  } finally {
    categoryBusy.value = false;
  }
}

function onFileChange(event: Event) {
  if (!canManage.value) return;
  const input = event.target as HTMLInputElement;
  selectedFile.value = input.files?.[0] ?? null;
}

function previewURL(item: { id: string }) {
  return previewURLs.value[item.id] ?? '';
}

/**
 * Media reads go through the authenticated request client instead of placing
 * a protected API path in an <img> tag (native image requests do not carry the
 * application's bearer-token interceptor). Object URLs are revoked on unmount
 * so opening the picker repeatedly does not leak blob handles.
 */
async function ensurePreviewURL(item: { id: string }) {
  if (!item.id || previewURLs.value[item.id] || previewLoading.has(item.id)) return;
  previewLoading.add(item.id);
  try {
    const blob = await openMediaResourceApi(item.id);
    if (blob instanceof Blob) {
      previewURLs.value = {
        ...previewURLs.value,
        [item.id]: URL.createObjectURL(blob),
      };
    }
  } catch {
    // Keep the metadata tile available when a protected preview is unavailable.
  } finally {
    previewLoading.delete(item.id);
  }
}

function revokePreviewURLs() {
  for (const url of Object.values(previewURLs.value)) URL.revokeObjectURL(url);
  previewURLs.value = {};
}

function isImage(item: { mime?: string }) {
  return item.mime?.toLowerCase().startsWith('image/') ?? false;
}

function selectAsset(item: FileObject) {
  if (isImage(item)) selectedAsset.value = item;
}

async function openLogoPicker() {
	if (!canManage.value) return;
	logoReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
	logoPickerOpen.value = true;
  await nextTick();
  logoDialog.value?.focus();
  try {
    // Use the shared catalog rather than the legacy offset endpoint so the
    // picker sees system, tenant and organization resources in one stable
    // scope-aware list. Non-images remain in the result and are disabled in
    // the picker instead of being silently hidden.
    const result = await listMediaResourcesApi({ limit: 200 });
    logoAssets.value = result.items;
    void Promise.all(
      result.items.filter(isImage).slice(0, 48).map((item) => ensurePreviewURL(item)),
    );
    let storedId = window.localStorage.getItem(logoStorageKey);
    try {
      const setting = await getBrandingSettingsApi();
      storedId = setting.value.logoResourceId || storedId;
    } catch {
      // The local fixture may not mount settings; localStorage still keeps the
      // picker usable while the server-side setting is unavailable.
    }
    if (storedId) {
      logoAsset.value = result.items.find((item) => item.id === storedId) ?? logoAsset.value;
    }
  } catch {
    error.value = String($t('page.files.loadError'));
  }
}
function closeLogoPicker() {
  logoPickerOpen.value = false;
  const target = logoReturnFocus;
  logoReturnFocus = null;
  void nextTick(() => target?.focus());
}
function isBrandingLogoUsage(usage: MediaUsage) {
  return usage.module === 'branding' && usage.entityType === 'setting' && usage.entityId === 'admin' && usage.field === 'logo';
}

async function listBrandingLogoUsages(resourceId: string) {
  const usages = await listMediaUsagesApi(resourceId);
  return usages.filter(isBrandingLogoUsage);
}

async function removeLogoUsage(resourceId: string, detached: MediaUsage[]) {
  const usages = await listBrandingLogoUsages(resourceId);
  for (const usage of usages) {
    await detachMediaUsageApi(usage.id, `branding:logo:detach:${usage.id}`);
    detached.push(usage);
  }
}

async function restoreLogoUsages(usages: MediaUsage[]) {
  for (const usage of usages) {
    await attachMediaUsageApi(usage.resourceId, {
      module: usage.module,
      entityType: usage.entityType,
      entityId: usage.entityId,
      field: usage.field,
    }, `branding:logo:rollback:${usage.id}`);
  }
}

async function chooseLogo(item: MediaResource): Promise<boolean> {
  if (!canManage.value || !isImage(item)) return false;
  logoBusy.value = true;
  error.value = '';
  message.value = '';
  const previousAsset = logoAsset.value;
  let previousSettings: Awaited<ReturnType<typeof getBrandingSettingsApi>> | null = null;
  let updatedVersion: number | undefined;
  let attachedUsage: MediaUsage | null = null;
  let attachedUsageWasNew = false;
  const detachedPreviousUsages: MediaUsage[] = [];
  try {
    // Read the server-side final state first. A localStorage-only fallback is
    // intentionally not used for a save, otherwise a failed mutation could
    // make the UI claim a Logo that the server never committed.
    previousSettings = await getBrandingSettingsApi();
    const previousID = previousSettings.value.logoResourceId || previousAsset?.id;
    const existingUsages = await listBrandingLogoUsages(item.id);
    attachedUsageWasNew = existingUsages.length === 0;
    const updated = await updateBrandingSettingsApi({
      ...previousSettings.value,
      logoResourceId: item.id,
    }, previousSettings.version ?? 0);
    updatedVersion = updated.version ?? ((previousSettings.version ?? 0) + 1);
    attachedUsage = await attachMediaUsageApi(item.id, {
      module: 'branding',
      entityType: 'setting',
      entityId: 'admin',
      field: 'logo',
    }, `branding:logo:${item.id}`);
    if (previousID && previousID !== item.id) await removeLogoUsage(previousID, detachedPreviousUsages);
    logoAsset.value = item;
    void ensurePreviewURL(item);
    window.localStorage.setItem(logoStorageKey, item.id);
    closeLogoPicker();
    return true;
  } catch {
    let rolledBack = updatedVersion === undefined;
    if (previousSettings && updatedVersion !== undefined) {
      try {
        await updateBrandingSettingsApi(previousSettings.value, updatedVersion);
        rolledBack = true;
      } catch {
        rolledBack = false;
      }
    }
    // Restore any old references detached before a later cleanup failure. A
    // failed save never changes the visible Logo; reconciliation can repair a
    // rare provider outage without exposing a broken reference.
    if (detachedPreviousUsages.length > 0) {
      try { await restoreLogoUsages(detachedPreviousUsages); } catch { /* retain the primary error */ }
    }
    if (rolledBack && attachedUsageWasNew && attachedUsage) {
      try { await detachMediaUsageApi(attachedUsage.id, `branding:logo:rollback:${attachedUsage.id}`); } catch { /* retain the primary error */ }
    }
    error.value = String($t('page.files.logoSaveError'));
    await focusError();
    return false;
  } finally {
    logoBusy.value = false;
  }
}
function triggerLogoUpload() { logoUploadInput.value?.click(); }
async function uploadLogo(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = '';
  if (!file || !file.type.startsWith('image/')) return;
  uploading.value = true; error.value = '';
  try {
    const uploaded = await uploadMediaResourceApi(file, 'public-read');
    if (uploaded && isImage(uploaded) && !(await chooseLogo(uploaded))) {
      // This endpoint always creates a fresh resource. If the subsequent
      // branding transaction is compensated, remove that newly-created,
      // now-unreferenced object as the final best-effort compensation step.
      try {
        await deleteMediaResourceApi(
          uploaded.id,
          `branding:logo:upload-cleanup:${uploaded.id}`,
        );
      } catch {
        // The primary save error is already visible. A cleanup worker may
        // reconcile a provider outage without replacing that actionable error.
      }
      return;
    }
    message.value = String($t('page.files.logoUploaded'));
    await load();
  } catch { error.value = String($t('page.files.uploadError')); await focusError(); }
  finally { uploading.value = false; }
}

async function upload() {
  if (!canManage.value) return;
  if (!selectedFile.value) {
    error.value = String($t('page.files.fileRequired'));
    await focusError();
    return;
  }
  uploading.value = true;
  error.value = '';
  message.value = '';
  try {
    const uploaded = await uploadFileApi(
      selectedFile.value,
      acl.value,
      selectedCategoryId.value || undefined,
    );
    if (uploaded && isImage(uploaded)) selectedAsset.value = uploaded;
    selectedFile.value = null;
    message.value = String($t('page.files.uploaded'));
    await load();
  } catch {
    error.value = String($t('page.files.uploadError'));
    await focusError();
  } finally {
    uploading.value = false;
  }
}

function formatSize(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024)
    return `${(value / 1024 / 1024).toFixed(1)} MB`;
  return `${(value / 1024 / 1024 / 1024).toFixed(1)} GB`;
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

function categoryLabel(id?: string) {
  if (!id) return String($t('page.files.uncategorized'));
  return (
    categories.value.find((category) => category.id === id)?.name ??
    String($t('page.files.uncategorized'))
  );
}

async function download(item: FileObject, preview = false) {
  actionId.value = item.id;
  error.value = '';
  try {
    const blob = await downloadFileApi(item.id, preview);
    const url = URL.createObjectURL(blob);
    if (preview) window.open(url, '_blank', 'noopener,noreferrer');
    else {
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = item.name;
      anchor.click();
    }
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch {
    error.value = String($t('page.files.downloadError'));
    await focusError();
  } finally {
    actionId.value = '';
  }
}

async function deleteItem(item: FileObject) {
  if (!canManage.value) return;
  if (!window.confirm(String($t('page.files.confirmDelete')))) return;
  actionId.value = item.id;
  error.value = '';
  try {
    await deleteFileApi(item.id);
    message.value = String($t('page.files.deleted'));
    await load();
  } catch {
    error.value = String($t('page.files.deleteError'));
    await focusError();
  } finally {
    actionId.value = '';
  }
}

async function createSignedURL(item: FileObject) {
  if (!canManage.value) return;
  actionId.value = item.id;
  error.value = '';
  try {
    const result = await signedFileUrlApi(item.id);
    await navigator.clipboard?.writeText(result.url);
    message.value = String($t('page.files.signedURLCopied'));
  } catch {
    error.value = String($t('page.files.signedURLError'));
    await focusError();
  } finally {
    actionId.value = '';
  }
}

async function cleanupDryRun() {
  if (!canManage.value) return;
  cleanupLoading.value = true;
  error.value = '';
  try {
    cleanupReport.value = await cleanupDryRunApi(cleanupAge.value);
    message.value = String($t('page.files.cleanupDone'));
  } catch {
    error.value = String($t('page.files.cleanupError'));
    await focusError();
  } finally {
    cleanupLoading.value = false;
  }
}

onBeforeUnmount(revokePreviewURLs);

onMounted(async () => {
  await loadCategories();
  await load();
  try {
    const setting = await getBrandingSettingsApi();
    if (setting.value.logoResourceId) {
      logoAsset.value = await getMediaResourceApi(setting.value.logoResourceId);
      void ensurePreviewURL(logoAsset.value);
    }
  } catch {
    // The picker can still recover the local fixture value when opened.
  }
});
</script>

<template>
  <ManagementPage
    class="media-library-page"
    :aria-busy="loading || uploading"
    aria-labelledby="media-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.files.eyebrow') }}</p>
        <h1 id="media-title">{{ $t('page.files.title') }}</h1>
        <p class="description">{{ $t('page.files.description') }}</p>
      </div>
      <div class="provider-meta">
        <button class="secondary" type="button" @click="openGuide">{{ $t('page.files.guideButton') }}</button>
        <span class="provider-chip">{{ $t('page.files.localProvider') }}</span>
        <a href="/system/settings">{{ $t('page.files.providerSettings') }}</a>
      </div>
    </header>

    <aside v-if="guideOpen" ref="guideDrawer" class="guide-drawer" role="dialog" aria-modal="true" aria-labelledby="files-guide-title" @click.self="closeGuide" @keydown.esc="closeGuide" tabindex="-1">
      <div class="guide-panel"><div class="section-heading"><div><p class="eyebrow">{{ $t('page.files.guideButton') }}</p><h2 id="files-guide-title">{{ guide.title }}</h2></div><button class="secondary" type="button" @click="closeGuide">{{ $t('page.files.guideClose') }}</button></div><p class="description">{{ $t('page.files.guideAudience') }}</p><h3>{{ $t('page.files.guideNormal') }}</h3><ol><li v-for="step in guide.steps" :key="step">{{ step }}</li></ol><h3>{{ $t('page.files.guideDeveloper') }}</h3><ul><li v-for="step in guide.developer" :key="step">{{ step }}</li></ul></div>
    </aside>

    <p
      v-if="error"
      ref="errorSummary"
      class="feedback feedback-error"
      role="alert"
      tabindex="-1"
    >
      {{ error }}
    </p>
    <p v-if="categoryError" class="feedback feedback-error" role="alert">
      {{ categoryError }}
    </p>
    <p v-if="message" class="feedback feedback-success" aria-live="polite">
      {{ message }}
    </p>

    <div class="media-layout">
      <aside class="category-panel" aria-labelledby="category-title">
        <div class="section-heading">
          <h2 id="category-title">{{ $t('page.files.categories') }}</h2>
          <span>{{ categories.length }}</span>
        </div>
        <button
          type="button"
          class="all-category"
          :class="{ active: !selectedCategoryId }"
          @click="selectCategory('')"
        >
          {{ $t('page.files.allCategories') }}
        </button>
        <nav class="category-tree" :aria-label="$t('page.files.categoryTree')">
          <div
            v-for="category in categoryRows"
            :key="category.id"
            class="category-row"
            :style="{ paddingInlineStart: `${0.5 + category.depth * 1.1}rem` }"
          >
            <button
              type="button"
              class="category-select"
              :class="{ active: selectedCategoryId === category.id }"
              @click="selectCategory(category.id)"
            >
              <span aria-hidden="true">◈</span>{{ category.name }}
            </button>
            <span v-if="canManage" class="category-actions"><button
                type="button"
                :disabled="categoryBusy"
                :aria-label="$t('page.files.editCategory')"
                @click="editCategory(category)"
              >
                ✎</button><button
                type="button"
                :disabled="categoryBusy"
                :aria-label="$t('page.files.deleteCategory')"
                @click="removeCategory(category)"
              >
                ×
              </button></span>
          </div>
          <p v-if="!categoryRows.length" class="empty-state">
            {{ $t('page.files.noCategories') }}
          </p>
        </nav>
        <form
          v-if="canManage"
          class="category-form"
          @submit.prevent="createCategory"
        >
          <h3>{{ $t('page.files.newCategory') }}</h3>
          <input
            v-model="categoryName"
            :placeholder="$t('page.files.categoryName')"
            maxlength="80"
          />
          <select v-model="categoryParentId">
            <option value="">{{ $t('page.files.rootCategory') }}</option>
            <option
              v-for="category in categoryRows"
              :key="category.id"
              :value="category.id"
            >
              {{ '· '.repeat(category.depth) }}{{ category.name }}
            </option>
          </select>
          <button
            class="primary"
            type="submit"
            :disabled="categoryBusy || !categoryName.trim()"
          >
            {{
              categoryBusy
                ? $t('page.files.savingCategory')
                : $t('page.files.createCategory')
            }}
          </button>
        </form>
      </aside>

      <section class="files-content">
        <section class="logo-card" aria-labelledby="logo-title">
          <div>
            <p class="eyebrow">{{ $t('page.files.logoEyebrow') }}</p>
            <h2 id="logo-title">{{ $t('page.files.logoTitle') }}</h2>
            <p class="description">{{ logoAsset?.name || $t('page.files.logoEmpty') }}</p>
          </div>
          <div class="logo-actions">
            <button type="button" class="secondary" :disabled="logoBusy" @click="openLogoPicker">{{ $t('page.files.logoChoose') }}</button>
            <button type="button" class="primary" :disabled="uploading || logoBusy || !canManage" @click="triggerLogoUpload">{{ $t('page.files.logoUpload') }}</button>
            <input ref="logoUploadInput" class="sr-only" type="file" accept="image/*" @change="uploadLogo" />
          </div>
          <img
            v-if="logoAsset && previewURL(logoAsset)"
            class="logo-preview"
            :src="previewURL(logoAsset)"
            :alt="logoAsset.name"
          />
        </section>

        <aside v-if="logoPickerOpen" ref="logoDialog" class="logo-drawer" role="dialog" aria-modal="true" aria-labelledby="logo-picker-title" tabindex="-1" @keydown.esc="closeLogoPicker">
          <div class="logo-panel">
            <div class="section-heading"><h2 id="logo-picker-title">{{ $t('page.files.logoChoose') }}</h2><button type="button" class="secondary" @click="closeLogoPicker">{{ $t('page.files.guideClose') }}</button></div>
            <p class="description">{{ $t('page.files.logoPickerHelp') }}</p>
            <div class="logo-grid">
              <button v-for="item in logoAssets" :key="item.id" type="button" class="logo-item" :disabled="!isImage(item) || logoBusy" :aria-pressed="logoAsset?.id === item.id" @mouseenter="ensurePreviewURL(item)" @focus="ensurePreviewURL(item)" @click="chooseLogo(item)">
                <img v-if="isImage(item) && previewURL(item)" :src="previewURL(item)" :alt="item.name" />
                <span>{{ item.name }}</span><small>{{ categoryLabel(item.categoryId) }}</small>
              </button>
            </div>
          </div>
        </aside>

        <section
          v-if="canManage"
          class="upload-card"
          aria-labelledby="upload-title"
        >
          <div class="section-heading">
            <div>
              <h2 id="upload-title">{{ $t('page.files.uploadTitle') }}</h2>
              <p>{{ $t('page.files.supportedTypes') }}</p>
            </div>
            <span class="selected-category">{{ selectedCategoryName }}</span>
          </div>
          <div class="upload-controls">
            <label class="file-picker"><span>{{ $t('page.files.chooseFile') }}</span><input
                type="file"
                :accept="accept"
                :disabled="uploading"
                @change="onFileChange"
            /></label>
            <label class="field"><span>{{ $t('page.files.acl') }}</span><select v-model="acl" :disabled="uploading">
                <option value="private">{{ $t('page.files.private') }}</option>
                <option value="public-read">
                  {{ $t('page.files.publicRead') }}
                </option>
              </select></label>
            <button
              class="primary"
              type="button"
              :disabled="uploading || !selectedFile"
              @click="upload"
            >
              {{
                uploading ? $t('page.files.uploading') : $t('page.files.upload')
              }}
            </button>
          </div>
          <p v-if="selectedFile" class="selected-file" aria-live="polite">
            {{ selectedFile.name }} · {{ formatSize(selectedFile.size) }} ·
            {{ selectedFile.type || 'application/octet-stream' }}
          </p>
          <p class="help">{{ $t('page.files.uploadHelp') }}</p>
        </section>

        <section class="table-card" aria-labelledby="files-table-title">
          <div class="table-heading">
            <div>
              <p class="eyebrow">{{ $t('page.files.tableEyebrow') }}</p>
              <h2 id="files-table-title">{{ selectedCategoryName }}</h2>
            </div>
            <button type="button" :disabled="loading" @click="load">
              {{ $t('page.files.refresh') }}
            </button>
          </div>
          <p v-if="selectedAsset" class="selected-file" role="status">{{ $t('page.files.selectedAsset', { name: selectedAsset.name }) }}</p>
          <p v-if="!loading && !hasFiles" class="empty-state">
            {{ $t('page.files.empty') }}
          </p>
          <div v-else class="table-scroll">
            <table>
              <caption class="sr-only">
                {{
                  $t('page.files.tableLabel')
                }}
              </caption>
              <thead>
                <tr>
                  <th>{{ $t('page.files.name') }}</th>
                  <th>{{ $t('page.files.category') }}</th>
                  <th>{{ $t('page.files.mime') }}</th>
                  <th>{{ $t('page.files.size') }}</th>
                  <th>{{ $t('page.files.acl') }}</th>
                  <th>{{ $t('page.files.createdAt') }}</th>
                  <th>{{ $t('page.files.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in page.items" :key="item.id">
                  <td class="name-cell">{{ item.name }}</td>
                  <td>{{ categoryLabel(item.categoryId) }}</td>
                  <td>{{ item.mime }}</td>
                  <td>{{ formatSize(item.size) }}</td>
                  <td>{{ item.acl }}</td>
                  <td>{{ formatDate(item.createdAt) }}</td>
                  <td class="actions">
                    <button
                      type="button"
                      class="asset-select"
                      :disabled="!isImage(item)"
                      :aria-pressed="selectedAsset?.id === item.id"
                      @click="selectAsset(item)"
                    >
                      {{
                        selectedAsset?.id === item.id
                          ? $t('page.files.selected')
                          : $t('page.files.selectImage')
                      }}
                    </button>
                    <button
                      type="button"
                      :disabled="actionId === item.id"
                      @click="download(item, true)"
                    >
                      {{ $t('page.files.preview') }}
</button><button
                      type="button"
                      :disabled="actionId === item.id"
                      @click="download(item)"
                    >
                      {{ $t('page.files.download') }}
</button><button
                      v-if="canManage"
                      type="button"
                      :disabled="actionId === item.id"
                      @click="createSignedURL(item)"
                    >
                      {{ $t('page.files.signedURL') }}
</button><button
                      v-if="canManage"
                      class="danger"
                      type="button"
                      :disabled="actionId === item.id"
                      @click="deleteItem(item)"
                    >
                      {{ $t('page.files.delete') }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section
          v-if="canManage"
          class="cleanup-card"
          aria-labelledby="cleanup-title"
        >
          <h2 id="cleanup-title">{{ $t('page.files.cleanupTitle') }}</h2>
          <p>{{ $t('page.files.cleanupDescription') }}</p>
          <div class="cleanup-controls">
            <label class="field"><span>{{ $t('page.files.cleanupAge') }}</span><input
                v-model.number="cleanupAge"
                min="1"
                type="number"
/></label><button
              type="button"
              :disabled="cleanupLoading"
              @click="cleanupDryRun"
            >
              {{
                cleanupLoading
                  ? $t('page.files.cleanupRunning')
                  : $t('page.files.cleanupDryRun')
              }}
            </button>
          </div>
          <p v-if="cleanupReport" class="cleanup-result" aria-live="polite">
            {{
              $t('page.files.cleanupResult', {
                count: cleanupReport.matchingCount,
                bytes: formatSize(cleanupReport.bytes),
              })
            }}
          </p>
        </section>
      </section>
    </div>
  </ManagementPage>
</template>

<style scoped>
.media-library-page {
  --line: hsl(var(--border));
  --muted: hsl(var(--muted-foreground));

  color: hsl(var(--foreground));
}

.page-heading,
.provider-meta,
.section-heading,
.table-heading,
.upload-controls,
.cleanup-controls {
  display: flex;
  gap: 1rem;
  align-items: center;
  justify-content: space-between;
}

.page-heading {
  flex-wrap: wrap;
  align-items: flex-start;
}

.provider-meta {
  flex-wrap: wrap;
}

.provider-meta a {
  font-size: 0.8rem;
  color: hsl(var(--primary));
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
h3,
p {
  margin: 0;
}

h1 {
  font-size: clamp(1.7rem, 3vw, 2.4rem);
}

h2 {
  font-size: 1.1rem;
}

h3 {
  font-size: 0.9rem;
}

.description,
.help,
.section-heading span,
.upload-card p,
.cleanup-card p {
  color: var(--muted);
}

.description {
  max-inline-size: 72ch;
  margin-block-start: 0.5rem;
}

.provider-chip,
.selected-category {
  display: inline-flex;
  padding: 0.4rem 0.7rem;
  font-size: 0.75rem;
  font-weight: 750;
  color: #075985;
  background: #e0f2fe;
  border-radius: 999px;
}

.selected-category {
  color: #3730a3;
  background: #eef2ff;
}

.feedback {
  padding: 0.75rem 1rem;
  margin-block-start: 1rem;
  border-radius: 0.7rem;
}

.feedback-error {
  color: hsl(var(--destructive));
  background: color-mix(in srgb, hsl(var(--destructive)) 10%, hsl(var(--card)));
}

.feedback-success {
  color: #166534;
  background: #dcfce7;
}

.media-layout {
  display: grid;
  grid-template-columns: minmax(14rem, 18rem) minmax(0, 1fr);
  gap: 1rem;
  margin-block-start: 1.25rem;
}

.category-panel,
.upload-card,
.table-card,
.cleanup-card {
  background: hsl(var(--card));
  border: 1px solid var(--line);
  border-radius: 1rem;
}

.category-panel {
  align-self: start;
  padding: 1rem;
}

.files-content {
  display: grid;
  gap: 1rem;
  min-inline-size: 0;
}

.category-panel .section-heading {
  margin-block-end: 0.8rem;
}

.all-category,
.category-select,
.category-actions button {
  color: hsl(var(--foreground));
  cursor: pointer;
  background: transparent;
  border: 0;
}

.all-category,
.category-select {
  inline-size: 100%;
  padding: 0.55rem 0.5rem;
  text-align: start;
  border-radius: 0.5rem;
}

.all-category.active,
.category-select.active {
  font-weight: 750;
  color: hsl(var(--primary));
  background: color-mix(in srgb, hsl(var(--primary)) 10%, transparent);
}

.category-tree {
  display: grid;
  gap: 0.15rem;
  max-block-size: 28rem;
  margin-block: 0.35rem;
  overflow: auto;
}

.category-row {
  display: flex;
  gap: 0.25rem;
  align-items: center;
  min-inline-size: 0;
}

.category-select {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-select span {
  margin-inline-end: 0.35rem;
  color: hsl(var(--primary));
}

.category-actions {
  display: flex;
  gap: 0.1rem;
}

.category-actions button {
  inline-size: 1.6rem;
  block-size: 1.6rem;
  color: var(--muted);
  border-radius: 0.35rem;
}

.category-actions button:hover,
.category-actions button:focus-visible {
  color: hsl(var(--primary));
  background: color-mix(in srgb, hsl(var(--primary)) 10%, transparent);
}

.category-form {
  display: grid;
  gap: 0.5rem;
  padding-block-start: 0.8rem;
  margin-block-start: 0.8rem;
  border-block-start: 1px solid var(--line);
}

input,
select,
button {
  min-block-size: 2.4rem;
  padding: 0.45rem 0.7rem;
  color: hsl(var(--foreground));
  background: hsl(var(--background));
  border: 1px solid var(--line);
  border-radius: 0.55rem;
}

button {
  cursor: pointer;
}

button.primary {
  color: hsl(var(--primary-foreground));
  background: hsl(var(--primary));
  border-color: hsl(var(--primary));
}

button.danger {
  color: #b91c1c;
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

.upload-card,
.table-card,
.cleanup-card {
  padding: 1rem;
}

.upload-card {
  margin: 0;
}

.upload-controls {
  flex-wrap: wrap;
  align-items: end;
  justify-content: flex-start;
  margin-block-start: 1rem;
}

.file-picker,
.field {
  display: grid;
  gap: 0.3rem;
  font-size: 0.78rem;
  color: var(--muted);
}

.file-picker input {
  max-inline-size: 22rem;
}

.field select,
.field input {
  min-inline-size: 10rem;
}

.selected-file {
  margin-block-start: 0.65rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8rem;
  overflow-wrap: anywhere;
}

.help {
  margin-block-start: 0.65rem;
  font-size: 0.78rem;
}

.table-heading {
  align-items: flex-start;
}

.table-heading button {
  flex: 0 0 auto;
}

.table-scroll {
  margin-block-start: 1rem;
  overflow: auto;
  border-block: 1px solid var(--line);
}

table {
  inline-size: 100%;
  min-inline-size: 62rem;
  border-collapse: collapse;
}

th,
td {
  padding: 0.7rem 0.55rem;
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

.name-cell {
  max-inline-size: 18rem;
  font-weight: 650;
  overflow-wrap: anywhere;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem;
}

.actions button {
  min-block-size: 2rem;
  padding: 0.25rem 0.5rem;
  font-size: 0.74rem;
}

.empty-state {
  padding: 2rem;
  color: var(--muted);
  text-align: center;
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

.cleanup-card {
  margin-block-start: 0;
}

.cleanup-controls {
  align-items: end;
  justify-content: flex-start;
  margin-block-start: 0.8rem;
}

.cleanup-result {
  margin-block-start: 0.7rem;
  font-weight: 650;
  color: #166534;
}

@media (width <= 860px) {
  .media-layout {
    grid-template-columns: 1fr;
  }

  .category-panel {
    position: static;
  }

  .category-tree {
    max-block-size: 18rem;
  }
}

@media (width <= 560px) {
  .page-heading,
  .provider-meta,
  .upload-controls,
  .cleanup-controls {
    flex-direction: column;
    align-items: stretch;
  }

  .upload-controls > * {
    inline-size: 100%;
  }

  .file-picker input,
  .field select,
  .field input {
    inline-size: 100%;
    max-inline-size: none;
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


.logo-card { display:flex; align-items:center; justify-content:space-between; gap:1rem; padding:1rem; }
.logo-actions { display:flex; flex-wrap:wrap; gap:.5rem; }
.logo-preview { inline-size:3.5rem; block-size:3.5rem; object-fit:contain; border:1px solid var(--line); border-radius:.5rem; }
.logo-drawer { position:fixed; inset:0; z-index:1001; display:flex; justify-content:flex-end; background:rgb(15 23 42 / 38%); }
.logo-panel { width:min(42rem,100%); height:100%; overflow:auto; padding:24px; background:white; box-shadow:-12px 0 32px rgb(15 23 42 / 18%); }
.logo-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(10rem,1fr)); gap:.75rem; margin-top:1rem; }
.logo-item { display:grid; gap:.35rem; padding:.65rem; text-align:start; border:1px solid var(--line); border-radius:.5rem; background:transparent; }
.logo-item img { width:100%; height:6rem; object-fit:contain; background:#f8fafc; }
.logo-item small { color:var(--muted); }
.logo-item:disabled { opacity:.45; cursor:not-allowed; }
@media (width <= 560px) { .logo-card { align-items:stretch; flex-direction:column; } }

.guide-drawer { position: fixed; inset: 0; z-index: 1000; display: flex; justify-content: flex-end; background: rgb(15 23 42 / 38%); }
.guide-panel { width: min(34rem, 100%); height: 100%; overflow: auto; padding: 24px; background: white; box-shadow: -12px 0 32px rgb(15 23 42 / 18%); }
.guide-panel h3 { margin-top: 24px; }
.guide-panel li { margin: 8px 0; line-height: 1.5; }
.heading-actions { display:flex; gap: 8px; flex-wrap: wrap; }
@media (prefers-reduced-motion: reduce) { .guide-drawer * { transition: none !important; } }
</style>
