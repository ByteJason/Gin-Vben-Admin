import type {
  TaskDefinition,
  TaskDefinitionInput,
  TaskRun,
} from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type { TaskDefinition, TaskDefinitionInput, TaskRun } from '@vben/api-client';

const replace = (template: string, key: string, value: string) =>
  template.replace(`{${key}}`, encodeURIComponent(value));

export function listTasksApi() {
  return requestClient.get<TaskDefinition[]>(ADMIN_ENDPOINTS.listTasks);
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

export function runTaskApi(id: string, input: { confirm: true; idempotencyKey?: string }) {
  return requestClient.post<{ taskId: string; status: string }>(
    replace(ADMIN_ENDPOINTS.runTask, 'id', id),
    input,
  );
}

export function listTaskRunsApi(id: string) {
  return requestClient.get<TaskRun[]>(replace(ADMIN_ENDPOINTS.listTaskRuns, 'id', id));
}
