const endpoint = '/api/system/install/v1/status';
const capabilitiesEndpoint = '/api/system/install/v1/capabilities';
const planEndpoint = '/api/system/install/v1/plan';
const databaseCheckEndpoint = '/api/system/install/v1/check/database';
const redisCheckEndpoint = '/api/system/install/v1/check/redis';
const applyEndpoint = '/api/system/install/v1/apply';
const progressEndpoint = '/api/system/install/v1/progress';
const retryEndpoint = '/api/system/install/v1/retry';
const rollbackEndpoint = '/api/system/install/v1/rollback';

const title = document.querySelector('#status-title');
const badge = document.querySelector('#status-badge');
const message = document.querySelector('#status-message');
const details = document.querySelector('#status-details');
const installerVersion = document.querySelector('#installer-version');
const selectedUi = document.querySelector('#selected-ui');
const selectedUiSummary = document.querySelector('#selected-ui-summary');
const selectedMode = document.querySelector('#selected-mode');
const retryButton = document.querySelector('#retry-button');
const platformSummary = document.querySelector('#platform-summary');
const capabilityList = document.querySelector('#capability-list');
const selectionPanel = document.querySelector('#selection-panel');
const planForm = document.querySelector('#plan-form');
const modeChoice = document.querySelector('#mode-choice');
const localeMode = document.querySelector('#locale-mode');
const localeChoice = document.querySelector('#locale-choice');
const localeSuggestion = document.querySelector('#locale-suggestion');
const planButton = document.querySelector('#plan-button');
const planMessage = document.querySelector('#plan-message');
const planPanel = document.querySelector('#plan-panel');
const planCleanup = document.querySelector('#plan-cleanup');
const planBuild = document.querySelector('#plan-build');
const planEnv = document.querySelector('#plan-env');
const planRestart = document.querySelector('#plan-restart');
const planEntries = document.querySelector('#plan-entries');
const connectionPanel = document.querySelector('#connection-panel');
const databaseForm = document.querySelector('#database-form');
const databaseDriver = document.querySelector('#database-driver');
const databasePort = document.querySelector('#database-port');
const databaseResult = document.querySelector('#database-result');
const redisForm = document.querySelector('#redis-form');
const redisAddress = document.querySelector('#redis-address');
const redisResult = document.querySelector('#redis-result');
const adminForm = document.querySelector('#admin-form');
const adminUsername = document.querySelector('#admin-username');
const adminPassword = document.querySelector('#admin-password');
const adminPasswordConfirm = document.querySelector('#admin-password-confirm');
const applyButton = document.querySelector('#apply-button');
const applyResult = document.querySelector('#apply-result');
const installFailureDetails = document.querySelector('#install-failure-details');
const failureReason = document.querySelector('#failure-reason');
const failureStep = document.querySelector('#failure-step');
const failureErrorCode = document.querySelector('#failure-error-code');
const failureErrorKey = document.querySelector('#failure-error-key');
const failureReasonKey = document.querySelector('#failure-reason-key');
const failureOperation = document.querySelector('#failure-operation');
const failureDatabaseCode = document.querySelector('#failure-database-code');
const failureJobId = document.querySelector('#failure-job-id');
const rollbackButton = document.querySelector('#rollback-button');
const applyProgress = document.querySelector('#apply-progress');
const applySteps = document.querySelector('#apply-steps');
const nextSteps = document.querySelector('#next-steps');

const uiLabels = { antd: 'Ant Design Vue', ele: 'Element Plus', naive: 'Naive UI' };
const modeLabels = {
  embedded: '嵌入式单包',
  standalone: '静态资源独立部署',
  api_only: '仅 API',
  dev: '开发调试',
};
const stepLabels = {
  queued: '等待执行',
  request: '提交安装请求',
  coordination: '获取安装执行权',
  plan: '复核目录权限',
  database: '验证数据库',
  redis: '验证 Redis',
  recovery: '恢复上次安装事务',
  journal: '保存安装事务',
  schema: '执行数据库迁移',
  identity: '初始化管理员',
  environment: '写入运行配置',
  marker: '生成安装标记',
  lock: '写入安装锁',
  complete: '安装完成',
  failed: '安装失败',
};

let currentPlan = null;
let databaseCheckPassed = false;
let redisCheckPassed = false;
let retryJobId = null;
let rollbackJobId = null;
let statusRefreshTimer = null;

function browserLanguageHeader() {
  const languages = Array.isArray(navigator.languages) && navigator.languages.length
    ? navigator.languages
    : [navigator.language || 'en-US'];
  return languages.join(',');
}

function suggestBrowserLocale() {
  const browserLocale = /^zh(?:-|$)/i.test(navigator.language || '') ? 'zh-CN' : 'en-US';
  localeChoice.value = browserLocale;
  localeSuggestion.textContent = `已根据浏览器语言建议 ${browserLocale}，可手动调整。`;
}

async function fetchInstallationStatus(fetcher = fetch) {
  const response = await fetcher(endpoint, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  });
  const envelope = await response.json();
  if (!response.ok || envelope.code !== 0 || !envelope.data) {
    throw new Error('status request failed');
  }
  return envelope.data;
}

async function loadStatus() {
  setPending();
  try {
    const status = await fetchInstallationStatus();
    renderStatus(status);
    return status;
  } catch {
    renderError();
    return null;
  }
}

async function loadCapabilities() {
  platformSummary.textContent = '检测中';
  capabilityList.replaceChildren(createCapabilityMessage('正在识别可用工具'));
  try {
    const response = await fetch(capabilitiesEndpoint, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('capabilities request failed');
    }
    renderCapabilities(envelope.data);
  } catch {
    platformSummary.textContent = '未识别';
    capabilityList.replaceChildren(createCapabilityMessage('暂未读取到运行工具信息'));
  }
}

function setPending() {
  clearStatusRefresh();
  title.textContent = '正在检查安装状态';
  badge.textContent = '检查中';
  badge.dataset.tone = 'pending';
  message.textContent = '正在连接本机安装服务，请稍候。';
  message.setAttribute('aria-live', 'polite');
  details.hidden = true;
  retryButton.hidden = true;
  rollbackButton.hidden = true;
  nextSteps.hidden = true;
}

function renderStatus(status) {
  clearStatusRefresh();
  installerVersion.textContent = status.installerVersion || '—';
  details.hidden = false;
  retryButton.hidden = true;
  rollbackButton.hidden = true;
  message.setAttribute('aria-live', 'polite');
  nextSteps.hidden = true;

  if (status.installed) {
    title.textContent = '系统已完成安装';
    badge.textContent = '已安装';
    badge.dataset.tone = 'success';
    message.textContent = '安装已完成。请停止并重新启动服务端，然后在 admin/ 依次运行 pnpm run build 和 pnpm run dev。';
    selectedUi.textContent = uiLabels[status.selectedUi] || status.selectedUi || '—';
    selectedUiSummary.textContent = selectedUi.textContent;
    selectedMode.textContent = modeLabels[status.mode] || status.mode || '—';
    selectionPanel.hidden = true;
    connectionPanel.hidden = true;
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
    clearFailedJobActions();
    nextSteps.hidden = false;
    title.focus();
    return;
  }

  if (status.state === 'installing') {
    title.textContent = '安装任务正在执行';
    badge.textContent = '安装中';
    badge.dataset.tone = 'pending';
    message.textContent = '安装事务正在执行；若服务中断，重新启动服务端并在此页重新提交即可恢复。';
    selectedUi.textContent = uiLabels[status.selectedUi] || status.selectedUi || '—';
    selectedUiSummary.textContent = selectedUi.textContent;
    selectedMode.textContent = '安装中';
    selectionPanel.hidden = true;
    connectionPanel.hidden = true;
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
    clearFailedJobActions();
    retryButton.hidden = false;
    scheduleStatusRefresh();
    title.focus();
    return;
  }

  if (status.state === 'pristine') {
    title.textContent = '等待选择管理界面';
    badge.textContent = '等待初始化';
    badge.dataset.tone = 'pending';
    message.textContent = '等待执行 pnpm run init 并选择一个 UI；完成后此页面会自动继续。';
    details.hidden = true;
    selectionPanel.hidden = true;
    connectionPanel.hidden = true;
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
    clearFailedJobActions();
    retryButton.hidden = false;
    scheduleStatusRefresh();
    title.focus();
    return;
  }

  if (status.state === 'inconsistent') {
    renderError('初始化尚未完成，请返回终端重新运行 pnpm run init 继续恢复；需要先查看 checkpoint 时可运行 pnpm run init -- --check。');
    return;
  }

  title.textContent = '安装服务已就绪';
  badge.textContent = '待安装';
  badge.dataset.tone = 'ready';
  message.textContent = '本机安装状态可用，接下来将检查运行环境和目录权限。';
  selectedUi.textContent = uiLabels[status.selectedUi] || status.selectedUi || '—';
  selectedUiSummary.textContent = selectedUi.textContent;
  selectedMode.textContent = '尚未选择';
  selectionPanel.hidden = false;
  connectionPanel.hidden = true;
  currentPlan = null;
  databaseCheckPassed = false;
  redisCheckPassed = false;
  clearFailedJobActions();
  updateApplyButton();
  title.focus();
}

function renderError(detail = '请确认服务已启动，然后重新检查。') {
  clearStatusRefresh();
  title.textContent = '安装服务暂不可用';
  badge.textContent = '检查失败';
  badge.dataset.tone = 'error';
  message.textContent = detail;
  message.setAttribute('aria-live', 'assertive');
  details.hidden = true;
  retryButton.hidden = false;
  selectionPanel.hidden = true;
  connectionPanel.hidden = true;
  nextSteps.hidden = true;
  title.focus();
}

function renderCapabilities(capabilities) {
  const platform = capabilities.platform || {};
  platformSummary.textContent = [platform.os, platform.arch].filter(Boolean).join(' / ') || '未识别';
  const items = Array.isArray(capabilities.tools) ? capabilities.tools : [];
  const nodes = items.map((tool) => {
    const item = document.createElement('li');
    const name = document.createElement('strong');
    const state = document.createElement('span');
    name.textContent = String(tool.id || 'tool').toUpperCase();
    state.textContent = tool.available ? tool.version || '可用' : '未检测到';
    state.dataset.available = tool.available ? 'true' : 'false';
    item.append(name, state);
    return item;
  });
  capabilityList.replaceChildren(...(nodes.length ? nodes : [createCapabilityMessage('未返回工具信息')]));
}

function createCapabilityMessage(value) {
  const item = document.createElement('li');
  item.className = 'capability-placeholder';
  item.textContent = value;
  return item;
}

async function requestPlan(event) {
  event.preventDefault();
  currentPlan = null;
  databaseCheckPassed = false;
  redisCheckPassed = false;
  clearFailedJobActions();
  updateApplyButton();
  planButton.disabled = true;
  planButton.textContent = '正在检查';
  planMessage.textContent = '正在验证目录的读取、写入、创建、重命名与删除能力。';
  planMessage.dataset.tone = 'pending';
  planPanel.hidden = true;
  try {
    const mode = modeChoice.value;
    const response = await fetch(planEndpoint, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        'Accept-Language': browserLanguageHeader(),
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ mode }),
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('plan request failed');
    }
    renderPlan(envelope.data);
  } catch {
    planMessage.textContent = '目录预检未完成，请检查服务与目录权限后重试。';
    planMessage.dataset.tone = 'error';
    planMessage.focus();
  } finally {
    planButton.disabled = false;
    planButton.textContent = '检查目录权限';
  }
}

function renderPlan(plan) {
  currentPlan = plan;
  databaseCheckPassed = false;
  redisCheckPassed = false;
  clearFailedJobActions();
  planCleanup.textContent = yesNo(plan.canCleanup);
  planBuild.textContent = yesNo(plan.canBuild);
  planEnv.textContent = yesNo(plan.canWriteEnv);
  planRestart.textContent = yesNo(plan.requiresRestart);
  const entries = Array.isArray(plan.entries) ? plan.entries : [];
  const nodes = entries.map((entry) => {
    const item = document.createElement('li');
    const path = document.createElement('code');
    const action = document.createElement('span');
    const ready = document.createElement('strong');
    const permission = entry.permission || {};
    path.textContent = String(entry.path || '—');
    action.textContent = actionLabel(entry.action);
    const permitted = permission.canRead && permission.canRename && permission.canDelete;
    ready.textContent = permitted || entry.action === 'keep' ? '可用' : '需处理';
    ready.dataset.ready = permitted || entry.action === 'keep' ? 'true' : 'false';
    item.append(path, action, ready);
    return item;
  });
  planEntries.replaceChildren(...nodes);
  const ready = plan.canBuild && plan.canWriteEnv && plan.canCleanup;
  planMessage.textContent = ready ? '预检通过，可以继续填写服务连接信息。' : '部分目录权限需要处理，尚未执行任何文件变更。';
  planMessage.dataset.tone = ready ? 'success' : 'error';
  planPanel.hidden = false;
  connectionPanel.hidden = !ready;
  updateApplyButton();
}

function yesNo(value) {
  return value ? '是' : '否';
}

function actionLabel(action) {
  return { keep: '保留', remove: '待移除', create: '待创建', write: '待写入' }[action] || '检查';
}

async function requestDependencyCheck(event, endpoint, resultElement) {
  event.preventDefault();
  const form = event.currentTarget;
  const submit = form.querySelector('button[type="submit"]');
  submit.disabled = true;
  submit.textContent = '测试中';
  resultElement.textContent = '正在建立临时连接。';
  resultElement.dataset.tone = 'pending';
  const formValues = Object.fromEntries(new FormData(form).entries());
  if (endpoint === databaseCheckEndpoint) {
    formValues.port = Number(formValues.port);
    if (!formValues.dsn) delete formValues.dsn;
  } else {
    formValues.db = Number(formValues.db || 0);
    formValues.addr = redisAddress.value.trim();
  }
  try {
    const response = await fetch(endpoint, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify(formValues),
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('dependency check failed');
    }
    const result = envelope.data;
    if (endpoint === databaseCheckEndpoint) databaseCheckPassed = Boolean(result.ok);
    if (endpoint === redisCheckEndpoint) redisCheckPassed = Boolean(result.ok);
    resultElement.textContent = result.ok
      ? `连接成功，耗时 ${Number(result.latencyMs || 0)} ms。`
      : '连接未成功，请检查地址和账号后重试。';
    resultElement.dataset.tone = result.ok ? 'success' : 'error';
  } catch {
    if (endpoint === databaseCheckEndpoint) databaseCheckPassed = false;
    if (endpoint === redisCheckEndpoint) redisCheckPassed = false;
    resultElement.textContent = '连接测试未完成，请检查服务状态和输入。';
    resultElement.dataset.tone = 'error';
  } finally {
    submit.disabled = false;
    submit.textContent = endpoint === databaseCheckEndpoint ? '测试数据库连接' : '测试 Redis 连接';
    updateApplyButton();
  }
}

function updateApplyButton() {
  applyButton.disabled = !currentPlan || !currentPlan.canBuild || !currentPlan.canWriteEnv || !currentPlan.canCleanup || !databaseCheckPassed || !redisCheckPassed;
}

function invalidateDependencyCheck(resultElement) {
  if (resultElement === databaseResult) databaseCheckPassed = false;
  if (resultElement === redisResult) redisCheckPassed = false;
  resultElement.textContent = '配置已变化，请重新测试。';
  resultElement.dataset.tone = 'pending';
  updateApplyButton();
}

function clearFailedJobActions() {
  retryJobId = null;
  rollbackJobId = null;
  rollbackButton.hidden = true;
  clearInstallationFailure();
}

function setFailedJobActions(job) {
  retryJobId = job.canRetry ? job.id : null;
  rollbackJobId = job.canRollback ? job.id : null;
  rollbackButton.hidden = !rollbackJobId;
}

function invalidatePlanIfModeChanged() {
  if (currentPlan && currentPlan.mode !== modeChoice.value) {
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
    clearFailedJobActions();
    planPanel.hidden = true;
    connectionPanel.hidden = true;
    planMessage.textContent = '选择已变更，请重新检查目录权限。';
    planMessage.dataset.tone = 'pending';
  }
  updateApplyButton();
}

function dependencyFormValues() {
  const database = Object.fromEntries(new FormData(databaseForm).entries());
  database.port = Number(database.port);
  if (!database.dsn) delete database.dsn;
  database.mode = database.mode || 'single';

  const redis = Object.fromEntries(new FormData(redisForm).entries());
  redis.mode = 'single';
  redis.db = Number(redis.db || 0);
  redis.addr = redisAddress.value.trim();
  return { database, redis };
}

function renderApplyResult(result) {
  setProgress(100, '安装完成');
  applyResult.textContent = '安装已完成。请重启服务端，然后依次运行 pnpm run build 和 pnpm run dev。';
  applyResult.dataset.tone = 'success';
  applyResult.setAttribute('role', 'status');
  applyResult.setAttribute('aria-live', 'polite');
  applyResult.focus();
  applySteps.replaceChildren();
  for (const step of Array.isArray(result.steps) ? result.steps : []) {
    const item = document.createElement('li');
    item.textContent = `${step.id || 'step'}：${step.status || 'completed'}`;
    applySteps.append(item);
  }
}

function clearStatusRefresh() {
  if (statusRefreshTimer !== null) {
    window.clearTimeout(statusRefreshTimer);
    statusRefreshTimer = null;
  }
}

function scheduleStatusRefresh() {
  clearStatusRefresh();
  statusRefreshTimer = window.setTimeout(loadStatus, 2000);
}

function renderJobProgress(job) {
  const progress = Math.max(0, Math.min(100, Number(job.progress || 0)));
  const step = stepLabels[job.currentStep] || '正在执行安装任务';
  setProgress(progress, `${step}，${progress}%`);
  applyResult.textContent = `${step}（${progress}%）`;
  applyResult.dataset.tone = job.state === 'failed' ? 'error' : job.state === 'completed' ? 'success' : 'pending';
  if (job.state !== 'failed') clearInstallationFailure();
  rollbackButton.hidden = !(job.state === 'failed' && job.canRollback);
  applySteps.replaceChildren();
  for (const completed of Array.isArray(job.steps) ? job.steps : []) {
    const item = document.createElement('li');
    item.textContent = `${stepLabels[completed.id] || completed.id}：已完成`;
    applySteps.append(item);
  }
}

function setProgress(value, description) {
  applyProgress.value = value;
  applyProgress.textContent = `${value}%`;
  applyProgress.setAttribute('aria-valuetext', description);
}

function wait(milliseconds) {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}

async function pollInstallation(jobId) {
  for (let attempt = 0; attempt < 1800; attempt += 1) {
    if (attempt > 0) await wait(1000);
    const response = await fetch(`${progressEndpoint}/${encodeURIComponent(jobId)}`, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('installation progress unavailable');
    }
    const job = envelope.data;
    renderJobProgress(job);
    if (job.state === 'completed' || job.state === 'failed') return job;
  }
  throw new Error('installation progress timed out');
}

function installationFailureMessage(job) {
  const databaseReasons = {
    tls_mode_mismatch: '数据库未启用 TLS，且数据库连接测试与迁移采用了不同模式；请统一 TLS 配置。当前输入已保留。',
    tls_configuration_failed: '数据库 TLS 证书、CA 或主机名校验失败，请检查加密连接配置。当前输入已保留。',
    authentication_failed: '数据库身份验证失败，请检查账号、密码和访问规则。当前输入已保留。',
    permission_denied: '数据库账号缺少建表、索引或约束变更权限。当前输入已保留。',
    database_unavailable: '数据库迁移连接不可用，请检查地址、服务状态及 TLS 模式。当前输入已保留。',
    database_busy: '数据库当前被锁定或事务发生冲突，请等待占用结束后重试。当前输入已保留。',
    schema_unavailable: '目标数据库 schema 不存在或当前账号不可访问。当前输入已保留。',
    schema_conflict: '目标数据库已存在冲突的表、字段或约束，请核对现有结构。当前输入已保留。',
    migration_dirty: '数据库迁移版本处于 dirty 状态，需要先核对失败版本再继续。当前输入已保留。',
    migration_statement_failed: '数据库迁移语句执行失败，请按任务 ID 和数据库代码定位。当前输入已保留。',
    migration_status_failed: '数据库迁移状态读取或校验失败，请按任务 ID 定位。当前输入已保留。',
    migration_close_failed: '数据库迁移已经执行，但连接收尾失败，请核对迁移状态后重试。当前输入已保留。',
    invalid_configuration: '数据库迁移配置不完整，请重新检查连接参数。当前输入已保留。',
    unknown: '数据库迁移出现未分类故障，请按任务 ID 在服务端日志中定位。当前输入已保留。',
  };
  if (databaseReasons[job?.failureReason]) return databaseReasons[job.failureReason];
  if (job?.errorKey === 'installation_running') {
    return '检测到另一项初始化或安装任务正在执行。若终端中的 pnpm run init 已结束，请重新运行 pnpm run init 继续恢复。当前输入已保留。';
  }
  if (job?.errorKey === 'installation_completed') {
    return '服务端已存在有效安装标记，请重新检查实例状态。当前输入已保留。';
  }
  if (job?.errorKey === 'invalid_request') {
    return '安装请求校验未通过，请检查运行方式、语言和管理员配置。当前输入已保留。';
  }
  if (job?.errorKey === 'validation_failed') {
    const reasons = {
      plan: '安装方案或目录预检未通过，请重新检查目录权限。当前输入已保留。',
      database: '数据库连接复核未通过，请检查数据库服务和当前配置。当前输入已保留。',
      redis: 'Redis 连接复核未通过，请检查 Redis 服务和当前配置。当前输入已保留。',
      journal: '安装事务状态校验未通过，请保留当前目录并重新检查初始化状态。当前输入已保留。',
      coordination: '安装执行权释放或校验未完成，请稍后重试。当前输入已保留。',
    };
    return reasons[job?.failureStep] || '安装前置校验未通过，请根据失败位置检查配置。当前输入已保留。';
  }
  if (job?.errorKey === 'request_unavailable') {
    return '安装请求或进度查询未完成，请确认服务端仍在运行。当前输入已保留。';
  }
  if (job?.errorKey === 'internal_error') {
    const reasons = {
      schema: '数据库结构迁移执行失败，请查看失败任务定位信息。当前输入已保留。',
      identity: '初始管理员创建失败，请查看失败任务定位信息。当前输入已保留。',
      environment: '运行配置写入失败，请检查目录权限。当前输入已保留。',
      journal: '安装事务记录写入失败，请检查安装状态目录。当前输入已保留。',
      marker: '安装标记生成失败，请检查安装配置。当前输入已保留。',
      lock: '安装锁写入失败，请检查安装状态目录。当前输入已保留。',
      recovery: '上次安装事务恢复失败，请查看服务端终端。当前输入已保留。',
    };
    return reasons[job?.failureStep] || '服务端安装执行失败，请查看失败任务定位信息。当前输入已保留。';
  }
  return '安装未完成，请根据失败位置和错误标识继续排查。当前输入已保留。';
}

function installationFailureDiagnostics(job) {
  const stage = job?.failureStep || job?.currentStep || '';
  return {
    reason: installationFailureMessage(job),
    step: stepLabels[stage] || stage || '未提供',
    errorCode: Number.isInteger(job?.errorCode) ? String(job.errorCode) : '—',
    errorKey: String(job?.errorKey || '未提供'),
    reasonKey: String(job?.failureReason || '未提供'),
    operation: String(job?.failureOperation || '未提供'),
    databaseCode: String(job?.databaseCode || '—'),
    jobId: String(job?.id || '未提供'),
  };
}

function renderInstallationFailure(job) {
  const diagnostics = installationFailureDiagnostics(job);
  failureReason.textContent = diagnostics.reason;
  failureStep.textContent = diagnostics.step;
  failureErrorCode.textContent = diagnostics.errorCode;
  failureErrorKey.textContent = diagnostics.errorKey;
  failureReasonKey.textContent = diagnostics.reasonKey;
  failureOperation.textContent = diagnostics.operation;
  failureDatabaseCode.textContent = diagnostics.databaseCode;
  failureJobId.textContent = diagnostics.jobId;
  installFailureDetails.hidden = false;
  installFailureDetails.focus();
}

function clearInstallationFailure() {
  installFailureDetails.hidden = true;
  for (const output of [failureReason, failureStep, failureErrorCode, failureErrorKey, failureReasonKey, failureOperation, failureDatabaseCode, failureJobId]) {
    output.textContent = '—';
  }
}

function announceApplyError(detail, diagnosticsAvailable = false) {
  applyResult.textContent = detail;
  applyResult.dataset.tone = 'error';
  applyResult.setAttribute('role', diagnosticsAvailable ? 'status' : 'alert');
  applyResult.setAttribute('aria-live', diagnosticsAvailable ? 'polite' : 'assertive');
  if (diagnosticsAvailable) return;
  applyResult.focus();
}

async function postInstallationRequest(targetEndpoint, payload, fetcher = fetch) {
  const response = await fetcher(targetEndpoint, {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      'Accept-Language': browserLanguageHeader(),
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
  });
  const envelope = await response.json();
  return { response, envelope };
}

async function submitInstallationRequest(payload, failedJobId, fetcher = fetch) {
  const targetEndpoint = failedJobId
    ? `${retryEndpoint}/${encodeURIComponent(failedJobId)}`
    : applyEndpoint;
  let outcome = await postInstallationRequest(targetEndpoint, payload, fetcher);
  if (failedJobId && outcome.response.status === 404 && outcome.envelope?.code === 30000) {
    clearFailedJobActions();
    outcome = await postInstallationRequest(applyEndpoint, payload, fetcher);
  }
  return outcome;
}

function installationCompletionDetected(envelope, job) {
  return envelope?.code === 10006 || job?.errorKey === 'installation_completed';
}

function commitCompletedInstallation(status, result) {
  clearFailedJobActions();
  clearInstallSecrets();
  if (result) renderApplyResult(result);
  renderStatus(status);
}

async function reconcileCompletedInstallation(statusReader = fetchInstallationStatus) {
  const status = await statusReader();
  if (!status?.installed) return false;
  commitCompletedInstallation(status);
  return true;
}

async function requestInstallation(event) {
  event.preventDefault();
  clearInstallationFailure();
  setProgress(0, '准备安装');
  if (!currentPlan || !databaseCheckPassed || !redisCheckPassed) {
    announceApplyError('请先完成目录、数据库和 Redis 检查。');
    return;
  }
  if (adminPassword.value !== adminPasswordConfirm.value) {
    announceApplyError('两次输入的管理员密码不一致。');
    adminPasswordConfirm.focus();
    return;
  }

  applyButton.disabled = true;
  applyButton.textContent = '安装中';
  applyResult.textContent = '服务端正在按顺序执行迁移、管理员初始化、配置写入和安装锁定。';
  applyResult.dataset.tone = 'pending';
  applyResult.setAttribute('role', 'status');
  applyResult.setAttribute('aria-live', 'polite');
  let installationCompleted = false;
  try {
    const dependencies = dependencyFormValues();
    const payload = {
      mode: modeChoice.value,
      localeMode: localeMode.value,
      locale: localeChoice.value,
      database: dependencies.database,
      redis: dependencies.redis,
      admin: { username: adminUsername.value.trim(), password: adminPassword.value },
    };
    const { response, envelope } = await submitInstallationRequest(payload, retryJobId);
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      if (installationCompletionDetected(envelope)) {
        installationCompleted = await reconcileCompletedInstallation();
        if (installationCompleted) return;
      }
      const failure = {
        id: envelope?.traceId || envelope?.meta?.requestId,
        failureStep: 'request',
        errorCode: Number.isInteger(envelope?.code) ? envelope.code : undefined,
        errorKey: envelope?.code === 10007 ? 'installation_running' : envelope?.code === 10006 ? 'installation_completed' : envelope?.code === 10000 ? 'invalid_request' : 'request_unavailable',
      };
      announceApplyError(installationFailureMessage(failure), true);
      renderInstallationFailure(failure);
      return;
    }
    let result = envelope.data;
    if (response.status === 202 && result.id) {
      renderJobProgress(result);
      result = await pollInstallation(result.id);
      if (result.state === 'failed') {
        if (installationCompletionDetected(null, result)) {
          installationCompleted = await reconcileCompletedInstallation();
          if (installationCompleted) return;
        }
        setFailedJobActions(result);
        announceApplyError(installationFailureMessage(result), true);
        renderInstallationFailure(result);
        return;
      }
    }
    const completedStatus = { installed: true, installerVersion: 'current', mode: result.mode };
    completedStatus.selectedUi = result.selectedUi;
    commitCompletedInstallation(completedStatus, result);
    installationCompleted = true;
  } catch {
    announceApplyError('安装请求未完成，请确认服务仍在运行。当前输入已保留；服务恢复后可直接重试，修改连接配置后再重新测试。', true);
    renderInstallationFailure({ failureStep: 'request', errorKey: 'request_unavailable' });
  } finally {
    if (!installationCompleted) {
      applyButton.textContent = retryJobId ? '重新尝试安装' : '开始安装';
      updateApplyButton();
    }
  }
}

async function requestRollback() {
  if (!rollbackJobId) return;
  rollbackButton.disabled = true;
  rollbackButton.textContent = '正在回滚';
  applyResult.textContent = '正在按安装清单恢复本次失败事务。';
  applyResult.dataset.tone = 'pending';
  try {
    const response = await fetch(`${rollbackEndpoint}/${encodeURIComponent(rollbackJobId)}`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ confirmRollback: true }),
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('rollback request failed');
    }
    renderJobProgress(envelope.data);
    clearInstallationFailure();
    applyResult.textContent = '本次失败事务已回滚，当前输入仍保留，可以直接重试或修改后再测试。';
    applyResult.dataset.tone = 'success';
    rollbackButton.hidden = true;
    rollbackJobId = null;
    retryJobId = envelope.data.canRetry ? envelope.data.jobId : null;
  } catch {
    applyResult.textContent = '回滚未完成，请保留当前安装目录并使用离线恢复流程。';
    applyResult.dataset.tone = 'error';
  } finally {
    rollbackButton.disabled = false;
    rollbackButton.textContent = '回滚本次失败的安装';
  }
}

function clearInstallSecrets() {
  clearSensitiveFields(databaseForm);
  clearSensitiveFields(redisForm);
  clearSensitiveFields(adminForm);
}

function clearSensitiveFields(form) {
  for (const input of form.querySelectorAll('input[type="password"]')) input.value = '';
}

async function loadAll() {
  await Promise.allSettled([loadStatus(), loadCapabilities()]);
}

retryButton.addEventListener('click', loadAll);
suggestBrowserLocale();
planForm.addEventListener('submit', requestPlan);
databaseForm.addEventListener('submit', (event) => requestDependencyCheck(event, databaseCheckEndpoint, databaseResult));
redisForm.addEventListener('submit', (event) => requestDependencyCheck(event, redisCheckEndpoint, redisResult));
adminForm.addEventListener('submit', requestInstallation);
rollbackButton.addEventListener('click', requestRollback);
databaseForm.addEventListener('input', () => invalidateDependencyCheck(databaseResult));
redisForm.addEventListener('input', () => invalidateDependencyCheck(redisResult));
modeChoice.addEventListener('change', invalidatePlanIfModeChanged);
databaseDriver.addEventListener('change', () => {
  databasePort.value = databaseDriver.value === 'postgres' ? '5432' : '3306';
});
loadAll();
