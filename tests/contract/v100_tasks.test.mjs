import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');

test('B1.3 task contract exposes persisted definitions and execution seams', () => {
  for (const path of [
    'server/internal/application/tasks/service.go',
    'server/internal/application/tasks/scheduler.go',
    'server/internal/application/tasks/runs.go',
    'server/internal/application/jobs/worker.go',
    'server/internal/platform/jobs/redis_queue.go',
    'server/internal/transport/http/tasks/handler.go',
    'server/migrations/schema.go',
    'server/internal/platform/persistence/model/admin_tasks_models.go',
  ]) {
    assert.equal(existsSync(new URL(path, root)), true, `missing ${path}`);
  }
  const service = read('server/internal/application/tasks/service.go');
  const domain = read('server/internal/domain/task/task.go');
  const taskSurface = `${service}\n${domain}`;
  for (const token of ['TaskDefinition', 'cron', 'timezone', 'idempotency', 'MaxAttempts', 'PayloadSchema']) {
    assert.match(taskSurface, new RegExp(token, 'i'), `task surface missing ${token}`);
  }
  const execution = `${read('server/internal/application/tasks/runs.go')}\n${read('server/internal/application/tasks/scheduler.go')}\n${read('server/internal/platform/jobs/redis_queue.go')}`;
  for (const token of ['RunService', 'RunFailed', 'RunDeadLetter', 'Cancel', 'Retry', 'AppendLog', 'Scheduler', 'RedisQueue', 'NewAsynqQueue']) {
    assert.match(execution, new RegExp(token), `execution seam missing ${token}`);
  }
  const handler = read('server/internal/transport/http/tasks/handler.go');
  for (const token of ['/api/admin/v1/tasks', 'manual', 'run', 'tenant', 'MessageKey']) {
    assert.match(handler, new RegExp(token, 'i'), `handler missing ${token}`);
  }
  const openapi = read('contracts/openapi/admin-v1.yaml');
  for (const path of [
    '/api/admin/v1/tasks:',
    '/api/admin/v1/tasks/{id}:',
    '/api/admin/v1/tasks/{id}/run:',
    '/api/admin/v1/tasks/{id}/runs:',
    '/api/admin/v1/tasks/{id}/runs/{runId}/logs:',
    '/api/admin/v1/tasks/{id}/runs/{runId}/cancel:',
    '/api/admin/v1/tasks/{id}/runs/{runId}/retry:',
  ]) {
    assert.match(openapi, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
  }
  assert.match(openapi, /TaskDefinition/);
  assert.match(openapi, /TaskRun/);
  assert.match(openapi, /TaskRunLog/);
  assert.match(openapi, /payloadSchema/);
  assert.match(openapi, /writeOnly: true/);
  const model = read('server/internal/platform/persistence/model/admin_tasks_models.go');
  for (const token of ['TaskDefinition', 'TaskRun', 'TaskRunLog', 'payload_schema']) {
    assert.match(model, new RegExp(token), `persistence model missing ${token}`);
  }
});

test('B1.3 three UI templates expose equivalent task management pages', () => {
  for (const app of ['web-antd', 'web-ele', 'web-naive']) {
    for (const path of [
      `admin/apps/${app}/src/api/core/tasks.ts`,
      `admin/apps/${app}/src/views/system/tasks/index.vue`,
      `admin/apps/${app}/src/router/routes/modules/system.ts`,
    ]) {
      assert.equal(existsSync(new URL(path, root)), true, `missing ${path}`);
    }
    const page = read(`admin/apps/${app}/src/views/system/tasks/index.vue`);
    const api = read(`admin/apps/${app}/src/api/core/tasks.ts`);
    const route = read(`admin/apps/${app}/src/router/routes/modules/system.ts`);
    for (const token of ['loading', 'empty', 'error', 'aria-busy', 'manual', 'run', 'retry', 'cancelRun', 'logs']) {
      assert.match(page, new RegExp(token, 'i'), `${app} page missing ${token}`);
    }
    assert.match(api, /listTasks|runTask/);
    assert.match(api, /cancelTaskRunApi|retryTaskRunApi|listTaskRunLogsApi/);
    assert.match(route, /SystemTasks|system\/tasks|tasks/);
    for (const locale of ['zh-CN', 'en-US']) {
      const messages = JSON.parse(read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`));
      assert.ok(messages.tasks?.title, `${app} ${locale} tasks.title missing`);
      assert.ok(messages.tasks?.run, `${app} ${locale} tasks.run missing`);
    }
  }
});
