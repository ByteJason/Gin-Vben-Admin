import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..');

function readFunction(source, name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name} must exist`);
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
  assert.match(html, /敏感值不写入恢复日志/);
  assert.doesNotMatch(html, /凭据仅用于本次测试/);
  assert.match(html, /id="admin-username"/);
  assert.match(html, /id="admin-username"[^>]*value="admin"/);
  assert.match(html, /id="admin-password"/);
  assert.match(html, /id="admin-password-confirm"/);
  assert.doesNotMatch(html, /id="confirm-cleanup"/);
  assert.match(html, /id="apply-button"/);
  assert.match(html, /id="apply-result"/);
  assert.match(html, /id="rollback-button"/);
  assert.match(html, /id="apply-progress"/);
  assert.match(html, /id="next-steps"/);
  assert.match(html, /go run \.\/cmd\/api\/main\.go/);
  assert.match(html, /pnpm run build/);
  assert.match(html, /pnpm run dev/);
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
  assert.match(script, /currentPlan\s*=\s*\{\s*\.\.\.currentPlan,\s*installed:\s*true\s*\}/);
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
  assert.match(script, /pnpm run build/);
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
  assert.match(styles, /@media\s*\(max-width:\s*1120px\)/);
  assert.match(styles, /@media\s*\(max-width:\s*720px\)/);
});

test('failed installation feedback requires dependency rechecks and distinguishes a busy lease', () => {
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');

  const failureMessage = readFunction(script, 'installationFailureMessage');
  const busyMessage = Function(
    `'use strict'; ${failureMessage}; return installationFailureMessage({ errorKey: 'installation_running', canRetry: true });`,
  )();
  assert.match(busyMessage, /另一项初始化或安装任务正在执行/);
  assert.match(busyMessage, /重新运行 pnpm run init/);
  assert.doesNotMatch(busyMessage, /自动回滚|副作用/);

  const invalidate = readFunction(script, 'invalidateDependencyChecks');
  const invalidated = Function(`
    'use strict';
    let databaseCheckPassed = true;
    let redisCheckPassed = true;
    let updates = 0;
    const databaseResult = { textContent: '连接成功', dataset: { tone: 'success' } };
    const redisResult = { textContent: '连接成功', dataset: { tone: 'success' } };
    const updateApplyButton = () => { updates += 1; };
    ${invalidate}
    invalidateDependencyChecks();
    return { databaseCheckPassed, redisCheckPassed, updates, databaseResult, redisResult };
  `)();
  assert.equal(invalidated.databaseCheckPassed, false);
  assert.equal(invalidated.redisCheckPassed, false);
  assert.equal(invalidated.updates, 1);
  for (const result of [invalidated.databaseResult, invalidated.redisResult]) {
    assert.match(result.textContent, /重新测试/);
    assert.equal(result.dataset.tone, 'pending');
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

  assert.match(script, /finally\s*\{[\s\S]*?clearInstallSecrets\(\);[\s\S]*?invalidateDependencyChecks\(\)/);
  assert.match(script, /announceApplyError\(installationFailureMessage\(result\)\)/);
  assert.match(script, /const targetEndpoint = retryJobId[\s\S]*?encodeURIComponent\(retryJobId\)/);
  assert.match(script, /rollbackEndpoint[\s\S]*?encodeURIComponent\(rollbackJobId\)/);
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
