import type {
  TaskDefinition,
  TaskDefinitionInput,
  TaskDefinitionPage,
  TaskRun,
  TaskRunLog,
  TaskRunPage,
  TaskListQuery,
  TaskRunListQuery,
} from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type {
  TaskDefinition,
  TaskDefinitionInput,
  TaskRun,
  TaskRunLog,
  TaskListQuery,
  TaskRunListQuery,
} from '@vben/api-client';

const replace = (template: string, key: string, value: string) =>
  template.replace(`{${key}}`, encodeURIComponent(value));

export function listTasksApi(query: TaskListQuery = { page: 1, pageSize: 20 }) {
  return requestClient.get<TaskDefinitionPage>(ADMIN_ENDPOINTS.listTasks, { params: query });
}

export function createTaskApi(input: TaskDefinitionInput) {
  return requestClient.post<TaskDefinition>(ADMIN_ENDPOINTS.createTask, input);
}

export function updateTaskApi(id: string, input: TaskDefinitionInput) {
  return requestClient.request<TaskDefinition>(replace(ADMIN_ENDPOINTS.updateTask, 'id', id), {
    data: input,
    method: 'PATCH',
  });
}

export function deleteTaskApi(id: string) {
  return requestClient.delete<void>(replace(ADMIN_ENDPOINTS.deleteTask, 'id', id));
}

export function runTaskApi(id: string, input: { confirm: true; payload?: Record<string, unknown>; idempotencyKey?: string }) {
  return requestClient.post<TaskRun>(replace(ADMIN_ENDPOINTS.runTask, 'id', id), input);
}

export function listAllTaskRunsApi(query: TaskRunListQuery) {
  return requestClient.get<TaskRunPage>(ADMIN_ENDPOINTS.listAllTaskRuns, { params: query });
}

export function listTaskRunsApi(id: string) {
  return requestClient.get<TaskRunPage>(replace(ADMIN_ENDPOINTS.listTaskRuns, 'id', id));
}

export function listTaskRunLogsApi(taskId: string, runId: string) {
  return requestClient.get<TaskRunLog[]>(
    replace(replace(ADMIN_ENDPOINTS.listTaskRunLogs, 'id', taskId), 'runId', runId),
  );
}

export function cancelTaskRunApi(taskId: string, runId: string) {
  return requestClient.post<TaskRun>(
    replace(replace(ADMIN_ENDPOINTS.cancelTaskRun, 'id', taskId), 'runId', runId),
  );
}

export function retryTaskRunApi(taskId: string, runId: string) {
  return requestClient.post<TaskRun>(
    replace(replace(ADMIN_ENDPOINTS.retryTaskRun, 'id', taskId), 'runId', runId),
  );
}

export function listTaskMethodsApi() { return requestClient.get<string[]>(ADMIN_ENDPOINTS.listTaskMethods); }
export function previewTaskScheduleApi(cron: string, timezone: string) { return requestClient.get<string[]>(ADMIN_ENDPOINTS.previewTaskSchedule, { params: { cron, timezone } }); }
