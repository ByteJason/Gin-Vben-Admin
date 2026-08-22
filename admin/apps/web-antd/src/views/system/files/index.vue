<script setup lang="ts">
import type { FileACL, FileObject, FilePage } from '#/api/core/files';

import { computed, nextTick, onMounted, ref } from 'vue';

import {
  cleanupDryRunApi,
  deleteFileApi,
  downloadFileApi,
  listFilesApi,
  signedFileUrlApi,
  uploadFileApi,
} from '#/api/core/files';
import { $t } from '#/locales';

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
const cleanupReport = ref<{ bytes: number; cutoff: string; matchingCount: number }>();

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
  const input = event.target as HTMLInputElement;
  selectedFile.value = input.files?.[0] ?? null;
}

async function upload() {
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
  <main class="files-page" :aria-busy="loading || uploading" aria-labelledby="files-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.files.eyebrow') }}</p>
        <h1 id="files-title">{{ $t('page.files.title') }}</h1>
        <p class="description">{{ $t('page.files.description') }}</p>
      </div>
      <span class="provider-chip">{{ $t('page.files.localProvider') }}</span>
    </header>

    <p v-if="error" ref="errorSummary" class="feedback feedback-error" role="alert" tabindex="-1">
      {{ error }}
    </p>
    <p v-if="message" class="feedback feedback-success" aria-live="polite">{{ message }}</p>
    <p class="sr-status" aria-live="polite">{{ loading ? $t('page.files.loading') : '' }}</p>

    <section class="upload-card" aria-labelledby="files-upload-title">
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
            <option value="public-read">{{ $t('page.files.publicRead') }}</option>
          </select>
        </label>
        <button class="primary" type="button" :disabled="uploading || !selectedFile" @click="upload">
          {{ uploading ? $t('page.files.uploading') : $t('page.files.upload') }}
        </button>
      </div>
      <p v-if="selectedFile" class="selected-file" aria-live="polite">
        {{ selectedFile.name }} · {{ formatSize(selectedFile.size) }} · {{ selectedFile.type || 'application/octet-stream' }}
      </p>
      <p class="help">{{ $t('page.files.uploadHelp') }}</p>
    </section>

    <section class="table-card" aria-labelledby="files-table-title">
      <div class="table-heading">
        <h2 id="files-table-title">{{ $t('page.files.tableTitle') }}</h2>
        <button type="button" :disabled="loading" @click="load">{{ $t('page.files.refresh') }}</button>
      </div>
      <p v-if="!loading && !hasFiles" class="empty-state">{{ $t('page.files.empty') }}</p>
      <div v-else class="table-scroll">
        <table>
          <caption class="sr-only">{{ $t('page.files.tableLabel') }}</caption>
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
                <button type="button" :disabled="actionId === item.id" @click="download(item, true)">{{ $t('page.files.preview') }}</button>
                <button type="button" :disabled="actionId === item.id" @click="download(item)">{{ $t('page.files.download') }}</button>
                <button type="button" :disabled="actionId === item.id" @click="createSignedURL(item)">{{ $t('page.files.signedURL') }}</button>
                <button class="danger" type="button" :disabled="actionId === item.id" @click="deleteItem(item)">{{ $t('page.files.delete') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="cleanup-card" aria-labelledby="files-cleanup-title">
      <h2 id="files-cleanup-title">{{ $t('page.files.cleanupTitle') }}</h2>
      <p>{{ $t('page.files.cleanupDescription') }}</p>
      <div class="cleanup-controls">
        <label class="field">
          <span>{{ $t('page.files.cleanupAge') }}</span>
          <input v-model.number="cleanupAge" min="1" type="number" />
        </label>
        <button type="button" :disabled="cleanupLoading" @click="cleanupDryRun">
          {{ cleanupLoading ? $t('page.files.cleanupRunning') : $t('page.files.cleanupDryRun') }}
        </button>
      </div>
      <p v-if="cleanupReport" class="cleanup-result" aria-live="polite">
        {{ $t('page.files.cleanupResult', { count: cleanupReport.matchingCount, bytes: formatSize(cleanupReport.bytes) }) }}
      </p>
    </section>
  </main>
</template>

<style scoped>
.files-page { max-width: 1280px; margin: 0 auto; padding: 32px; color: #172033; }
.page-heading, .table-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; }
.eyebrow { color: #5267d9; font-size: .76rem; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
h1 { margin: 4px 0 8px; font-size: clamp(1.7rem, 4vw, 2.5rem); }
h2 { margin: 0 0 12px; font-size: 1.1rem; }
.description, .help, .cleanup-card p { color: #64748b; }
.provider-chip { border: 1px solid #cbd5e1; border-radius: 999px; padding: 8px 12px; white-space: nowrap; }
.feedback { border-radius: 10px; margin: 16px 0; padding: 12px 14px; }
.feedback-error { background: #fff1f2; color: #be123c; }
.feedback-success { background: #ecfdf5; color: #047857; }
.upload-card, .table-card, .cleanup-card { border: 1px solid #e2e8f0; border-radius: 16px; background: #fff; box-shadow: 0 8px 30px rgb(15 23 42 / 5%); margin-top: 20px; padding: 22px; }
.upload-controls, .cleanup-controls { align-items: end; display: flex; flex-wrap: wrap; gap: 12px; }
.file-picker, .field { display: grid; gap: 6px; min-width: 180px; }
.file-picker span, .field span { color: #475569; font-size: .85rem; font-weight: 600; }
input, select, button { border: 1px solid #cbd5e1; border-radius: 9px; font: inherit; min-height: 40px; padding: 8px 12px; }
button { background: #fff; cursor: pointer; }
button:hover:not(:disabled), button:focus-visible { border-color: #5267d9; outline: 3px solid rgb(82 103 217 / 22%); }
button.primary { background: #5267d9; border-color: #5267d9; color: #fff; }
button.danger { color: #be123c; }
button:disabled { cursor: not-allowed; opacity: .55; }
.selected-file { color: #334155; font-weight: 600; margin: 14px 0 0; }
.table-scroll { overflow-x: auto; }
table { border-collapse: collapse; min-width: 900px; width: 100%; }
th, td { border-bottom: 1px solid #e2e8f0; padding: 12px 10px; text-align: left; vertical-align: top; }
th { color: #475569; font-size: .78rem; letter-spacing: .04em; text-transform: uppercase; }
.name-cell { font-weight: 650; max-width: 220px; overflow-wrap: anywhere; }
.actions { display: flex; flex-wrap: wrap; gap: 6px; }
.empty-state { color: #64748b; padding: 28px 0; text-align: center; }
.sr-only, .sr-status { clip: rect(0 0 0 0); clip-path: inset(50%); height: 1px; overflow: hidden; position: absolute; white-space: nowrap; width: 1px; }
@media (max-width: 768px) { .files-page { padding: 20px 14px; } .page-heading { display: grid; } .provider-chip { justify-self: start; } .upload-controls, .cleanup-controls { align-items: stretch; flex-direction: column; } .field, .file-picker { width: 100%; } }
@media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; } }
</style>
