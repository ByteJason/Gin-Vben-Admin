<script setup lang="ts">
import type {
  TaskDefinition,
  TaskDefinitionInput,
  TaskListQuery,
  TaskRun,
  TaskRunListQuery,
  TaskRunLog,
} from '#/api/core/tasks';

import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';

import { useAccess } from '@vben/access';
import { ManagementDrawer, ManagementPage, notify } from '@vben/common-ui';

import {
  cancelTaskRunApi,
  createTaskApi,
  deleteTaskApi,
  listAllTaskRunsApi,
  listTaskRunLogsApi,
  listTasksApi,
  listTaskMethodsApi,
  previewTaskScheduleApi,
  retryTaskRunApi,
  runTaskApi,
  updateTaskApi,
} from '#/api/core/tasks';
import { $t } from '#/locales';

const { hasAccessByCodes } = useAccess();
const canManage = computed(() => hasAccessByCodes(['ops:tasks:manage']));
const label = (key: string) => String($t(`page.tasks.${key}`));
const activeTab = ref<'runs' | 'tasks'>('tasks');
const tasks = ref<TaskDefinition[]>([]);
const runs = ref<TaskRun[]>([]);
const taskPage = reactive({ page: 1, pageSize: 10, total: 0 });
const runPage = reactive({ page: 1, pageSize: 10, total: 0 });
const taskFilter = reactive({ name: '', executorType: '', enabled: '' });
const runFilter = reactive({ taskId: '', taskName: '', status: '', triggerSource: '', from: '', to: '' });
const selectedIds = ref<string[]>([]);
const loading = ref(false);
const runsLoading = ref(false);
const saving = ref(false);
const actionId = ref('');
const batchAction = ref('');
const error = ref('');
const runsError = ref('');
const formError = ref('');
const notice = ref('');
const taskDrawerOpen = ref(false);
const taskModal = ref<HTMLDialogElement>();
const methods = ref<string[]>([]);
const methodsError = ref('');
const previewDates = ref<string[]>([]);
const previewError = ref('');
const previewLoading = ref(false);
const includeSeconds = ref(false);
let previewTimer: ReturnType<typeof setTimeout> | undefined;
let previewRequest = 0;
watch(taskDrawerOpen, async (open) => {
  await nextTick();
  if (open) { taskModal.value?.showModal(); void loadMethods(); } else taskModal.value?.close();
});
async function loadMethods() {
  methodsError.value = '';
  try { methods.value = await listTaskMethodsApi(); } catch { methodsError.value = label('methodsLoadError'); }
}
async function loadPreview() {
  const request = ++previewRequest;
  previewDates.value = []; previewError.value = '';
  if (!form.cron.trim()) { previewLoading.value = false; return; }
  previewLoading.value = true;
  try { const dates = await previewTaskScheduleApi(form.cron.trim(), form.timezone.trim()); if (request === previewRequest) previewDates.value = dates; }
  catch { if (request === previewRequest) previewError.value = label('previewInvalid'); }
  finally { if (request === previewRequest) previewLoading.value = false; }
}
function toggleSeconds() {
  const fields = form.cron.trim().split(/\s+/);
  if (fields.length === 5 && includeSeconds.value) form.cron = `0 ${form.cron.trim()}`;
  else if (fields.length === 6 && !includeSeconds.value) form.cron = fields.slice(1).join(' ');
}
function applyCronTemplate(event: Event) { form.cron = (event.target as HTMLSelectElement).value; includeSeconds.value = false; }

const taskDetails = ref<TaskDefinition>();
const runDetailsOpen = ref(false);
const selectedRun = ref<TaskRun>();
const runLogs = ref<TaskRunLog[]>([]);
const logsLoading = ref(false);
const logsError = ref('');
const editingId = ref('');
let tasksRequest = 0;
let runsRequest = 0;
let logsRequest = 0;
const mutationBusy = computed(() => saving.value || Boolean(actionId.value) || Boolean(batchAction.value));
const allSelected = computed(() => tasks.value.length > 0 && tasks.value.every((task) => selectedIds.value.includes(task.id)));
const selectedTasks = computed(() => tasks.value.filter((task) => selectedIds.value.includes(task.id)));
const taskPages = computed(() => Math.max(1, Math.ceil(taskPage.total / taskPage.pageSize)));
const runPages = computed(() => Math.max(1, Math.ceil(runPage.total / runPage.pageSize)));
const statuses = ['pending', 'running', 'succeeded', 'failed', 'dead_letter', 'cancelled', 'timeout', 'skipped'] as const;

const emptyForm = () => ({
  name: '', description: '', executorType: 'registered' as 'http' | 'registered',
  methodKey: '', timezone: 'Asia/Shanghai', cron: '', enabled: true,
  concurrency: 1, concurrencyPolicy: 'forbid' as 'allow' | 'forbid' | 'replace',
  timeoutSeconds: 30, maxAttempts: 3, payloadText: '{}', schemaText: '{"type":"object"}',
  url: '', method: 'GET', headersText: '{}', body: '', allowInternal: false,
});
const form = reactive(emptyForm());
watch(() => [form.cron, form.timezone], () => {
  clearTimeout(previewTimer);
  previewTimer = setTimeout(() => void loadPreview(), 350);
});
onBeforeUnmount(() => { clearTimeout(previewTimer); taskModal.value?.close(); });

function formatTime(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}
function nextExecution(item: TaskDefinition) {
  if (!item.enabled) return label('disabled');
  return item.nextRunAt ? formatTime(item.nextRunAt) : item.cron ? label('nextUnknown') : label('manual');
}
function taskDescription(item: TaskDefinition) {
  return item.description || label('noDescription');
}
function executorLabel(item: TaskDefinition) {
  return item.executorType === 'http' ? label('typeHttp') : item.executorType === 'registered' ? label('typeRegistered') : item.type;
}
function statusLabel(value: string) {
  return statuses.some((status) => status === value) ? label(`status_${value}`) : value;
}
function runStatus(value: { status?: string; errorCode?: string }) {
  return value.errorCode === 'concurrency.skipped' ? label('status_skipped') : value.errorCode === 'worker.timeout' ? label('status_timeout') : statusLabel(value.status || '');
}
function sourceLabel(value?: string) { return value === 'schedule' ? label('triggerSchedule') : value === 'manual' ? label('triggerManual') : value === 'retry' ? label('retry') : value || '—'; }
function fail(key: string, target = error) {
  target.value = label(key);
  notify('error', target.value);
}
async function loadTasks(reset = false) {
  if (reset) taskPage.page = 1;
  const request = ++tasksRequest;
  loading.value = true;
  error.value = '';
  const query: TaskListQuery = {
    page: taskPage.page, pageSize: taskPage.pageSize,
    name: taskFilter.name.trim() || undefined,
    executorType: (taskFilter.executorType || undefined) as TaskListQuery['executorType'],
    enabled: taskFilter.enabled === '' ? undefined : taskFilter.enabled === 'true',
  };
  try {
    const result = await listTasksApi(query);
    if (request !== tasksRequest) return;
    tasks.value = result.items;
    taskPage.total = result.total;
    selectedIds.value = selectedIds.value.filter((id) => result.items.some((task) => task.id === id));
    if (taskPage.page > taskPages.value) {
      taskPage.page = taskPages.value;
      await loadTasks();
    }
  } catch {
    if (request === tasksRequest) fail('loadError');
  } finally {
    if (request === tasksRequest) loading.value = false;
  }
}
async function loadRuns(reset = false) {
  if (reset) runPage.page = 1;
  const request = ++runsRequest;
  runsLoading.value = true;
  runsError.value = '';
  const query: TaskRunListQuery = {
    page: runPage.page, pageSize: runPage.pageSize,
    taskId: runFilter.taskId.trim() || undefined,
    taskName: runFilter.taskName.trim() || undefined,
    status: (runFilter.status || undefined) as TaskRunListQuery['status'],
    triggerSource: (runFilter.triggerSource || undefined) as TaskRunListQuery['triggerSource'],
    from: runFilter.from ? new Date(runFilter.from).toISOString() : undefined,
    to: runFilter.to ? new Date(runFilter.to).toISOString() : undefined,
  };
  if (query.from && query.to && query.from > query.to) {
    fail('invalidTimeRange', runsError);
    runsLoading.value = false;
    return;
  }
  try {
    const result = await listAllTaskRunsApi(query);
    if (request !== runsRequest) return;
    runs.value = result.items;
    runPage.total = result.total;
  } catch {
    if (request === runsRequest) fail('runsLoadError', runsError);
  } finally {
    if (request === runsRequest) runsLoading.value = false;
  }
}
function switchTab(tab: 'runs' | 'tasks') {
  activeTab.value = tab;
  if (tab === 'runs') void loadRuns();
}
function navigateTabs(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
  event.preventDefault();
  const tab = event.key === 'Home' ? 'tasks' : event.key === 'End' ? 'runs' : activeTab.value === 'tasks' ? 'runs' : 'tasks';
  switchTab(tab);
  document.querySelector<HTMLButtonElement>(`#${tab}-tab`)?.focus();
}
function resetFilters() {
  if (activeTab.value === 'tasks') {
    Object.assign(taskFilter, { name: '', executorType: '', enabled: '' });
    void loadTasks(true);
  } else {
    Object.assign(runFilter, { taskId: '', taskName: '', status: '', triggerSource: '', from: '', to: '' });
    void loadRuns(true);
  }
}
function toggleAll() {
  selectedIds.value = allSelected.value ? [] : tasks.value.map((task) => task.id);
}
function openForm(item?: TaskDefinition) {
  if (!canManage.value || mutationBusy.value) return;
  formError.value = '';
  editingId.value = item?.id ?? '';
  Object.assign(form, emptyForm());
  if (item) Object.assign(form, {
    name: item.name, description: item.description ?? '', executorType: item.executorType ?? (item.type === 'http' ? 'http' : 'registered'),
    methodKey: item.methodKey ?? '', timezone: item.timezone, cron: item.cron ?? '', enabled: item.enabled,
    concurrency: item.concurrency, concurrencyPolicy: item.concurrencyPolicy,
    timeoutSeconds: item.timeoutSeconds, maxAttempts: item.maxAttempts,
    payloadText: JSON.stringify(item.payload ?? {}, null, 2), schemaText: JSON.stringify(item.payloadSchema, null, 2),
    url: item.http?.url ?? '', method: item.http?.method ?? 'GET',
    headersText: JSON.stringify(item.http?.headers ?? {}, null, 2), body: typeof item.http?.body === 'string' ? item.http.body : item.http?.body ? JSON.stringify(item.http.body, null, 2) : '', allowInternal: item.http?.allowInternal ?? false,
  });
  includeSeconds.value = form.cron.trim().split(/\s+/).length === 6;
  taskDrawerOpen.value = true;
  void loadPreview();
}
function jsonObject(text: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(text);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('object required');
  return parsed as Record<string, unknown>;
}
function formInput(): TaskDefinitionInput {
  const headers = jsonObject(form.headersText);
  if (Object.values(headers).some((value) => typeof value !== 'string')) throw new Error('string headers required');
  return {
    name: form.name.trim(), description: form.description.trim(),
    type: form.executorType === 'http' ? 'http' : 'manual', executorType: form.executorType,
    methodKey: form.executorType === 'registered' ? form.methodKey.trim() : undefined,
    payload: jsonObject(form.payloadText), payloadSchema: jsonObject(form.schemaText),
    cron: form.cron.trim(), timezone: form.timezone.trim(), enabled: form.enabled,
    concurrency: form.concurrency, concurrencyPolicy: form.concurrencyPolicy,
    timeoutSeconds: form.timeoutSeconds, maxAttempts: form.maxAttempts,
    http: form.executorType === 'http' ? {
      url: form.url.trim(), method: form.method as NonNullable<TaskDefinitionInput['http']>['method'], headers: headers as Record<string, string>,
      body: form.body, allowInternal: form.allowInternal,
    } : undefined,
  };
}
async function saveTask() {
  if (!canManage.value || saving.value) return;
  if (mutationBusy.value) return;
  formError.value = '';
  let input: TaskDefinitionInput;
  try { input = formInput(); } catch { fail('payloadInvalid', formError); return; }
  if (!input.name) { fail('nameRequired', formError); return; }
  if (form.executorType === 'registered' && !form.methodKey.trim()) { fail('methodRequired', formError); return; }
  saving.value = true;
  try {
    if (editingId.value) await updateTaskApi(editingId.value, input);
    else await createTaskApi(input);
    taskDrawerOpen.value = false;
    notice.value = label('saved');
    notify('success', notice.value);
    await loadTasks();
  } catch { fail('saveError', formError); }
  finally { saving.value = false; }
}
function taskInput(item: TaskDefinition, enabled: boolean): TaskDefinitionInput {
  return {
    name: item.name, description: item.description, type: item.type,
    executorType: item.executorType, methodKey: item.methodKey, http: item.http,
    payload: item.payload, payloadSchema: item.payloadSchema, cron: item.cron,
    timezone: item.timezone, enabled, concurrency: item.concurrency,
    concurrencyPolicy: item.concurrencyPolicy, timeoutSeconds: item.timeoutSeconds,
    maxAttempts: item.maxAttempts, idempotencyKey: item.idempotencyKey,
  };
}
async function applyTaskAction(item: TaskDefinition, action: 'delete' | 'disable' | 'enable' | 'run') {
  if (action === 'delete') await deleteTaskApi(item.id);
  else if (action === 'run') { const run = await runTaskApi(item.id, { confirm: true }); if (run.errorCode === 'concurrency.skipped') return 'skipped'; }
  else await updateTaskApi(item.id, taskInput(item, action === 'enable'));
}
async function taskAction(item: TaskDefinition, action: 'delete' | 'disable' | 'enable' | 'run') {
  if (!canManage.value || mutationBusy.value) return;
  if ((action === 'delete' || action === 'run') && !window.confirm(label(action === 'delete' ? 'deleteConfirm' : 'runConfirm'))) return;
  actionId.value = item.id;
  error.value = '';
  try {
    const outcome = await applyTaskAction(item, action);
    notice.value = outcome === 'skipped' ? label('runSkipped') : label(action === 'run' ? 'runAccepted' : action === 'delete' ? 'deleted' : 'saved');
    notify('success', notice.value);
    await loadTasks();
  } catch { fail(action === 'run' ? 'runError' : action === 'delete' ? 'deleteError' : 'saveError'); }
  finally { actionId.value = ''; }
}
async function applyBatch(action: 'delete' | 'disable' | 'enable') {
  if (!canManage.value || mutationBusy.value || selectedTasks.value.length === 0) return;
  if (!window.confirm(`${label('batchConfirm')} ${selectedTasks.value.length}`)) return;
  batchAction.value = action;
  const snapshot = [...selectedTasks.value];
  const failedIds: string[] = [];
  for (const task of snapshot) {
    try { await applyTaskAction(task, action); } catch { failedIds.push(task.id); }
  }
  selectedIds.value = failedIds;
  notice.value = `${label('batchSucceeded')}: ${snapshot.length - failedIds.length} · ${label('batchFailed')}: ${failedIds.length}`;
  notify(failedIds.length ? 'warning' : 'success', notice.value);
  await loadTasks();
  batchAction.value = '';
}
function showTaskRuns(item: TaskDefinition) {
  runFilter.taskId = item.id;
  runPage.page = 1;
  taskDetails.value = undefined;
  switchTab('runs');
}
async function openRunDetails(run: TaskRun) {
  selectedRun.value = run;
  runDetailsOpen.value = true;
  runLogs.value = [];
  logsError.value = '';
  logsLoading.value = true;
  const request = ++logsRequest;
  try {
    const result = await listTaskRunLogsApi(run.taskId, run.id);
    if (request === logsRequest) runLogs.value = result;
  } catch { if (request === logsRequest) fail('logsLoadError', logsError); }
  finally { if (request === logsRequest) logsLoading.value = false; }
}
async function runAction(run: TaskRun, action: 'cancel' | 'retry') {
  if (!canManage.value || mutationBusy.value) return;
  if (!window.confirm(label(action === 'cancel' ? 'cancelRunConfirm' : 'retryRunConfirm'))) return;
  actionId.value = run.id;
  try {
    if (action === 'cancel') await cancelTaskRunApi(run.taskId, run.id);
    else await retryTaskRunApi(run.taskId, run.id);
    notify('success', label(action === 'cancel' ? 'runCancelled' : 'retryAccepted'));
    await loadRuns();
  } catch { fail(action === 'cancel' ? 'cancelRunError' : 'retryError', runsError); }
  finally { actionId.value = ''; }
}
function changePage(kind: 'runs' | 'tasks', step: number) {
  if (kind === 'tasks') { taskPage.page += step; void loadTasks(); }
  else { runPage.page += step; void loadRuns(); }
}
onMounted(() => void loadTasks());
</script>

<template>
  <ManagementPage class="tasks-page" aria-labelledby="tasks-title" :busy="loading || runsLoading || mutationBusy">
    <header class="page-heading">
      <div><h1 id="tasks-title">{{ label('title') }}</h1><p class="description">{{ label('description') }}</p></div>
      <div class="toolbar">
        <button class="secondary" type="button" :disabled="loading || runsLoading" @click="activeTab === 'tasks' ? loadTasks() : loadRuns()">{{ label('refresh') }}</button>
        <button v-if="canManage" class="primary" type="button" :disabled="mutationBusy" @click="openForm()">{{ label('newTask') }}</button>
      </div>
    </header>
    <div class="workspace-tabs" role="tablist" :aria-label="label('title')" @keydown="navigateTabs">
      <button id="tasks-tab" type="button" role="tab" :aria-selected="activeTab === 'tasks'" :tabindex="activeTab === 'tasks' ? 0 : -1" aria-controls="tasks-panel" @click="switchTab('tasks')">{{ label('listTitle') }}</button>
      <button id="runs-tab" type="button" role="tab" :aria-selected="activeTab === 'runs'" :tabindex="activeTab === 'runs' ? 0 : -1" aria-controls="runs-panel" @click="switchTab('runs')">{{ label('runsTitle') }}</button>
    </div>
    <p v-if="notice" class="feedback success" role="status">{{ notice }}</p>
    <section v-show="activeTab === 'tasks'" id="tasks-panel" class="panel" role="tabpanel" aria-labelledby="tasks-tab">
      <form class="filters" @submit.prevent="loadTasks(true)">
        <label><span>{{ label('name') }}</span><input v-model="taskFilter.name" :placeholder="label('searchName')" /></label>
        <label><span>{{ label('executor') }}</span><select v-model="taskFilter.executorType"><option value="">{{ label('all') }}</option><option value="registered">{{ label('typeRegistered') }}</option><option value="http">{{ label('typeHttp') }}</option></select></label>
        <label><span>{{ label('status') }}</span><select v-model="taskFilter.enabled"><option value="">{{ label('all') }}</option><option value="true">{{ label('enabled') }}</option><option value="false">{{ label('disabled') }}</option></select></label>
        <div class="toolbar"><button class="primary" :disabled="loading" type="submit">{{ label('search') }}</button><button class="secondary" :disabled="loading" type="button" @click="resetFilters">{{ label('reset') }}</button></div>
      </form>
      <div v-if="canManage" class="batch-toolbar">
        <span>{{ label('selected') }} {{ selectedIds.length }}</span>
        <button type="button" :disabled="mutationBusy || !selectedIds.length" @click="applyBatch('enable')">{{ label('batchEnable') }}</button>
        <button type="button" :disabled="mutationBusy || !selectedIds.length" @click="applyBatch('disable')">{{ label('batchDisable') }}</button>
        <button class="danger" type="button" :disabled="mutationBusy || !selectedIds.length" @click="applyBatch('delete')">{{ label('batchDelete') }}</button>
      </div>
      <p v-if="error" class="feedback error" role="alert">{{ error }} <button type="button" @click="loadTasks()">{{ label('retry') }}</button></p>
      <div class="table-scroll" :aria-busy="loading">
        <table><caption class="sr-only">{{ label('tableLabel') }}</caption><thead><tr>
          <th v-if="canManage" scope="col"><input type="checkbox" :checked="allSelected" :disabled="mutationBusy || !tasks.length" :aria-label="label('selectAll')" @change="toggleAll" /></th>
          <th scope="col">{{ label('taskInfo') }}</th><th scope="col">{{ label('executionTarget') }}</th><th scope="col">{{ label('cron') }}</th><th scope="col">{{ label('executor') }}</th><th scope="col">{{ label('status') }}</th><th scope="col">{{ label('nextExecution') }}</th><th scope="col">{{ label('lastExecution') }}</th><th scope="col">{{ label('actions') }}</th>
        </tr></thead><tbody>
          <tr v-if="loading"><td colspan="9" class="table-state" role="status">{{ label('loading') }}</td></tr>
          <tr v-else-if="!tasks.length"><td colspan="9" class="table-state">{{ error ? label('loadError') : label('empty') }}</td></tr>
          <tr v-for="item in tasks" v-else :key="item.id">
            <td v-if="canManage"><input v-model="selectedIds" type="checkbox" :value="item.id" :disabled="mutationBusy" :aria-label="`${label('selectTask')} ${item.name}`" /></td>
            <th scope="row"><button class="link-button" type="button" @click="taskDetails = item">{{ item.name }}</button><small class="description-cell">{{ taskDescription(item) }}</small></th>
            <td class="target-cell" :title="item.http?.url || item.methodKey">{{ item.methodKey || item.http?.url || '—' }}</td><td><code>{{ item.cron || label('manual') }}</code><small>{{ item.timezone }}</small></td>
            <td>{{ executorLabel(item) }}<small>{{ item.methodKey || item.http?.method || '—' }}</small></td>
            <td><span class="status-pill" :class="item.enabled ? 'ok' : 'off'">{{ item.enabled ? label('enabled') : label('disabled') }}</span></td>
            <td class="nowrap">{{ nextExecution(item) }}</td><td class="nowrap"><template v-if="item.lastRunAt"><span>{{ runStatus({ status:item.lastRunStatus, errorCode:item.lastRunErrorCode }) }}</span><small>{{ formatTime(item.lastRunAt) }}</small></template><span v-else>—</span></td>
            <td><div class="actions"><button type="button" @click="showTaskRuns(item)">{{ label('logs') }}</button><template v-if="canManage"><button type="button" :disabled="mutationBusy" @click="openForm(item)">{{ label('edit') }}</button><button type="button" :disabled="mutationBusy" @click="taskAction(item, 'run')">{{ actionId === item.id ? label('running') : label('run') }}</button><button type="button" :disabled="mutationBusy" @click="taskAction(item, item.enabled ? 'disable' : 'enable')">{{ item.enabled ? label('disable') : label('enable') }}</button><button class="danger" type="button" :disabled="mutationBusy" @click="taskAction(item, 'delete')">{{ label('delete') }}</button></template></div></td>
          </tr>
        </tbody></table>
      </div>
      <footer class="pagination"><span>{{ label('total') }} {{ taskPage.total }}</span><label>{{ label('pageSize') }} <select v-model.number="taskPage.pageSize" :disabled="loading" @change="loadTasks(true)"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option></select></label><div class="toolbar"><button type="button" :disabled="loading || taskPage.page <= 1" @click="changePage('tasks', -1)">{{ label('previousPage') }}</button><span>{{ taskPage.page }} / {{ taskPages }}</span><button type="button" :disabled="loading || taskPage.page >= taskPages" @click="changePage('tasks', 1)">{{ label('nextPage') }}</button></div></footer>
    </section>

    <p class="info-banner">{{ label('retentionHint') }}</p>
    <section v-show="activeTab === 'runs'" id="runs-panel" class="panel" role="tabpanel" aria-labelledby="runs-tab">
      <form class="filters" @submit.prevent="loadRuns(true)">
        <label><span>{{ label('name') }}</span><input v-model="runFilter.taskName" :placeholder="label('searchName')" @input="runFilter.taskId = ''" /></label>
        <label><span>{{ label('status') }}</span><select v-model="runFilter.status"><option value="">{{ label('all') }}</option><option v-for="status in statuses" :key="status" :value="status">{{ statusLabel(status) }}</option></select></label>
        <label><span>{{ label('triggerSource') }}</span><select v-model="runFilter.triggerSource"><option value="">{{ label('all') }}</option><option value="manual">{{ label('triggerManual') }}</option><option value="schedule">{{ label('triggerSchedule') }}</option><option value="retry">{{ label('retry') }}</option></select></label>
        <label><span>{{ label('from') }}</span><input v-model="runFilter.from" type="datetime-local" /></label><label><span>{{ label('to') }}</span><input v-model="runFilter.to" type="datetime-local" /></label>
        <div class="toolbar"><button class="primary" :disabled="runsLoading" type="submit">{{ label('search') }}</button><button class="secondary" :disabled="runsLoading" type="button" @click="resetFilters">{{ label('reset') }}</button></div>
      </form>
      <p v-if="runsError" class="feedback error" role="alert">{{ runsError }} <button type="button" @click="loadRuns()">{{ label('retry') }}</button></p>
      <div class="table-scroll" :aria-busy="runsLoading"><table><caption class="sr-only">{{ label('runsTitle') }}</caption><thead><tr><th scope="col">{{ label('runId') }}</th><th scope="col">{{ label('taskInfo') }}</th><th scope="col">{{ label('triggerSource') }}</th><th scope="col">{{ label('executor') }}</th><th scope="col">{{ label('status') }}</th><th scope="col">{{ label('startedAt') }}</th><th scope="col">{{ label('duration') }}</th><th scope="col">{{ label('resultSummary') }}</th><th scope="col">{{ label('actions') }}</th></tr></thead><tbody>
        <tr v-if="runsLoading"><td colspan="9" class="table-state" role="status">{{ label('loading') }}</td></tr><tr v-else-if="!runs.length"><td colspan="9" class="table-state">{{ runsError ? label('runsLoadError') : label('runsEmpty') }}</td></tr>
        <tr v-for="run in runs" v-else :key="run.id"><th scope="row"><button class="link-button identifier" type="button" @click="openRunDetails(run)">{{ run.id }}</button><small>{{ run.attemptCount }} / {{ run.maxAttempts }}</small></th><td><strong>{{ run.taskName || run.taskId }}</strong><small>{{ run.taskDescription }}</small></td><td>{{ sourceLabel(run.triggerSource) }}</td><td>{{ run.executorType === 'http' ? label('typeHttp') : label('typeRegistered') }}</td><td><span class="status-pill" :class="run.status === 'succeeded' ? 'ok' : run.status === 'failed' || run.status === 'dead_letter' ? 'failed' : 'off'">{{ runStatus(run) }}</span></td><td class="nowrap">{{ formatTime(run.startedAt || run.createdAt) }}</td><td>{{ run.durationMs == null ? '—' : `${run.durationMs} ms` }}</td><td class="description-cell">{{ run.resultSummary || '—' }}</td><td><div class="actions"><button type="button" @click="openRunDetails(run)">{{ label('details') }}</button><button v-if="canManage && ['pending', 'running', 'failed'].includes(run.status)" type="button" :disabled="mutationBusy" @click="runAction(run, 'cancel')">{{ label('cancelRun') }}</button><button v-if="canManage && ['failed', 'dead_letter'].includes(run.status)" type="button" :disabled="mutationBusy" @click="runAction(run, 'retry')">{{ label('retry') }}</button></div></td></tr>
      </tbody></table></div>
      <footer class="pagination"><span>{{ label('total') }} {{ runPage.total }}</span><label>{{ label('pageSize') }} <select v-model.number="runPage.pageSize" :disabled="runsLoading" @change="loadRuns(true)"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option></select></label><div class="toolbar"><button type="button" :disabled="runsLoading || runPage.page <= 1" @click="changePage('runs', -1)">{{ label('previousPage') }}</button><span>{{ runPage.page }} / {{ runPages }}</span><button type="button" :disabled="runsLoading || runPage.page >= runPages" @click="changePage('runs', 1)">{{ label('nextPage') }}</button></div></footer>
    </section>

    <dialog ref="taskModal" class="task-modal" aria-labelledby="task-modal-title" :aria-busy="saving" @cancel="saving ? $event.preventDefault() : taskDrawerOpen = false" @close="taskDrawerOpen = false">
      <header class="modal-header"><h2 id="task-modal-title">{{ editingId ? label('editTitle') : label('newTitle') }}</h2><button type="button" :disabled="saving" :aria-label="label('cancel')" @click="taskDrawerOpen = false">×</button></header>
      <form v-if="canManage" class="task-form" @submit.prevent="saveTask">
        <p v-if="formError" class="feedback error wide" role="alert">{{ formError }}</p>
        <label><span>{{ label('name') }}</span><input v-model="form.name" required maxlength="160" :disabled="saving" /></label>
        <label><span>{{ label('executor') }}</span><select v-model="form.executorType" :disabled="saving"><option value="registered">{{ label('typeRegistered') }}</option><option value="http">{{ label('typeHttp') }}</option></select></label>
        <label class="wide"><span>{{ label('descriptionField') }}</span><textarea v-model="form.description" rows="2" maxlength="2000" :disabled="saving"></textarea></label>
        <label v-if="form.executorType === 'registered'" class="wide"><span>{{ label('methodKey') }}</span><select v-model="form.methodKey" required :disabled="saving"><option value="">{{ label('chooseMethod') }}</option><option v-for="key in methods" :key="key" :value="key">{{ key }}</option></select><span v-if="methodsError" role="alert">{{ methodsError }} <button type="button" @click="loadMethods">{{ label('retry') }}</button></span><small>{{ label('methodHint') }}</small></label>
        <template v-if="form.executorType === 'http'">
          <label><span>{{ label('httpMethod') }}</span><select v-model="form.method" :disabled="saving"><option v-for="method in ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']" :key="method">{{ method }}</option></select></label>
          <label><span>{{ label('httpUrl') }}</span><input v-model="form.url" type="text" required pattern="https?://.*|\[REDACTED\]" :disabled="saving" /></label>
          <label class="wide"><span>{{ label('httpHeaders') }}</span><textarea v-model="form.headersText" rows="3" :disabled="saving"></textarea><small>{{ label('headersHint') }}</small></label>
          <label class="wide"><span>{{ label('httpBody') }}</span><textarea v-model="form.body" rows="4" :disabled="saving"></textarea></label>
          <label class="toggle wide"><input v-model="form.allowInternal" type="checkbox" :disabled="saving" /><span>{{ label('allowInternal') }}</span><small>{{ label('allowInternalHint') }}</small></label>
        </template>
        <label><span>{{ label('cron') }}</span><input v-model="form.cron" :disabled="saving" /><select :disabled="saving" aria-label="Cron templates" @change="applyCronTemplate"><option value="">{{ label('cronTemplates') }}</option><option value="@hourly">{{ label('hourly') }}</option><option value="@daily">{{ label('daily') }}</option><option value="*/30 * * * *">{{ label('everyThirtyMinutes') }}</option></select><small>{{ label('cronHint') }}</small></label>
        <label><span>{{ label('timezone') }}</span><input v-model="form.timezone" required :disabled="saving" /><small>{{ label('timezoneHint') }}</small></label>
        <label class="toggle wide"><input v-model="includeSeconds" type="checkbox" :disabled="saving" @change="toggleSeconds" /><span>{{ label('includeSeconds') }}</span></label>
        <aside class="cron-preview wide"><h3>{{ label('previewTitle') }}</h3><p v-if="previewLoading" role="status">{{ label('loading') }}</p><p v-else-if="previewError" role="alert">{{ previewError }}</p><ol v-else-if="previewDates.length"><li v-for="date in previewDates" :key="date">{{ formatTime(date) }}</li></ol><p v-else>{{ label('previewEmpty') }}</p><small>{{ label('previewHint') }}</small></aside>
        <label><span>{{ label('concurrency') }}</span><input v-model.number="form.concurrency" type="number" min="1" max="100" required :disabled="saving" /></label>
        <label><span>{{ label('policy') }}</span><select v-model="form.concurrencyPolicy" :disabled="saving"><option value="forbid">{{ label('policyForbid') }}</option><option value="allow">{{ label('policyAllow') }}</option><option value="replace">{{ label('policyReplace') }}</option></select></label>
        <label><span>{{ label('timeout') }}</span><input v-model.number="form.timeoutSeconds" type="number" min="1" max="3600" required :disabled="saving" /></label>
        <label><span>{{ label('maxAttempts') }}</span><input v-model.number="form.maxAttempts" type="number" min="1" max="10" required :disabled="saving" /></label>
        <label class="wide"><span>{{ label('payload') }}</span><textarea v-model="form.payloadText" rows="4" required :disabled="saving"></textarea></label>
        <details class="wide"><summary>{{ label('payloadSchema') }}</summary><label><span class="sr-only">{{ label('payloadSchema') }}</span><textarea v-model="form.schemaText" rows="4" :disabled="saving"></textarea></label></details>
        <label class="toggle wide"><input v-model="form.enabled" type="checkbox" :disabled="saving" /><span>{{ label('enabled') }}</span></label>
        <div class="form-actions wide"><button class="primary" type="submit" :disabled="saving">{{ saving ? label('saving') : label('save') }}</button><button class="secondary" type="button" :disabled="saving" @click="taskDrawerOpen = false">{{ label('cancel') }}</button></div>
      </form>
    </dialog>
    <ManagementDrawer :open="Boolean(taskDetails)" :title="label('taskDetails')" wide @close="taskDetails = undefined">
      <template v-if="taskDetails"><dl class="detail-grid"><dt>{{ label('name') }}</dt><dd>{{ taskDetails.name }}</dd><dt>{{ label('id') }}</dt><dd>{{ taskDetails.id }}</dd><dt>{{ label('descriptionField') }}</dt><dd>{{ taskDescription(taskDetails) }}</dd><dt>{{ label('executor') }}</dt><dd>{{ executorLabel(taskDetails) }} · {{ taskDetails.methodKey || taskDetails.http?.method || '—' }}</dd><dt>{{ label('cron') }}</dt><dd>{{ taskDetails.cron || label('manual') }} · {{ taskDetails.timezone }}</dd><dt>{{ label('nextExecution') }}</dt><dd>{{ nextExecution(taskDetails) }}</dd><dt>{{ label('status') }}</dt><dd>{{ taskDetails.enabled ? label('enabled') : label('disabled') }}</dd><dt>{{ label('timeout') }}</dt><dd>{{ taskDetails.timeoutSeconds }}</dd></dl><button class="primary" type="button" @click="showTaskRuns(taskDetails)">{{ label('logs') }}</button></template>
    </ManagementDrawer>
    <ManagementDrawer :open="runDetailsOpen" :title="label('runDetails')" :busy="logsLoading" wide @close="runDetailsOpen = false">
      <template v-if="selectedRun"><dl class="detail-grid"><dt>{{ label('runId') }}</dt><dd>{{ selectedRun.id }}</dd><dt>{{ label('taskId') }}</dt><dd>{{ selectedRun.taskId }}</dd><dt>{{ label('triggerSource') }}</dt><dd>{{ sourceLabel(selectedRun.triggerSource) }}</dd><dt>{{ label('executor') }}</dt><dd>{{ selectedRun.executorType || '—' }}</dd><dt>{{ label('status') }}</dt><dd>{{ runStatus(selectedRun) }}</dd><dt>{{ label('startedAt') }}</dt><dd>{{ formatTime(selectedRun.startedAt) }}</dd><dt>{{ label('finishedAt') }}</dt><dd>{{ formatTime(selectedRun.finishedAt) }}</dd><dt>{{ label('duration') }}</dt><dd>{{ selectedRun.durationMs == null ? '—' : `${selectedRun.durationMs} ms` }}</dd><dt>{{ label('resultSummary') }}</dt><dd>{{ selectedRun.resultSummary || '—' }}</dd><dt>{{ label('errorCode') }}</dt><dd>{{ selectedRun.errorCode || selectedRun.lastErrorCode || label('noError') }}</dd></dl>
        <h3>{{ label('configSnapshot') }}</h3><pre class="output">{{ selectedRun.configSnapshot ? JSON.stringify(selectedRun.configSnapshot, null, 2) : '—' }}</pre>
        <h3>{{ label('redactedOutput') }}</h3><pre class="output">{{ selectedRun.redactedOutput || '—' }}</pre>
        <div class="section-heading"><h3>{{ label('attemptLogs') }}</h3><button class="secondary" type="button" :disabled="logsLoading" @click="openRunDetails(selectedRun)">{{ label('refresh') }}</button></div>
        <p v-if="logsLoading" role="status">{{ label('loading') }}</p><p v-else-if="logsError" class="feedback error" role="alert">{{ logsError }}</p><p v-else-if="!runLogs.length" class="empty-state">{{ label('logsEmpty') }}</p>
        <ol v-else class="attempt-logs"><li v-for="entry in runLogs" :key="entry.id"><div class="section-heading"><strong>#{{ entry.attempt }} · {{ statusLabel(entry.status) }}</strong><time>{{ formatTime(entry.createdAt) }}</time></div><p>{{ entry.errorCode || label('noError') }} · {{ entry.message || entry.resultSummary || '—' }}</p><pre v-if="entry.redactedOutput" class="output">{{ entry.redactedOutput }}</pre></li></ol>
      </template>
    </ManagementDrawer>
  </ManagementPage>
</template>

<style scoped>
.tasks-page { color: hsl(var(--foreground)); }
.page-heading, .section-heading, .pagination { display: flex; gap: 16px; align-items: center; justify-content: space-between; }
.page-heading { align-items: flex-start; }
h1 { margin: 0 0 6px; font-size: clamp(1.4rem, 3vw, 1.8rem); }
h3 { margin: 14px 0; font-size: 1rem; }
.description, small, .empty-state { color: hsl(var(--muted-foreground)); }
.description { margin: 0; }
.toolbar, .actions, .form-actions, .batch-toolbar { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
button, input, select, textarea { font: inherit; }
button { padding: 6px 12px; cursor: pointer; background: hsl(var(--background)); border: 1px solid hsl(var(--border)); border-radius: 6px; }
button:disabled { cursor: not-allowed; opacity: 0.5; }
button:not(:disabled):hover { background: hsl(var(--muted)); }
button:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, summary:focus-visible { outline: 2px solid hsl(var(--primary)); outline-offset: 3px; }
button.primary { color: hsl(var(--primary-foreground)); background: hsl(var(--primary)); border-color: hsl(var(--primary)); }
button.danger { color: #b42318; }
.workspace-tabs { display: flex; gap: 24px; margin-top: 24px; border-bottom: 1px solid hsl(var(--border)); }
.workspace-tabs button { padding: 12px 4px; color: hsl(var(--muted-foreground)); border: 0; border-bottom: 2px solid transparent; border-radius: 0; }
.workspace-tabs button[aria-selected='true'] { font-weight: 600; color: hsl(var(--primary)); border-color: hsl(var(--primary)); }
.panel { min-width: 0; padding: 20px 0; }
.filters { display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-end; padding: 16px; margin-bottom: 16px; background: hsl(var(--muted) / 35%); border: 1px solid hsl(var(--border)); border-radius: 8px; }
.filters label, .task-form label { display: grid; gap: 6px; font-size: 0.85rem; }
.filters input { width: 190px; }
input:not([type='checkbox']), select, textarea { min-height: 36px; padding: 7px 10px; color: hsl(var(--foreground)); background: hsl(var(--background)); border: 1px solid hsl(var(--border)); border-radius: 6px; }
input[type='checkbox'] { width: 16px; height: 16px; accent-color: hsl(var(--primary)); }
.batch-toolbar { margin-bottom: 14px; font-size: 0.85rem; }
.batch-toolbar > span { margin-right: auto; color: hsl(var(--muted-foreground)); }
.table-scroll { overflow-x: auto; border: 1px solid hsl(var(--border)); border-radius: 8px; }
table { width: 100%; min-width: 1000px; font-size: 0.85rem; border-collapse: collapse; }
th, td { padding: 12px; text-align: left; vertical-align: middle; border-bottom: 1px solid hsl(var(--border)); }
thead { background: hsl(var(--muted) / 50%); }
thead th { font-size: 0.78rem; font-weight: 600; color: hsl(var(--muted-foreground)); white-space: nowrap; }
tbody tr:last-child > * { border-bottom: 0; }
tbody th { font-weight: 500; }
small { display: block; margin-top: 4px; font-size: 0.74rem; }
.description-cell { min-width: 140px; max-width: 220px; overflow-wrap: anywhere; }
.identifier { max-width: 180px; font-family: ui-monospace, monospace; font-size: 0.75rem; overflow-wrap: anywhere; }
.nowrap { white-space: nowrap; }
.link-button { padding: 0; color: hsl(var(--primary)); text-align: left; background: transparent; border: 0; }
.actions { min-width: 245px; gap: 4px; }
.target-cell { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.info-banner { padding: 12px 16px; color: hsl(var(--primary)); background: hsl(var(--primary) / 5%); border: 1px solid hsl(var(--primary) / 25%); border-radius: 8px; font-size: .8rem; }
.actions button { padding: 4px 7px; font-size: 0.78rem; }
.status-pill { display: inline-flex; padding: 3px 8px; font-size: 0.75rem; white-space: nowrap; border-radius: 20px; }
.status-pill.ok { color: #166534; background: #dcfce7; }
.status-pill.off { color: #92400e; background: #fef3c7; }
.status-pill.failed { color: #991b1b; background: #fee2e2; }
.table-state { padding: 42px 16px; color: hsl(var(--muted-foreground)); text-align: center; }
.pagination { flex-wrap: wrap; justify-content: flex-end; margin-top: 16px; font-size: 0.85rem; }
.pagination > span { margin-right: auto; }
.pagination .toolbar { flex-wrap: nowrap; }
.feedback { padding: 10px 14px; margin: 14px 0; border-radius: 6px; }
.feedback.error { color: #991b1b; background: #fef2f2; }
.feedback.success { color: #166534; background: #f0fdf4; }
.task-modal { width: min(960px, calc(100vw - 32px)); max-height: 90vh; padding: 0; color: hsl(var(--foreground)); background: hsl(var(--background)); border: 1px solid hsl(var(--border)); border-radius: 10px; box-shadow: 0 24px 70px #0003; }
.task-modal::backdrop { background: #0f172a66; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 18px 24px; border-bottom: 1px solid hsl(var(--border)); }
.modal-header h2 { margin: 0; font-size: 1.2rem; font-weight: 600; }
.task-modal .task-form { padding: 24px; }
.cron-preview { padding: 12px 16px; color: hsl(var(--primary)); background: hsl(var(--primary) / 6%); border: 1px solid hsl(var(--primary) / 15%); border-radius: 8px; }
.cron-preview ol { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 8px; padding-left: 22px; font-size: 0.85rem; }
.task-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.task-form .wide { grid-column: 1 / -1; }
.task-form textarea { width: 100%; font-family: ui-monospace, monospace; resize: vertical; }
.task-form .toggle { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.toggle small { flex-basis: 100%; margin-top: 0; }
.form-actions { position: sticky; bottom: 0; padding: 14px 0; background: hsl(var(--background)); border-top: 1px solid hsl(var(--border)); }
.detail-grid { display: grid; grid-template-columns: 120px minmax(0, 1fr); gap: 14px; }
.detail-grid dt { color: hsl(var(--muted-foreground)); }
.detail-grid dd { margin: 0; overflow-wrap: anywhere; }
.output { max-height: 280px; padding: 12px; overflow: auto; font-size: 0.8rem; white-space: pre-wrap; background: hsl(var(--muted) / 50%); border: 1px solid hsl(var(--border)); border-radius: 6px; }
.attempt-logs { padding-left: 24px; }
.attempt-logs li { padding: 12px 0; border-bottom: 1px solid hsl(var(--border)); }
.attempt-logs time { font-size: 0.75rem; color: hsl(var(--muted-foreground)); }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; white-space: nowrap; clip-path: inset(50%); }
@media (width <= 640px) { .page-heading { flex-direction: column; } .filters label, .filters input { width: 100%; } .filters .toolbar { width: 100%; } .task-form { grid-template-columns: 1fr; } .detail-grid { grid-template-columns: 90px minmax(0, 1fr); } .pagination { gap: 10px; } }
</style>
