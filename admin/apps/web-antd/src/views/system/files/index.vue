<script setup lang="ts">
import type { FileACL, FileObject, FilePage } from '#/api/core/files';

import { computed, nextTick, onMounted, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage } from '@vben/common-ui';

import {
  cleanupDryRunApi,
  deleteFileApi,
  downloadFileApi,
  listFilesApi,
  signedFileUrlApi,
  uploadFileApi,
} from '#/api/core/files';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['system:files:manage']));

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

const hasFiles = computed(() => page.value.items.length > 0);

async function focusError() {
  await nextTick();
  errorSummary.value?.focus();
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    page.value = await listFilesApi({ limit: 50, offset: 0 });
  } catch {
    error.value = String($t('page.files.loadError'));
    await focusError();
  } finally {
    loading.value = false;
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
    await uploadFileApi(selectedFile.value, acl.value);
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
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

async function download(item: FileObject, preview = false) {
  actionId.value = item.id;
  error.value = '';
  try {
    const blob = await downloadFileApi(item.id, preview);
    const url = URL.createObjectURL(blob);
    if (preview) {
      window.open(url, '_blank', 'noopener,noreferrer');
    } else {
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

onMounted(load);
</script>

<template>
  <ManagementPage
    class="files-page"
    :aria-busy="loading || uploading"
    aria-labelledby="files-title"
  >
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.files.eyebrow') }}</p>
        <h1 id="files-title">{{ $t('page.files.title') }}</h1>
        <p class="description">{{ $t('page.files.description') }}</p>
      </div>
      <span class="provider-chip">{{ $t('page.files.localProvider') }}</span>
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
      {{ loading ? $t('page.files.loading') : '' }}
    </p>

    <section
      v-if="canManage"
      class="upload-card"
      aria-labelledby="files-upload-title"
    >
      <h2 id="files-upload-title">{{ $t('page.files.uploadTitle') }}</h2>
      <div class="upload-controls">
        <label class="file-picker">
          <span>{{ $t('page.files.chooseFile') }}</span>
          <input type="file" :disabled="uploading" @change="onFileChange" />
        </label>
        <label class="field">
          <span>{{ $t('page.files.acl') }}</span>
          <select v-model="acl" :disabled="uploading">
            <option value="private">{{ $t('page.files.private') }}</option>
            <option value="public-read">
              {{ $t('page.files.publicRead') }}
            </option>
          </select>
        </label>
        <button
          class="primary"
          type="button"
          :disabled="uploading || !selectedFile"
          @click="upload"
        >
          {{ uploading ? $t('page.files.uploading') : $t('page.files.upload') }}
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
        <h2 id="files-table-title">{{ $t('page.files.tableTitle') }}</h2>
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
                  {{ $t('page.files.preview') }}
                </button>
                <button
                  type="button"
                  :disabled="actionId === item.id"
                  @click="download(item)"
                >
                  {{ $t('page.files.download') }}
                </button>
                <button
                  v-if="canManage"
                  type="button"
                  :disabled="actionId === item.id"
                  @click="createSignedURL(item)"
                >
                  {{ $t('page.files.signedURL') }}
                </button>
                <button
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
      aria-labelledby="files-cleanup-title"
    >
      <h2 id="files-cleanup-title">{{ $t('page.files.cleanupTitle') }}</h2>
      <p>{{ $t('page.files.cleanupDescription') }}</p>
      <div class="cleanup-controls">
        <label class="field">
          <span>{{ $t('page.files.cleanupAge') }}</span>
          <input v-model.number="cleanupAge" min="1" type="number" />
        </label>
        <button type="button" :disabled="cleanupLoading" @click="cleanupDryRun">
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
  </ManagementPage>
</template>

<style scoped>
.files-page {
  color: #172033;
}

.page-heading,
.table-heading {
  display: flex;
  gap: 24px;
  align-items: flex-start;
  justify-content: space-between;
}

.eyebrow {
  font-size: 0.76rem;
  font-weight: 700;
  color: #5267d9;
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

h1 {
  margin: 4px 0 8px;
  font-size: clamp(1.7rem, 4vw, 2.5rem);
}

h2 {
  margin: 0 0 12px;
  font-size: 1.1rem;
}

.description,
.help,
.cleanup-card p {
  color: #64748b;
}

.provider-chip {
  padding: 8px 12px;
  white-space: nowrap;
  border: 1px solid #cbd5e1;
  border-radius: 999px;
}

.feedback {
  padding: 12px 14px;
  margin: 16px 0;
  border-radius: 10px;
}

.feedback-error {
  color: #be123c;
  background: #fff1f2;
}

.feedback-success {
  color: #047857;
  background: #ecfdf5;
}

.upload-card,
.table-card,
.cleanup-card {
  padding: 22px;
  margin-top: 20px;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  box-shadow: 0 8px 30px rgb(15 23 42 / 5%);
}

.upload-controls,
.cleanup-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: end;
}

.file-picker,
.field {
  display: grid;
  gap: 6px;
  min-width: 180px;
}

.file-picker span,
.field span {
  font-size: 0.85rem;
  font-weight: 600;
  color: #475569;
}

input,
select,
button {
  min-height: 40px;
  padding: 8px 12px;
  font: inherit;
  border: 1px solid #cbd5e1;
  border-radius: 9px;
}

button {
  cursor: pointer;
  background: #fff;
}

button:hover:not(:disabled),
button:focus-visible {
  outline: 3px solid rgb(82 103 217 / 22%);
  border-color: #5267d9;
}

button.primary {
  color: #fff;
  background: #5267d9;
  border-color: #5267d9;
}

button.danger {
  color: #be123c;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.selected-file {
  margin: 14px 0 0;
  font-weight: 600;
  color: #334155;
}

.table-scroll {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 900px;
  border-collapse: collapse;
}

th,
td {
  padding: 12px 10px;
  vertical-align: top;
  text-align: left;
  border-bottom: 1px solid #e2e8f0;
}

th {
  font-size: 0.78rem;
  color: #475569;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.name-cell {
  max-width: 220px;
  font-weight: 650;
  overflow-wrap: anywhere;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.empty-state {
  padding: 28px 0;
  color: #64748b;
  text-align: center;
}

.sr-only,
.sr-status {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@media (max-width: 768px) {
  .page-heading {
    display: grid;
  }

  .provider-chip {
    justify-self: start;
  }

  .upload-controls,
  .cleanup-controls {
    flex-direction: column;
    align-items: stretch;
  }

  .field,
  .file-picker {
    width: 100%;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
