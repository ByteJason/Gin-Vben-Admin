<script setup lang="ts">
import type { TaskDefinition, TaskDefinitionInput, TaskRun, TaskRunLog } from '#/api/core/tasks';

import { computed, onMounted, reactive, ref } from 'vue';

import {
  cancelTaskRunApi,
  createTaskApi,
  deleteTaskApi,
  listTaskRunLogsApi,
  listTaskRunsApi,
  listTasksApi,
  retryTaskRunApi,
  runTaskApi,
  updateTaskApi,
} from '#/api/core/tasks';
import { $t } from '#/locales';

const tasks = ref<TaskDefinition[]>([]);
const runs = ref<TaskRun[]>([]);
const logs = ref<Record<string, TaskRunLog[]>>({});
const runAction = ref('');
const selectedId = ref('');
const editingId = ref('');
const loading = ref(false);
const saving = ref(false);
const running = ref('');
const deleting = ref('');
const error = ref('');
const notice = ref('');

const emptyForm = (): TaskDefinitionInput & { payloadText: string } => ({
  name: '',
  type: 'manual',
  payloadSchema: { type: 'object' },
  payloadText: '{"type":"object"}',
  timezone: 'UTC',
  enabled: true,
  concurrency: 1,
  concurrencyPolicy: 'forbid',
  timeoutSeconds: 30,
  maxAttempts: 3,
  cron: '',
  idempotencyKey: '',
});
const form = reactive(emptyForm());
const selected = computed(() => tasks.value.find((item) => item.id === selectedId.value));

function resetForm() {
  Object.assign(form, emptyForm());
  editingId.value = '';
}
function editTask(item: TaskDefinition) {
  selectedId.value = item.id;
  editingId.value = item.id;
  Object.assign(form, {
    name: item.name,
    type: item.type,
    payloadSchema: item.payloadSchema,
    payloadText: JSON.stringify(item.payloadSchema, null, 2),
    timezone: item.timezone,
    enabled: item.enabled,
    concurrency: item.concurrency,
    concurrencyPolicy: item.concurrencyPolicy,
    timeoutSeconds: item.timeoutSeconds,
    maxAttempts: item.maxAttempts,
    cron: item.cron ?? '',
    idempotencyKey: item.idempotencyKey ?? '',
  });
  void loadRuns(item.id);
}
function input(): TaskDefinitionInput {
  return {
    name: form.name.trim(),
    type: form.type,
    payloadSchema: form.payloadSchema,
    timezone: String(form.timezone ?? 'UTC').trim() || 'UTC',
    enabled: form.enabled,
    concurrency: Number(form.concurrency),
    concurrencyPolicy: form.concurrencyPolicy,
    timeoutSeconds: Number(form.timeoutSeconds),
    maxAttempts: Number(form.maxAttempts),
    cron: form.cron?.trim() || undefined,
    idempotencyKey: form.idempotencyKey?.trim() || undefined,
  };
}
async function loadTasks() {
  loading.value = true;
  error.value = '';
  try {
    tasks.value = await listTasksApi();
    if (!tasks.value.some((item) => item.id === selectedId.value)) {
      selectedId.value = tasks.value[0]?.id ?? '';
    }
    if (selected.value) await loadRuns(selected.value.id);
  } catch {
    error.value = String($t('page.tasks.loadError'));
  } finally {
    loading.value = false;
  }
}
async function loadRuns(id: string) {
  try {
    runs.value = await listTaskRunsApi(id);
  } catch {
    runs.value = [];
    error.value = String($t('page.tasks.runsLoadError'));
  }
}
async function loadRunLogs(taskId: string, runId: string) {
  try {
    logs.value[runId] = await listTaskRunLogsApi(taskId, runId);
  } catch {
    logs.value[runId] = [];
    error.value = String($t('page.tasks.logsLoadError'));
  }
}
async function saveTask() {
  error.value = '';
  notice.value = '';
  try {
    form.payloadSchema = JSON.parse(form.payloadText) as Record<string, unknown>;
  } catch {
    error.value = String($t('page.tasks.payloadInvalid'));
    return;
  }
  if (!form.name.trim()) {
    error.value = String($t('page.tasks.nameRequired'));
    return;
  }
  saving.value = true;
  try {
    if (editingId.value) await updateTaskApi(editingId.value, input());
    else await createTaskApi(input());
    notice.value = String($t('page.tasks.saved'));
    resetForm();
    await loadTasks();
  } catch {
    error.value = String($t('page.tasks.saveError'));
  } finally {
    saving.value = false;
  }
}
async function removeTask(item: TaskDefinition) {
  if (!window.confirm(String($t('page.tasks.deleteConfirm')))) return;
  deleting.value = item.id;
  try {
    await deleteTaskApi(item.id);
    notice.value = String($t('page.tasks.deleted'));
    if (selectedId.value === item.id) {
      selectedId.value = '';
      runs.value = [];
    }
    await loadTasks();
  } catch {
    error.value = String($t('page.tasks.deleteError'));
  } finally {
    deleting.value = '';
  }
}
async function runTask(item: TaskDefinition) {
  if (!window.confirm(String($t('page.tasks.runConfirm')))) return;
  running.value = item.id;
  error.value = '';
  try {
    await runTaskApi(item.id, { confirm: true, idempotencyKey: item.idempotencyKey });
    notice.value = String($t('page.tasks.runAccepted'));
    await loadRuns(item.id);
  } catch {
    error.value = String($t('page.tasks.runError'));
  } finally {
    running.value = '';
  }
}
async function cancelRun(item: TaskRun) {
  if (!selected.value || !window.confirm(String($t('page.tasks.cancelRunConfirm')))) return;
  runAction.value = item.id;
  error.value = '';
  try {
    await cancelTaskRunApi(selected.value.id, item.id);
    notice.value = String($t('page.tasks.runCancelled'));
    await loadRuns(selected.value.id);
  } catch {
    error.value = String($t('page.tasks.cancelRunError'));
  } finally {
    runAction.value = '';
  }
}
async function retryRun(item: TaskRun) {
  if (!selected.value || !window.confirm(String($t('page.tasks.retryRunConfirm')))) return;
  runAction.value = item.id;
  error.value = '';
  try {
    await retryTaskRunApi(selected.value.id, item.id);
    notice.value = String($t('page.tasks.retryAccepted'));
    await loadRuns(selected.value.id);
  } catch {
    error.value = String($t('page.tasks.retryError'));
  } finally {
    runAction.value = '';
  }
}

onMounted(() => void loadTasks());
</script>

<template>
  <main class="tasks-page" :aria-busy="loading || saving || Boolean(running) || Boolean(runAction)" aria-labelledby="tasks-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">{{ $t('page.tasks.eyebrow') }}</p>
        <h1 id="tasks-title">{{ $t('page.tasks.title') }}</h1>
        <p class="description">{{ $t('page.tasks.description') }}</p>
      </div>
      <div class="toolbar">
        <button class="secondary" type="button" :disabled="loading" @click="loadTasks">{{ $t('page.tasks.refresh') }}</button>
        <button class="secondary" type="button" @click="resetForm">{{ $t('page.tasks.newTask') }}</button>
      </div>
    </header>

    <p v-if="error" class="feedback error" role="alert" tabindex="-1">{{ error }}</p>
    <p v-if="notice" class="feedback success" role="status">{{ notice }}</p>
    <p class="sr-status" aria-live="polite">{{ loading ? $t('page.tasks.loading') : '' }}</p>

    <section class="workspace-grid">
      <article class="panel" aria-labelledby="tasks-list-title">
        <div class="section-heading"><div><p class="eyebrow">{{ $t('page.tasks.listEyebrow') }}</p><h2 id="tasks-list-title">{{ $t('page.tasks.listTitle') }}</h2></div></div>
        <div class="table-scroll">
          <table>
            <caption class="sr-only">{{ $t('page.tasks.tableLabel') }}</caption>
            <thead><tr><th scope="col">{{ $t('page.tasks.name') }}</th><th scope="col">{{ $t('page.tasks.type') }}</th><th scope="col">{{ $t('page.tasks.status') }}</th><th scope="col">{{ $t('page.tasks.actions') }}</th></tr></thead>
            <tbody>
              <tr v-if="!loading && tasks.length === 0"><td class="table-state" colspan="4">{{ $t('page.tasks.empty') }}</td></tr>
              <tr v-for="item in tasks" :key="item.id" :class="{ selected: selectedId === item.id }">
                <th scope="row"><button class="link-button" type="button" @click="editTask(item)">{{ item.name }}</button><small>{{ item.cron || $t('page.tasks.manual') }}</small></th>
                <td>{{ item.type }}</td>
                <td><span :class="['status-pill', item.enabled ? 'ok' : 'off']">{{ item.enabled ? $t('page.tasks.enabled') : $t('page.tasks.disabled') }}</span></td>
                <td class="actions"><button type="button" @click="editTask(item)">{{ $t('page.tasks.edit') }}</button><button type="button" :disabled="running === item.id" @click="runTask(item)">{{ running === item.id ? $t('page.tasks.running') : $t('page.tasks.run') }}</button><button class="danger" type="button" :disabled="deleting === item.id" @click="removeTask(item)">{{ $t('page.tasks.delete') }}</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </article>

      <article class="panel" aria-labelledby="tasks-form-title">
        <div class="section-heading"><div><p class="eyebrow">{{ $t('page.tasks.formEyebrow') }}</p><h2 id="tasks-form-title">{{ editingId ? $t('page.tasks.editTitle') : $t('page.tasks.newTitle') }}</h2></div></div>
        <form class="task-form" @submit.prevent="saveTask">
          <label><span>{{ $t('page.tasks.name') }}</span><input v-model="form.name" required /></label>
          <label><span>{{ $t('page.tasks.type') }}</span><select v-model="form.type"><option value="manual">manual</option><option value="http">http</option><option value="webhook">webhook</option></select></label>
          <label><span>{{ $t('page.tasks.timezone') }}</span><input v-model="form.timezone" required /></label>
          <label><span>{{ $t('page.tasks.cron') }}</span><input v-model="form.cron" placeholder="0 * * * *" /></label>
          <label><span>{{ $t('page.tasks.concurrency') }}</span><input v-model.number="form.concurrency" min="1" type="number" /></label>
          <label><span>{{ $t('page.tasks.timeout') }}</span><input v-model.number="form.timeoutSeconds" min="1" type="number" /></label>
          <label><span>{{ $t('page.tasks.maxAttempts') }}</span><input v-model.number="form.maxAttempts" min="1" max="10" type="number" /></label>
          <label><span>{{ $t('page.tasks.policy') }}</span><select v-model="form.concurrencyPolicy"><option value="forbid">forbid</option><option value="allow">allow</option><option value="replace">replace</option></select></label>
          <label class="wide"><span>{{ $t('page.tasks.payloadSchema') }}</span><textarea v-model="form.payloadText" rows="5" required /></label>
          <label class="toggle"><input v-model="form.enabled" type="checkbox" /><span>{{ $t('page.tasks.enabled') }}</span></label>
          <div class="form-actions"><button class="primary" type="submit" :disabled="saving">{{ saving ? $t('page.tasks.saving') : $t('page.tasks.save') }}</button><button v-if="editingId" class="secondary" type="button" @click="resetForm">{{ $t('page.tasks.cancel') }}</button></div>
        </form>
        <section v-if="selected" class="runs" aria-labelledby="task-runs-title">
          <div class="section-heading"><h3 id="task-runs-title">{{ $t('page.tasks.runsTitle') }}</h3><button class="secondary" type="button" @click="loadRuns(selected.id)">{{ $t('page.tasks.refreshRuns') }}</button></div>
          <p v-if="runs.length === 0" class="empty-state">{{ $t('page.tasks.runsEmpty') }}</p>
          <ul v-else><li v-for="run in runs" :key="run.id"><div><span>{{ run.status }}</span><small>{{ run.attemptCount }}/{{ run.maxAttempts }} · {{ run.createdAt }}</small></div><div class="run-actions"><button v-if="run.status === 'pending' || run.status === 'failed'" type="button" :disabled="runAction === run.id" @click="cancelRun(run)">{{ $t('page.tasks.cancelRun') }}</button><button v-if="run.status === 'failed' || run.status === 'dead_letter'" type="button" :disabled="runAction === run.id" @click="retryRun(run)">{{ $t('page.tasks.retry') }}</button><button type="button" @click="loadRunLogs(selected.id, run.id)">{{ $t('page.tasks.logs') }}</button></div><ul v-if="logs[run.id]?.length" class="run-logs"><li v-for="entry in logs[run.id]" :key="entry.id"><span>{{ entry.status }}</span><small>{{ entry.errorCode || $t('page.tasks.noError') }} · {{ entry.createdAt }}</small></li></ul></li></ul>
        </section>
      </article>
    </section>
  </main>
</template>

<style scoped>
.tasks-page { --ink:#172033; --muted:#64748b; --line:#dbe3ef; --accent:#2563eb; --ok:#15803d; --danger:#b42318; max-width:1600px; margin:0 auto; padding:32px; color:var(--ink); }
.page-heading,.section-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; }.toolbar,.actions,.form-actions{display:flex;gap:8px;flex-wrap:wrap}.eyebrow{margin:0 0 6px;color:#5267d9;font-size:.72rem;font-weight:800;letter-spacing:.12em}h1{margin:0 0 8px;font-size:clamp(1.7rem,4vw,2.5rem)}h2,h3{margin:0;font-size:1.15rem}.description,.muted,small{color:var(--muted)}.workspace-grid{display:grid;grid-template-columns:minmax(420px,1fr) minmax(480px,1.2fr);gap:24px;margin-top:24px}.panel{border:1px solid var(--line);border-radius:16px;background:color-mix(in srgb,#fff 94%,#dbeafe);padding:24px;box-shadow:0 10px 28px rgb(30 41 59 / 7%)}.task-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px;margin-top:20px}.task-form label{display:grid;gap:7px;font-size:.82rem;font-weight:700}.task-form .wide{grid-column:1/-1}.task-form input,.task-form select,.task-form textarea{min-height:40px;border:1px solid #cbd5e1;border-radius:9px;padding:8px 10px;color:var(--ink);background:#fff}.task-form textarea{resize:vertical;font-family:ui-monospace,monospace}.primary,.secondary,.actions button{min-height:40px;border:1px solid #cbd5e1;border-radius:9px;padding:0 13px;cursor:pointer;background:#fff}.primary{border-color:var(--accent);background:var(--accent);color:#fff;font-weight:700}.danger{color:var(--danger)}button:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid rgb(37 99 235 / 25%);outline-offset:2px}.feedback{margin:20px 0 0;border-radius:10px;padding:12px 14px}.error{color:#8b1e1e;background:#fef2f2}.success{color:#166534;background:#f0fdf4}.table-scroll{overflow-x:auto;margin-top:18px}table{width:100%;border-collapse:collapse;min-width:580px}th,td{border-bottom:1px solid var(--line);padding:11px 9px;text-align:left;vertical-align:middle}th{color:var(--muted);font-size:.74rem;letter-spacing:.04em;text-transform:uppercase}td small{display:block;margin-top:3px;font-weight:400}.link-button{border:0;background:transparent;color:var(--accent);font-weight:700;padding:0;cursor:pointer;text-align:left}.selected{background:#eff6ff}.status-pill{display:inline-flex;border-radius:999px;padding:4px 9px;font-size:.74rem;font-weight:800}.status-pill.ok{color:var(--ok);background:#dcfce7}.status-pill.off{color:#92400e;background:#fef3c7}.toggle{display:flex!important;align-items:center;gap:8px!important;padding-top:20px}.toggle input{width:18px;height:18px}.empty-state,.sr-status{color:var(--muted)}.table-state{text-align:center;color:var(--muted)}.runs{border-top:1px solid var(--line);margin-top:24px;padding-top:18px}.runs ul{list-style:none;padding:0;margin:12px 0}.runs li{display:flex;justify-content:space-between;gap:12px;border-bottom:1px solid var(--line);padding:10px 0}.run-actions{display:flex;gap:6px;flex-wrap:wrap}.run-logs{width:100%;margin:8px 0 0!important;padding-left:12px!important;font-size:.8rem}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
@media (max-width:1100px){.workspace-grid{grid-template-columns:1fr}.tasks-page{padding:22px 16px}}@media (max-width:560px){.page-heading,.section-heading,.toolbar{flex-direction:column;align-items:stretch}.task-form{grid-template-columns:1fr}.task-form .wide{grid-column:auto}.toggle{padding-top:0}}@media (prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;transition-duration:.01ms!important;animation-duration:.01ms!important}}
</style>
