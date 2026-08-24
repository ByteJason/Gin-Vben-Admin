import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..');

function readFunction(source, name) {
  let start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name} must exist`);
  if (source.slice(Math.max(0, start - 6), start) === 'async ') start -= 6;
  const end = source.indexOf('\n}\n', start);
  assert.notEqual(end, -1, `${name} must have a top-level closing brace`);
  return source.slice(start, end + 2);
}

test('installation shell is independent and exposes an accessible status region', () => {
  const html = readFileSync(join(root, 'src/index.html'), 'utf8');
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');
  const styles = readFileSync(join(root, 'src/styles.css'), 'utf8');

  assert.match(html, /lang="zh-CN"/);
  assert.match(html, /<main\b/);
  assert.match(html, /href="#install-main"/);
  assert.match(html, /aria-live="polite"/);
  assert.match(html, /id="capability-list"/);
  assert.match(html, /id="plan-form"/);
  assert.match(html, /id="selected-ui-summary"/);
  assert.doesNotMatch(html, /id="ui-choice"|name="selectedUi"/);
  assert.match(html, /id="mode-choice"/);
  assert.match(html, /<option value="dev" selected>开发调试（推荐）<\/option>/);
  assert.match(html, /id="locale-mode"/);
  assert.match(html, /id="locale-choice"/);
  assert.match(html, /id="locale-suggestion"/);
  assert.match(html, /id="plan-panel"/);
  assert.match(html, /id="connection-panel"/);
  assert.match(html, /id="database-form"/);
  assert.match(html, /id="database-driver"/);
  assert.match(html, /value="mysql"/);
  assert.match(html, /value="postgres"/);
  assert.match(html, /id="database-host"[^>]*value="localhost"/);
  assert.match(html, /id="database-port"[^>]*value="3306"/);
  assert.doesNotMatch(html, /id="database-password"[^>]*\svalue=/);
  assert.doesNotMatch(html, /id="database-dsn"[^>]*\svalue=/);
  assert.match(html, /id="redis-form"/);
  assert.match(html, /id="redis-address"[^>]*value="localhost:6379"/);
  assert.match(html, /id="redis-db"[^>]*value="0"/);
  assert.match(html, /id="admin-form"/);
  assert.match(html, /敏感值.*仅保留在当前页面.*不写入恢复日志/);
  assert.doesNotMatch(html, /凭据仅用于本次测试/);
  assert.match(html, /id="admin-username"/);
  assert.match(html, /id="admin-username"[^>]*value="admin"/);
  assert.match(html, /id="admin-password"/);
  assert.match(html, /id="admin-password-confirm"/);
  assert.doesNotMatch(html, /id="confirm-cleanup"/);
  assert.match(html, /id="apply-button"/);
  assert.match(html, /id="apply-result"/);
  assert.match(html, /id="install-failure-details"/);
  assert.match(html, /id="failure-reason"/);
  assert.match(html, /id="failure-step"/);
  assert.match(html, /id="failure-error-code"/);
  assert.match(html, /id="failure-error-key"/);
  assert.match(html, /id="failure-job-id"/);
  assert.match(html, /id="install-failure-details"[^>]*role="alert"[^>]*aria-live="assertive"[^>]*tabindex="-1"/);
  assert.match(html, /id="rollback-button"/);
  assert.match(html, /id="apply-progress"/);
  assert.match(html, /id="next-steps"/);
  assert.match(html, /回到仓库根目录/);
  assert.match(html, /class="terminal-grid"/);
  assert.match(html, /终端 1：服务端/);
  assert.match(html, /终端 2：管理端/);
  assert.match(html, /go run \.\/cmd\/api\/main\.go/);
  assert.match(html, /pnpm install/);
  assert.match(html, /pnpm run dev/);
  assert.doesNotMatch(html, /pnpm run build/);
  const nextSteps = html.match(/<section id="next-steps"[\s\S]*?<\/section>/)?.[0] ?? '';
  assert.match(nextSteps, /终端 1：[\s\S]*?cd server[\s\S]*?go run \.\/cmd\/api\/main\.go[\s\S]*?终端 2：[\s\S]*?cd admin[\s\S]*?pnpm install[\s\S]*?pnpm run dev/);
  assert.doesNotMatch(nextSteps, /pnpm run build/);
  assert.match(html, /aria-valuetext="尚未开始"/);
  assert.match(html, /autocomplete="new-password"/);
  assert.match(html, /aria-current="step"/);
  assert.match(html, /href="\/install\/styles\.css"/);
  assert.match(html, /src="\/install\/app\.js"/);
  assert.match(script, /\/api\/system\/install\/v1\/status/);
  assert.match(script, /\/api\/system\/install\/v1\/capabilities/);
  assert.match(script, /\/api\/system\/install\/v1\/plan/);
  assert.match(script, /\/api\/system\/install\/v1\/check\/database/);
  assert.match(script, /\/api\/system\/install\/v1\/check\/redis/);
  assert.match(script, /\/api\/system\/install\/v1\/apply/);
  assert.match(script, /\/api\/system\/install\/v1\/progress/);
  assert.match(script, /\/api\/system\/install\/v1\/retry/);
  assert.match(script, /\/api\/system\/install\/v1\/rollback/);
  assert.match(script, /pollInstallation/);
  assert.match(script, /let retryJobId\s*=\s*null/);
  assert.match(script, /let rollbackJobId\s*=\s*null/);
  assert.doesNotMatch(script, /lastFailedJobId/);
  assert.match(script, /requestRollback/);
  assert.match(script, /重新尝试安装/);
  assert.match(script, /currentStep/);
  assert.match(script, /canRollback/);
  assert.match(script, /retryJobId\s*=\s*job\.canRetry\s*\?\s*job\.id\s*:\s*null/);
  assert.match(script, /rollbackJobId\s*=\s*job\.canRollback\s*\?\s*job\.id\s*:\s*null/);
  assert.match(script, /commitCompletedInstallation/);
  assert.doesNotMatch(script, /uiChoice|confirmCleanup/);
  assert.match(script, /requestInstallation/);
  assert.match(script, /applyButton\.disabled\s*=\s*!currentPlan/);
  assert.match(script, /modeChoice\.addEventListener\('change', invalidatePlanIfModeChanged\)/);
  assert.match(script, /method:\s*'POST'/);
  assert.match(script, /selectedUi/);
  assert.doesNotMatch(script, /selectedUi\s*:/);
  assert.match(script, /JSON\.stringify\(\{ mode \}\)/);
  assert.match(script, /localeMode/);
  assert.match(script, /localeChoice/);
  assert.match(script, /canCleanup/);
  assert.match(script, /databaseDriver/);
  assert.match(script, /redisAddress/);
  assert.match(script, /credentials:\s*'same-origin'/);
  assert.match(script, /textContent/);
  assert.match(script, /\.focus\(\)/);
  assert.match(script, /aria-valuetext/);
  assert.match(script, /status\.state === 'installing'/);
  assert.match(script, /status\.state === 'pristine'/);
  assert.match(script, /等待执行 pnpm run init/);
  assert.match(script, /window\.setTimeout\(loadStatus,/);
  assert.match(script, /安装任务正在执行/);
  assert.doesNotMatch(script, /Ctrl\+C/);
  assert.match(script, /pnpm run dev/);
  assert.match(script, /pnpm install/);
  assert.match(script, /const installationCompletedMessage\s*=/);
  assert.equal(script.match(/installationCompletedMessage/g)?.length, 3);
  assert.doesNotMatch(script, /pnpm run build/);
  assert.doesNotMatch(script, /构建并暂存界面资源/);
  assert.doesNotMatch(script, /innerHTML/);
  assert.doesNotMatch(script, /localStorage|sessionStorage/);
  assert.doesNotMatch(`${html}\n${script}`, /admin\/apps|web-antd|web-ele|web-naive/);
  assert.match(styles, /:focus-visible/);
  assert.match(styles, /prefers-reduced-motion:\s*reduce/);
  assert.match(styles, /min-height:\s*44px/);
  assert.match(styles, /@media\s*\(max-width:\s*480px\)/);
});

test('installation forms expose semantic groups and responsive installation feedback', () => {
  const html = readFileSync(join(root, 'src/index.html'), 'utf8');
  const styles = readFileSync(join(root, 'src/styles.css'), 'utf8');

  assert.match(html, /<fieldset class="plan-group plan-group--runtime">[\s\S]*?<legend>界面与运行方式<\/legend>/);
  assert.match(html, /<fieldset class="plan-group plan-group--locale">[\s\S]*?<legend>语言偏好<\/legend>/);
  assert.match(html, /class="connection-grid"/);
  assert.match(html, /class="connection-form connection-form--database"/);
  assert.match(html, /class="connection-form connection-form--redis"/);
  assert.match(html, /class="connection-form connection-form--admin"/);
  assert.match(html, /id="apply-result"[^>]*aria-atomic="true"/);
  assert.match(html, /class="progress-panel"/);

  assert.match(styles, /\.page-shell\s*\{[\s\S]*?width:\s*min\(1400px,/);
  assert.match(styles, /\.connection-grid\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2,/);
  assert.match(styles, /\.connection-form--admin\s*\{[\s\S]*?grid-template-columns:\s*repeat\(3,/);
  assert.match(styles, /\.plan-form\s*>\s*\.primary-button\s*\{[\s\S]*?grid-column:\s*1\s*\/\s*-1/);
  assert.match(styles, /\.connection-result:not\(:empty\)/);
  assert.match(styles, /\.install-failure-details\s*\{/);
  assert.match(styles, /\.failure-diagnostics\s*\{[\s\S]*?grid-template-columns:\s*repeat\(2,/);
  assert.match(html, /id="failure-reason-key"/);
  assert.match(html, /id="failure-operation"/);
  assert.match(html, /id="failure-database-code"/);
  assert.match(styles, /@media\s*\(max-width:\s*1120px\)/);
  assert.match(styles, /@media\s*\(max-width:\s*720px\)/);
  assert.match(styles, /\.terminal-card pre\s*\{[\s\S]*?white-space:\s*pre-wrap;[\s\S]*?overflow-wrap:\s*anywhere;/);
  const compactLayout = styles.match(/@media\s*\(max-width:\s*720px\)\s*\{[\s\S]*?\n\}/)?.[0] ?? '';
  assert.match(compactLayout, /\.terminal-grid[\s\S]*?grid-template-columns:\s*1fr;/);
});

test('failed installation preserves current input and exposes actionable diagnostics', () => {
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');

  const failureMessage = readFunction(script, 'installationFailureMessage');
  const busyMessage = Function(
    `'use strict'; ${failureMessage}; return installationFailureMessage({ errorKey: 'installation_running', canRetry: true });`,
  )();
  assert.match(busyMessage, /另一项初始化或安装任务正在执行/);
  assert.match(busyMessage, /重新运行 pnpm run init/);
  assert.match(busyMessage, /当前输入已保留/);
  assert.doesNotMatch(busyMessage, /自动回滚|副作用/);
  assert.doesNotMatch(busyMessage, /敏感字段已清空|重新输入凭据/);

  const diagnosticsFunction = readFunction(script, 'installationFailureDiagnostics');
  const diagnostics = Function(`
    'use strict';
    const stepLabels = { database: '验证数据库' };
    ${failureMessage}
    ${diagnosticsFunction}
    return installationFailureDiagnostics({
      id: 'install-job-1',
      failureStep: 'database',
      currentStep: 'failed',
      errorCode: 10001,
      errorKey: 'validation_failed',
      canRetry: true,
    });
  `)();
  assert.deepEqual(diagnostics, {
    reason: '数据库连接复核未通过，请检查数据库服务和当前配置。当前输入已保留。',
    step: '验证数据库',
    errorCode: '10001',
    errorKey: 'validation_failed',
    reasonKey: '未提供',
    operation: '未提供',
    databaseCode: '—',
    jobId: 'install-job-1',
  });

  const tlsFailure = Function(`
    'use strict';
    ${failureMessage}
    return installationFailureMessage({
      errorKey: 'internal_error',
      failureStep: 'schema',
      failureReason: 'tls_mode_mismatch',
    });
  `)();
  assert.match(tlsFailure, /TLS/);
  assert.match(tlsFailure, /数据库连接测试与迁移/);
  assert.match(tlsFailure, /当前输入已保留/);

  const classifiedReasons = [
    'tls_configuration_failed', 'authentication_failed', 'permission_denied', 'database_unavailable', 'database_busy',
    'schema_unavailable', 'schema_conflict', 'migration_dirty', 'migration_statement_failed',
    'migration_status_failed', 'migration_close_failed', 'invalid_configuration', 'unknown',
  ];
  for (const failureReason of classifiedReasons) {
    const message = Function(
      'failureReason',
      `'use strict'; ${failureMessage}; return installationFailureMessage({ errorKey: 'internal_error', failureStep: 'schema', failureReason });`,
    )(failureReason);
    assert.match(message, /当前输入已保留/, failureReason);
    assert.doesNotMatch(message, /数据库结构迁移执行失败，请查看失败任务定位信息/, failureReason);
  }

  const setActions = readFunction(script, 'setFailedJobActions');
  const rollbackOnly = Function(`
    'use strict';
    let retryJobId = 'stale-retry';
    let rollbackJobId = null;
    const rollbackButton = { hidden: true };
    ${setActions}
    setFailedJobActions({ id: 'rollback-only', canRetry: false, canRollback: true });
    return { retryJobId, rollbackJobId, rollbackHidden: rollbackButton.hidden };
  `)();
  assert.deepEqual(rollbackOnly, {
    retryJobId: null,
    rollbackJobId: 'rollback-only',
    rollbackHidden: false,
  });

  const announce = readFunction(script, 'announceApplyError');
  const announced = Function(`
    'use strict';
    const attributes = {};
    let focused = false;
    const applyResult = {
      textContent: '',
      dataset: {},
      setAttribute(name, value) { attributes[name] = value; },
      focus() { focused = true; },
    };
    ${announce}
    announceApplyError('安装冲突');
    return { applyResult, attributes, focused };
  `)();
  assert.equal(announced.applyResult.dataset.tone, 'error');
  assert.equal(announced.attributes.role, 'alert');
  assert.equal(announced.attributes['aria-live'], 'assertive');
  assert.equal(announced.focused, true);

  const detailedAnnouncement = Function(`
    'use strict';
    const attributes = {};
    let focused = false;
    const applyResult = {
      textContent: '',
      dataset: {},
      setAttribute(name, value) { attributes[name] = value; },
      focus() { focused = true; },
    };
    ${announce}
    announceApplyError('安装冲突', true);
    return { attributes, focused };
  `)();
  assert.equal(detailedAnnouncement.attributes.role, 'status');
  assert.equal(detailedAnnouncement.attributes['aria-live'], 'polite');
  assert.equal(detailedAnnouncement.focused, false);

  assert.doesNotMatch(script, /敏感字段已清空|重新输入凭据/);
  assert.match(script, /rollbackEndpoint[\s\S]*?encodeURIComponent\(rollbackJobId\)/);
});

test('a stale retry falls back to apply exactly once and keeps the submitted credentials', async () => {
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');
  const postRequest = readFunction(script, 'postInstallationRequest');
  const submitRequest = readFunction(script, 'submitInstallationRequest');

  const recovered = await Function(`
    'use strict';
    const retryEndpoint = '/api/system/install/v1/retry';
    const applyEndpoint = '/api/system/install/v1/apply';
    let cleared = 0;
    const calls = [];
    const browserLanguageHeader = () => 'zh-CN';
    const responses = [
      { ok: false, status: 404, envelope: { code: 30000, message: 'not found' } },
      { ok: true, status: 202, envelope: { code: 0, data: { id: 'new-job', state: 'running' } } },
    ];
    const clearFailedJobActions = () => { cleared += 1; };
    const fetcher = async (url, options) => {
      calls.push({ url, body: JSON.parse(options.body) });
      const next = responses.shift();
      return { ok: next.ok, status: next.status, json: async () => next.envelope };
    };
    ${postRequest}
    ${submitRequest}
    return submitInstallationRequest({ admin: { password: 'ADMIN_SECRET' } }, 'old-job', fetcher)
      .then((outcome) => ({ outcome, calls, cleared }));
  `)();

  assert.deepEqual(recovered.calls.map(({ url }) => url), [
    '/api/system/install/v1/retry/old-job',
    '/api/system/install/v1/apply',
  ]);
  assert.deepEqual(recovered.calls.map(({ body }) => body.admin.password), ['ADMIN_SECRET', 'ADMIN_SECRET']);
  assert.equal(recovered.cleared, 1);
  assert.equal(recovered.outcome.envelope.data.id, 'new-job');

  const notRecovered = await Function(`
    'use strict';
    const retryEndpoint = '/api/system/install/v1/retry';
    const applyEndpoint = '/api/system/install/v1/apply';
    let cleared = 0;
    const calls = [];
    const browserLanguageHeader = () => 'zh-CN';
    const clearFailedJobActions = () => { cleared += 1; };
    const fetcher = async (url) => {
      calls.push(url);
      return { ok: false, status: 500, json: async () => ({ code: 50000 }) };
    };
    ${postRequest}
    ${submitRequest}
    return submitInstallationRequest({ admin: { password: 'ADMIN_SECRET' } }, 'old-job', fetcher)
      .then((outcome) => ({ outcome, calls, cleared }));
  `)();
  assert.deepEqual(notRecovered.calls, ['/api/system/install/v1/retry/old-job']);
  assert.equal(notRecovered.cleared, 0);
});

test('completed reconciliation clears secrets only after status confirms installation', async () => {
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');
  const clearSensitive = readFunction(script, 'clearSensitiveFields');
  const clearSecrets = readFunction(script, 'clearInstallSecrets');
  const commitCompleted = readFunction(script, 'commitCompletedInstallation');
  const reconcileCompleted = readFunction(script, 'reconcileCompletedInstallation');

  const run = async (statusResult) => Function(`
    'use strict';
    const makeInput = (value) => ({ value });
    const databaseInputs = [makeInput('DB_SECRET'), makeInput('DSN_SECRET')];
    const redisInputs = [makeInput('REDIS_SECRET')];
    const adminInputs = [makeInput('ADMIN_SECRET'), makeInput('ADMIN_SECRET')];
    const makeForm = (inputs) => ({ querySelectorAll: () => inputs });
    const databaseForm = makeForm(databaseInputs);
    const redisForm = makeForm(redisInputs);
    const adminForm = makeForm(adminInputs);
    const events = [];
    const clearFailedJobActions = () => events.push('clear-actions');
    const renderApplyResult = () => events.push('render-result');
    const renderStatus = (status) => events.push(status.installed ? 'render-installed' : 'render-uninstalled');
    ${clearSensitive}
    ${clearSecrets}
    ${commitCompleted}
    ${reconcileCompleted}
    const statusResult = ${JSON.stringify(statusResult)};
    const statusReader = async () => {
      if (statusResult === '__throw__') throw new Error('status unavailable');
      return statusResult;
    };
    return reconcileCompletedInstallation(statusReader)
      .then((completed) => ({ completed, error: '', values: [...databaseInputs, ...redisInputs, ...adminInputs].map((input) => input.value), events }))
      .catch((error) => ({ completed: false, error: error.message, values: [...databaseInputs, ...redisInputs, ...adminInputs].map((input) => input.value), events }));
  `)();

  const confirmed = await run({ installed: true, selectedUi: 'ele', mode: 'dev' });
  assert.equal(confirmed.completed, true);
  assert.deepEqual(confirmed.values, ['', '', '', '', '']);
  assert.deepEqual(confirmed.events, ['clear-actions', 'render-installed']);

  const unconfirmed = await run({ installed: false, state: 'ui_prepared' });
  assert.equal(unconfirmed.completed, false);
  assert.deepEqual(unconfirmed.values, ['DB_SECRET', 'DSN_SECRET', 'REDIS_SECRET', 'ADMIN_SECRET', 'ADMIN_SECRET']);
  assert.deepEqual(unconfirmed.events, []);

  const unavailable = await run('__throw__');
  assert.equal(unavailable.completed, false);
  assert.equal(unavailable.error, 'status unavailable');
  assert.deepEqual(unavailable.values, ['DB_SECRET', 'DSN_SECRET', 'REDIS_SECRET', 'ADMIN_SECRET', 'ADMIN_SECRET']);
  assert.deepEqual(unavailable.events, []);
});

test('the public installation flow wires immediate and asynchronous completed states to reconciliation', async () => {
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');
  const completionDetected = readFunction(script, 'installationCompletionDetected');
  const requestInstallation = readFunction(script, 'requestInstallation');

  const run = async (scenario) => Function(`
    'use strict';
    return (async () => {
      const scenario = ${JSON.stringify(scenario)};
      const events = [];
      let currentPlan = { mode: 'dev', canBuild: true, canWriteEnv: true, canCleanup: true };
      let databaseCheckPassed = true;
      let redisCheckPassed = true;
      let retryJobId = null;
      const adminPassword = { value: 'ADMIN_SECRET' };
      const adminPasswordConfirm = { value: 'ADMIN_SECRET', focus() {} };
      const adminUsername = { value: 'fixture_admin' };
      const modeChoice = { value: 'dev' };
      const localeMode = { value: 'single' };
      const localeChoice = { value: 'zh-CN' };
      const applyButton = { disabled: false, textContent: '' };
      const applyResult = { textContent: '', dataset: {}, setAttribute() {}, focus() {} };
      const clearInstallationFailure = () => events.push('clear-diagnostics');
      const setProgress = () => events.push('progress-reset');
      const announceApplyError = () => events.push('announce-failure');
      const dependencyFormValues = () => ({ database: {}, redis: {} });
      const submitInstallationRequest = async () => scenario === 'immediate'
        ? { response: { ok: false, status: 409 }, envelope: { code: 10006, traceId: 'completed-request' } }
        : { response: { ok: true, status: 202 }, envelope: { code: 0, data: { id: 'completed-job', state: 'running' } } };
      const reconcileCompletedInstallation = async () => { events.push('reconcile-completed'); return true; };
      const renderInstallationFailure = () => events.push('render-failure');
      const renderJobProgress = () => events.push('render-progress');
      const pollInstallation = async () => ({ id: 'completed-job', state: 'failed', errorKey: 'installation_completed' });
      const setFailedJobActions = () => events.push('set-failed-actions');
      const installationFailureMessage = () => 'failure';
      const commitCompletedInstallation = () => events.push('commit-direct');
      const updateApplyButton = () => events.push('update-button');
      ${completionDetected}
      ${requestInstallation}
      await requestInstallation({ preventDefault() {} });
      return { events, password: adminPassword.value };
    })();
  `)();

  const immediate = await run('immediate');
  assert.equal(immediate.events.filter((event) => event === 'reconcile-completed').length, 1);
  assert.doesNotMatch(immediate.events.join(','), /announce-failure|render-failure|set-failed-actions/);

  const asynchronous = await run('asynchronous');
  assert.deepEqual(asynchronous.events.filter((event) => event === 'reconcile-completed'), ['reconcile-completed']);
  assert.match(asynchronous.events.join(','), /render-progress.*reconcile-completed/);
  assert.doesNotMatch(asynchronous.events.join(','), /announce-failure|render-failure|set-failed-actions/);
});

test('editing one dependency marks only its own successful check as stale', () => {
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');
  const invalidateOne = readFunction(script, 'invalidateDependencyCheck');
  const edit = (target) => Function(`
    'use strict';
    let databaseCheckPassed = true;
    let redisCheckPassed = true;
    let updates = 0;
    const databaseResult = { textContent: '数据库连接成功', dataset: { tone: 'success' } };
    const redisResult = { textContent: 'Redis 连接成功', dataset: { tone: 'success' } };
    const updateApplyButton = () => { updates += 1; };
    ${invalidateOne}
    invalidateDependencyCheck(${target}Result);
    return { databaseCheckPassed, redisCheckPassed, databaseResult, redisResult, updates };
  `)();

  const databaseEdit = edit('database');
  assert.equal(databaseEdit.databaseCheckPassed, false);
  assert.equal(databaseEdit.redisCheckPassed, true);
  assert.match(databaseEdit.databaseResult.textContent, /配置已变化.*重新测试/);
  assert.equal(databaseEdit.databaseResult.dataset.tone, 'pending');
  assert.equal(databaseEdit.redisResult.textContent, 'Redis 连接成功');
  assert.equal(databaseEdit.redisResult.dataset.tone, 'success');
  assert.equal(databaseEdit.updates, 1);

  const redisEdit = edit('redis');
  assert.equal(redisEdit.databaseCheckPassed, true);
  assert.equal(redisEdit.redisCheckPassed, false);
  assert.equal(redisEdit.databaseResult.textContent, '数据库连接成功');
  assert.equal(redisEdit.databaseResult.dataset.tone, 'success');
  assert.match(redisEdit.redisResult.textContent, /配置已变化.*重新测试/);
  assert.equal(redisEdit.redisResult.dataset.tone, 'pending');
  assert.equal(redisEdit.updates, 1);

  assert.match(script, /databaseForm\.addEventListener\('input',\s*\(\)\s*=>\s*invalidateDependencyCheck\(databaseResult\)\)/);
  assert.match(script, /redisForm\.addEventListener\('input',\s*\(\)\s*=>\s*invalidateDependencyCheck\(redisResult\)\)/);
});

test('installation workspace builds a self-contained static dist', () => {
  rmSync(join(root, 'dist'), { force: true, recursive: true });
  const result = spawnSync(process.execPath, [join(root, 'scripts/build.mjs')], {
    cwd: root,
    encoding: 'utf8',
  });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  for (const path of ['index.html', 'app.js', 'styles.css', 'asset-manifest.json']) {
    assert.equal(existsSync(join(root, 'dist', path)), true, path);
  }
  const manifest = JSON.parse(readFileSync(join(root, 'dist/asset-manifest.json'), 'utf8'));
  assert.deepEqual(Object.keys(manifest.files).sort(), ['app.js', 'index.html', 'styles.css']);
  for (const digest of Object.values(manifest.files)) {
    assert.match(digest, /^[a-f0-9]{64}$/);
  }
});
