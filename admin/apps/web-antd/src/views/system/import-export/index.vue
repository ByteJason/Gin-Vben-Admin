<script setup lang="ts">
import type { ImportExportJob, ImportPreview } from '#/api/core/import-export';

import { computed, onMounted, ref } from 'vue';

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
  } finally {
    loading.value = false;
  }
}

async function chooseFile(event: Event) {
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
  } catch {
    error.value = String($t('page.importExport.previewError'));
  } finally {
    busy.value = '';
  }
}

async function commitImport() {
  if (!preview.value) return;
  busy.value = 'commit';
  error.value = '';
  try {
    await commitImportApi({ previewId: preview.value.id, idempotencyKey: `ui-${preview.value.id}` });
    notice.value = String($t('page.importExport.commitQueued'));
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.commitError'));
  } finally {
    busy.value = '';
  }
}

async function exportPreview() {
  if (!preview.value) return;
  busy.value = 'export';
  try {
    await startExportApi({
      fields: preview.value.headers,
      allowlist: Object.fromEntries(preview.value.headers.map((field) => [field, true])),
      rows: preview.value.previewRows,
      idempotencyKey: `export-${preview.value.id}`,
    });
    notice.value = String($t('page.importExport.exportQueued'));
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.exportError'));
  } finally {
    busy.value = '';
  }
}

async function cancelJob(job: ImportExportJob) {
  busy.value = job.id;
  try {
    await cancelImportExportJobApi(job.id);
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.cancelError'));
  } finally {
    busy.value = '';
  }
}

async function retryJob(job: ImportExportJob) {
  busy.value = job.id;
  try {
    await retryImportExportJobApi(job.id);
    await loadJobs();
  } catch {
    error.value = String($t('page.importExport.retryError'));
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
  <div class="import-export-page">
    <header class="page-heading">
      <div>
        <p class="eyebrow">IMPORT / EXPORT</p>
        <h1>{{ $t('page.importExport.title') }}</h1>
        <p class="description">{{ $t('page.importExport.description') }}</p>
        <p class="muted">{{ $t('page.importExport.limits') }}</p>
        <small class="muted">50 MB / 100,000 rows</small>
      </div>
      <div class="toolbar" aria-label="Template downloads">
        <button class="secondary" type="button" @click="downloadTemplate('csv')">{{ $t('page.importExport.templateCsv') }}</button>
        <button class="secondary" type="button" @click="downloadTemplate('xlsx')">{{ $t('page.importExport.templateXlsx') }}</button>
        <button class="secondary" type="button" :disabled="loading" @click="loadJobs">{{ $t('page.importExport.refresh') }}</button>
      </div>
    </header>

    <div v-if="error" class="feedback error" role="alert">{{ error }}</div>
    <div v-if="notice" class="feedback success" role="status">{{ notice }}</div>

    <section class="panel" aria-labelledby="import-title">
      <div class="section-heading">
        <div><p class="eyebrow">IMPORT</p><h2 id="import-title">{{ $t('page.importExport.importTitle') }}</h2></div>
        <label class="file-picker">{{ $t('page.importExport.chooseFile') }}<input type="file" accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" @change="chooseFile" /></label>
      </div>
      <p v-if="selectedFile" class="muted">{{ selectedFile.name }} · {{ selectedFile.size }} bytes</p>
      <div v-if="preview" class="preview" aria-live="polite">
        <p>{{ $t('page.importExport.previewSummary', { rows: preview.totalRows, errors: preview.errors.length }) }}</p>
        <div class="table-scroll"><table><caption class="sr-only">{{ $t('page.importExport.previewTable') }}</caption><thead><tr><th v-for="column in preview.headers" :key="column">{{ column }}</th></tr></thead><tbody><tr v-for="(row, index) in preview.previewRows" :key="index"><td v-for="column in preview.headers" :key="column">{{ row[column] }}</td></tr></tbody></table></div>
        <div class="actions"><button class="primary" type="button" :disabled="busy !== ''" @click="commitImport">{{ busy === 'commit' ? $t('page.importExport.working') : $t('page.importExport.commit') }}</button><button class="secondary" type="button" :disabled="busy !== ''" @click="exportPreview">{{ busy === 'export' ? $t('page.importExport.working') : $t('page.importExport.export') }}</button></div>
      </div>
      <p v-else class="empty-state">{{ $t('page.importExport.previewEmpty') }}</p>
    </section>

    <section class="panel" aria-labelledby="jobs-title">
      <div class="section-heading"><div><p class="eyebrow">ASYNC JOBS</p><h2 id="jobs-title">{{ $t('page.importExport.jobsTitle') }}</h2></div></div>
      <p v-if="loading" class="table-state" role="status">{{ $t('page.importExport.loading') }}</p>
      <p v-else-if="jobs.length === 0" class="empty-state">{{ $t('page.importExport.jobsEmpty') }}</p>
      <div v-else class="table-scroll"><table><caption class="sr-only">{{ $t('page.importExport.jobsTable') }}</caption><thead><tr><th>{{ $t('page.importExport.kind') }}</th><th>{{ $t('page.importExport.status') }}</th><th>{{ $t('page.importExport.progress') }}</th><th>{{ $t('page.importExport.actions') }}</th></tr></thead><tbody><tr v-for="job in jobs" :key="job.id"><td>{{ job.kind }}</td><td><span class="status-pill">{{ job.status }}</span></td><td>{{ job.processedRows }} / {{ job.totalRows }} · {{ job.errorCount }} {{ $t('page.importExport.errors') }}</td><td class="actions"><button v-if="['pending','running','failed'].includes(job.status)" class="secondary" type="button" :disabled="busy === job.id" @click="cancelJob(job)">{{ $t('page.importExport.cancel') }}</button><button v-if="['failed','cancelled'].includes(job.status)" class="secondary" type="button" :disabled="busy === job.id" @click="retryJob(job)">{{ $t('page.importExport.retry') }}</button><button v-if="job.kind === 'import' && job.errorCount" class="link-button" type="button" @click="downloadErrors(job)">{{ $t('page.importExport.downloadErrors') }}</button></td></tr></tbody></table></div>
      <p v-if="selectedJob" class="muted">{{ selectedJob.id }}</p>
    </section>
  </div>
</template>

<style scoped>
.import-export-page{max-width:1200px;margin:0 auto;padding:32px 24px;color:#172033}.page-heading,.section-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.eyebrow{margin:0 0 6px;color:#5267d9;font-size:.72rem;font-weight:800;letter-spacing:.12em}.page-heading h1{margin:0 0 8px;font-size:clamp(1.7rem,4vw,2.5rem)}h2{margin:0;font-size:1.15rem}.description,.muted,small{color:#64748b}.toolbar,.actions{display:flex;gap:8px;flex-wrap:wrap}.panel{border:1px solid #dbe2ea;border-radius:16px;background:#fff;padding:24px;margin-top:24px;box-shadow:0 10px 28px rgb(30 41 59 / 7%)}button{min-height:40px;border:1px solid #cbd5e1;border-radius:9px;padding:0 13px;cursor:pointer;background:#fff}button:disabled{cursor:not-allowed;opacity:.55}.primary{border-color:#5267d9;background:#5267d9;color:#fff;font-weight:700}.file-picker{display:inline-flex;align-items:center;gap:8px;border:1px solid #5267d9;border-radius:9px;padding:10px 13px;color:#3144a8;font-weight:700;cursor:pointer}.file-picker input{position:absolute;width:1px;height:1px;opacity:0}.feedback{margin:20px 0 0;border-radius:10px;padding:12px 14px}.error{color:#8b1e1e;background:#fef2f2}.success{color:#166534;background:#f0fdf4}.preview{margin-top:18px}.table-scroll{overflow-x:auto;margin-top:18px}table{width:100%;border-collapse:collapse;min-width:560px}th,td{border-bottom:1px solid #e2e8f0;padding:11px 9px;text-align:left;vertical-align:middle}.empty-state,.table-state{text-align:center;color:#64748b;padding:24px}.status-pill{display:inline-flex;border-radius:999px;padding:4px 9px;background:#eff6ff;font-size:.74rem;font-weight:800}.link-button{border:0;background:transparent;color:#5267d9;font-weight:700;padding:0}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}@media (max-width:760px){.page-heading,.section-heading{display:grid}.toolbar{justify-content:flex-start}.panel{padding:18px}}
</style>
