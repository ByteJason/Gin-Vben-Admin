<script setup lang="ts">
import type { ImportExportJob, ImportPreview } from '#/api/core/import-export';

import { computed, onMounted, ref } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementPage, notify } from '@vben/common-ui';

import {
  cancelImportExportJobApi,
  commitImportApi,
  downloadImportErrorsApi,
  downloadImportTemplateApi,
  listImportExportJobsApi,
  previewImportApi,
  retryImportExportJobApi,
  startExportApi,
} from '#/api/core/import-export';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['ops:data-jobs:manage']));

const preview = ref<ImportPreview | null>(null);
const jobs = ref<ImportExportJob[]>([]);
const selectedFile = ref<File | null>(null);
const loading = ref(false);
const busy = ref('');
const error = ref('');
const notice = ref('');
const selectedJob = computed(() => jobs.value[0]);

async function loadJobs() {
  loading.value = true;
  error.value = '';
  try {
    const result = await listImportExportJobsApi();
    jobs.value = result.items;
  } catch {
    error.value = String($t('page.importExport.loadError'));
    notify('error', error.value);
  } finally {
    loading.value = false;
  }
}

async function chooseFile(event: Event) {
  if (!canManage.value) return;
  const input = event.target as HTMLInputElement;
  selectedFile.value = input.files?.[0] ?? null;
  preview.value = null;
  notice.value = '';
  error.value = '';
  if (!selectedFile.value) return;
  const form = new FormData();
  form.append('file', selectedFile.value);
  form.append('columns', 'name,email');
  form.append('allowlist', 'name,email');
  busy.value = 'preview';
  try {
    preview.value = await previewImportApi(form);
    notice.value = String($t('page.importExport.previewReady'));
    notify('success', notice.value);
  } catch {
    error.value = String($t('page.importExport.previewError'));
    notify('error', error.value);
  } finally {
    busy.value = '';
  }
}

async function commitImport() {
  if (!canManage.value) return;
  if (!preview.value) return;
  busy.value = 'commit';
  error.value = '';
  try {
    await commitImportApi({
      previewId: preview.value.id,
      idempotencyKey: `ui-${preview.value.id}`,
    });
    notice.value = String($t('page.importExport.commitQueued'));
    notify('success', notice.value);
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.commitError'));
    notify('error', error.value);
  } finally {
    busy.value = '';
  }
}

async function exportPreview() {
  if (!canManage.value) return;
  if (!preview.value) return;
  busy.value = 'export';
  try {
    await startExportApi({
      fields: preview.value.headers,
      allowlist: Object.fromEntries(
        preview.value.headers.map((field) => [field, true]),
      ),
      rows: preview.value.previewRows,
      idempotencyKey: `export-${preview.value.id}`,
    });
    notice.value = String($t('page.importExport.exportQueued'));
    notify('success', notice.value);
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.exportError'));
    notify('error', error.value);
  } finally {
    busy.value = '';
  }
}

async function cancelJob(job: ImportExportJob) {
  if (!canManage.value) return;
  busy.value = job.id;
  try {
    await cancelImportExportJobApi(job.id);
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.cancelError'));
    notify('error', error.value);
  } finally {
    busy.value = '';
  }
}

async function retryJob(job: ImportExportJob) {
  if (!canManage.value) return;
  busy.value = job.id;
  try {
    await retryImportExportJobApi(job.id);
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.retryError'));
    notify('error', error.value);
  } finally {
    busy.value = '';
  }
}

async function downloadErrors(job: ImportExportJob) {
  const blob = await downloadImportErrorsApi(job.id);
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `import-errors-${job.id}.csv`;
  anchor.click();
  URL.revokeObjectURL(url);
}

async function downloadTemplate(format: 'csv' | 'xlsx') {
  const blob = await downloadImportTemplateApi(format);
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `import-template.${format}`;
  anchor.click();
  URL.revokeObjectURL(url);
}

onMounted(() => void loadJobs());
</script>

<template>
  <ManagementPage class="import-export-page">
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.importExport.eyebrow') }}</p>
        <h1>{{ $t('page.importExport.title') }}</h1>
        <p class="description">{{ $t('page.importExport.description') }}</p>
        <p class="muted">{{ $t('page.importExport.limits') }}</p>
        <small class="muted">{{ $t('page.importExport.limits') }}</small>
      </div>
      <div class="toolbar" aria-label="Template downloads">
        <button
          class="secondary"
          type="button"
          @click="downloadTemplate('csv')"
        >
          {{ $t('page.importExport.templateCsv') }}
        </button>
        <button
          class="secondary"
          type="button"
          @click="downloadTemplate('xlsx')"
        >
          {{ $t('page.importExport.templateXlsx') }}
        </button>
        <button
          class="secondary"
          type="button"
          :disabled="loading"
          @click="loadJobs"
        >
          {{ $t('page.importExport.refresh') }}
        </button>
      </div>
    </header>

    <section v-if="canManage" class="panel" aria-labelledby="import-title">
      <div class="section-heading">
        <div>
          <p class="eyebrow">{{ $t('page.importExport.import') }}</p>
          <h2 id="import-title">{{ $t('page.importExport.importTitle') }}</h2>
        </div>
        <label class="file-picker"
          >{{ $t('page.importExport.chooseFile')
          }}<input
            type="file"
            accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            @change="chooseFile"
        /></label>
      </div>
      <p v-if="selectedFile" class="muted">
        {{ selectedFile.name }} · {{ selectedFile.size }} bytes
      </p>
      <div v-if="preview" class="preview" aria-live="polite">
        <p>
          {{
            $t('page.importExport.previewSummary', {
              rows: preview.totalRows,
              errors: preview.errors.length,
            })
          }}
        </p>
        <div class="table-scroll">
          <table>
            <caption class="sr-only">
              {{
                $t('page.importExport.previewTable')
              }}
            </caption>
            <thead>
              <tr>
                <th v-for="column in preview.headers" :key="column">
                  {{ column }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, index) in preview.previewRows" :key="index">
                <td v-for="column in preview.headers" :key="column">
                  {{ row[column] }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="actions">
          <button
            class="primary"
            type="button"
            :disabled="busy !== ''"
            @click="commitImport"
          >
            {{
              busy === 'commit'
                ? $t('page.importExport.working')
                : $t('page.importExport.commit')
            }}</button
          ><button
            class="secondary"
            type="button"
            :disabled="busy !== ''"
            @click="exportPreview"
          >
            {{
              busy === 'export'
                ? $t('page.importExport.working')
                : $t('page.importExport.export')
            }}
          </button>
        </div>
      </div>
      <p v-else class="empty-state">
        {{ $t('page.importExport.previewEmpty') }}
      </p>
    </section>

    <section class="panel" aria-labelledby="jobs-title">
      <div class="section-heading">
        <div>
          <p class="eyebrow">{{ $t('page.importExport.jobsEyebrow') }}</p>
          <h2 id="jobs-title">{{ $t('page.importExport.jobsTitle') }}</h2>
        </div>
      </div>
      <p v-if="loading" class="table-state" role="status">
        {{ $t('page.importExport.loading') }}
      </p>
      <p v-else-if="jobs.length === 0" class="empty-state">
        {{ $t('page.importExport.jobsEmpty') }}
      </p>
      <div v-else class="table-scroll">
        <table>
          <caption class="sr-only">
            {{
              $t('page.importExport.jobsTable')
            }}
          </caption>
          <thead>
            <tr>
              <th>{{ $t('page.importExport.kind') }}</th>
              <th>{{ $t('page.importExport.status') }}</th>
              <th>{{ $t('page.importExport.progress') }}</th>
              <th>{{ $t('page.importExport.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="job in jobs" :key="job.id">
              <td>{{ job.kind }}</td>
              <td>
                <span class="status-pill">{{ job.status }}</span>
              </td>
              <td>
                {{ job.processedRows }} / {{ job.totalRows }} ·
                {{ job.errorCount }} {{ $t('page.importExport.errors') }}
              </td>
              <td class="actions">
                <button
                  v-if="
                    canManage &&
                    ['pending', 'running', 'failed'].includes(job.status)
                  "
                  class="secondary"
                  type="button"
                  :disabled="busy === job.id"
                  @click="cancelJob(job)"
                >
                  {{ $t('page.importExport.cancel') }}</button
                ><button
                  v-if="
                    canManage && ['failed', 'cancelled'].includes(job.status)
                  "
                  class="secondary"
                  type="button"
                  :disabled="busy === job.id"
                  @click="retryJob(job)"
                >
                  {{ $t('page.importExport.retry') }}</button
                ><button
                  v-if="job.kind === 'import' && job.errorCount"
                  class="link-button"
                  type="button"
                  @click="downloadErrors(job)"
                >
                  {{ $t('page.importExport.downloadErrors') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="selectedJob" class="muted">{{ selectedJob.id }}</p>
    </section>
  </ManagementPage>
</template>

<style scoped>
.import-export-page {
  color: #172033;
}

.page-heading,
.section-heading {
  display: flex;
  gap: 20px;
  align-items: flex-start;
  justify-content: space-between;
}

.eyebrow {
  margin: 0 0 6px;
  font-size: 0.72rem;
  font-weight: 800;
  color: #5267d9;
  letter-spacing: 0.12em;
}

.page-heading h1 {
  margin: 0 0 8px;
  font-size: clamp(1.7rem, 4vw, 2.5rem);
}

h2 {
  margin: 0;
  font-size: 1.15rem;
}

.description,
.muted,
small {
  color: #64748b;
}

.toolbar,
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.panel {
  padding: 24px;
  margin-top: 24px;
  background: #fff;
  border: 1px solid #dbe2ea;
  border-radius: 16px;
  box-shadow: 0 10px 28px rgb(30 41 59 / 7%);
}

button {
  min-height: 40px;
  padding: 0 13px;
  cursor: pointer;
  background: #fff;
  border: 1px solid #cbd5e1;
  border-radius: 9px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.primary {
  font-weight: 700;
  color: #fff;
  background: #5267d9;
  border-color: #5267d9;
}

.file-picker {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  padding: 10px 13px;
  font-weight: 700;
  color: #3144a8;
  cursor: pointer;
  border: 1px solid #5267d9;
  border-radius: 9px;
}

.file-picker input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.feedback {
  padding: 12px 14px;
  margin: 20px 0 0;
  border-radius: 10px;
}

.error {
  color: #8b1e1e;
  background: #fef2f2;
}

.success {
  color: #166534;
  background: #f0fdf4;
}

.preview {
  margin-top: 18px;
}

.table-scroll {
  margin-top: 18px;
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 560px;
  border-collapse: collapse;
}

th,
td {
  padding: 11px 9px;
  vertical-align: middle;
  text-align: left;
  border-bottom: 1px solid #e2e8f0;
}

.empty-state,
.table-state {
  padding: 24px;
  color: #64748b;
  text-align: center;
}

.status-pill {
  display: inline-flex;
  padding: 4px 9px;
  font-size: 0.74rem;
  font-weight: 800;
  background: #eff6ff;
  border-radius: 999px;
}

.link-button {
  padding: 0;
  font-weight: 700;
  color: #5267d9;
  background: transparent;
  border: 0;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

@media (max-width: 760px) {
  .page-heading,
  .section-heading {
    display: grid;
  }

  .toolbar {
    justify-content: flex-start;
  }

  .panel {
    padding: 18px;
  }
}
</style>
