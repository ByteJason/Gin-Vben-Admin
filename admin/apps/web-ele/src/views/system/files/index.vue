<script setup lang="ts">
import type {
  FileACL,
  FileCategory,
  FileCategoryInput,
  FileObject,
  FilePage,
} from '#/api/core/files';

import { computed, nextTick, onMounted, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';

import {
  cleanupDryRunApi,
  createFileCategoryApi,
  deleteFileApi,
  deleteFileCategoryApi,
  downloadFileApi,
  listFileCategoriesApi,
  listFilesApi,
  signedFileUrlApi,
  uploadFileApi,
  updateFileCategoryApi,
} from '#/api/core/files';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() =>
  hasAccessByCodes(['system:files:manage', 'media:library:manage']),
);
const categories = ref<FileCategory[]>([]);
const selectedCategoryId = ref('');
const categoryName = ref('');
const categoryParentId = ref('');
const categoryBusy = ref(false);
const categoryError = ref('');
const page = ref<FilePage>({ items: [], limit: 50, offset: 0, total: 0 });
const selectedFile = ref<File | null>(null);
const acl = ref<FileACL>('private');
const loading = ref(false);
const uploading = ref(false);
const actionId = ref('');
const error = ref('');
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
    await uploadFileApi(
      selectedFile.value,
      acl.value,
      selectedCategoryId.value || undefined,
    );
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
  if (
    !canManage.value ||
    !window.confirm(String($t('page.files.confirmDelete')))
  )
    return;
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

onMounted(async () => {
  await loadCategories();
  await load();
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
        <span class="provider-chip">{{ $t('page.files.localProvider') }}</span>
        <a href="/system/settings">{{ $t('page.files.providerSettings') }}</a>
      </div>
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
            <span v-if="canManage" class="category-actions"
              ><button
                type="button"
                :disabled="categoryBusy"
                :aria-label="$t('page.files.editCategory')"
                @click="editCategory(category)"
              >
                ✎</button
              ><button
                type="button"
                :disabled="categoryBusy"
                :aria-label="$t('page.files.deleteCategory')"
                @click="removeCategory(category)"
              >
                ×
              </button></span
            >
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

      <main class="files-content">
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
            <label class="file-picker"
              ><span>{{ $t('page.files.chooseFile') }}</span
              ><input
                type="file"
                :accept="accept"
                :disabled="uploading"
                @change="onFileChange"
            /></label>
            <label class="field"
              ><span>{{ $t('page.files.acl') }}</span
              ><select v-model="acl" :disabled="uploading">
                <option value="private">{{ $t('page.files.private') }}</option>
                <option value="public-read">
                  {{ $t('page.files.publicRead') }}
                </option>
              </select></label
            >
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
                      :disabled="actionId === item.id"
                      @click="download(item, true)"
                    >
                      {{ $t('page.files.preview') }}</button
                    ><button
                      type="button"
                      :disabled="actionId === item.id"
                      @click="download(item)"
                    >
                      {{ $t('page.files.download') }}</button
                    ><button
                      v-if="canManage"
                      type="button"
                      :disabled="actionId === item.id"
                      @click="createSignedURL(item)"
                    >
                      {{ $t('page.files.signedURL') }}</button
                    ><button
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
            <label class="field"
              ><span>{{ $t('page.files.cleanupAge') }}</span
              ><input
                v-model.number="cleanupAge"
                min="1"
                type="number" /></label
            ><button
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
      </main>
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
</style>
