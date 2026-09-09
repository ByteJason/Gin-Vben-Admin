import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const root = new URL('../', import.meta.url);
const variants = ['web-antd', 'web-ele', 'web-naive'];
const read = (path) => readFileSync(new URL(path, root), 'utf8');

for (const variant of variants) {
  test(`${variant}: scheduled tasks expose server-backed list and run workspaces`, () => {
    const source = read(`apps/${variant}/src/views/system/tasks/index.vue`);
    assert.match(source, /role="tablist"/);
    assert.match(source, /id="tasks-tab"/);
    assert.match(source, /id="runs-tab"/);
    assert.match(source, /listAllTaskRunsApi/);
    assert.match(source, /item\.nextRunAt/);
    assert.doesNotMatch(source, /cronFieldMatches|setMinutes\(|Math\.random/);
    assert.match(source, /v-model="selectedIds"/);
    assert.match(source, /batchAction/);
    assert.match(source, /runDetailsOpen/);
    assert.match(source, /executorType === 'registered'/);
    assert.match(source, /executorType === 'http'/);
    for (const field of ['methodKey', 'headersText', 'allowInternal', 'triggerSource', 'durationMs', 'resultSummary', 'redactedOutput']) {
      assert.ok(source.includes(field), `${variant} omits ${field}`);
    }
    assert.match(source, /saving\.value\) return/);
    assert.match(source, /runsRequest/);
    assert.match(source, /tasksRequest/);
  });

  test(`${variant}: query wrappers use generated paginated contracts`, () => {
    const source = read(`apps/${variant}/src/api/core/tasks.ts`);
    assert.match(source, /TaskListQuery/);
    assert.match(source, /TaskRunListQuery/);
    assert.match(source, /TaskDefinitionPage/);
    assert.match(source, /TaskRunPage/);
    assert.match(source, /ADMIN_ENDPOINTS\.listAllTaskRuns/);
    assert.match(source, /params: query/);
    assert.doesNotMatch(source, /mock|Math\.random/);
  });
}
