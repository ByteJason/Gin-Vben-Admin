const endpoint = '/api/system/install/v1/status';
const capabilitiesEndpoint = '/api/system/install/v1/capabilities';
const planEndpoint = '/api/system/install/v1/plan';
const databaseCheckEndpoint = '/api/system/install/v1/check/database';
const redisCheckEndpoint = '/api/system/install/v1/check/redis';
const applyEndpoint = '/api/system/install/v1/apply';
const progressEndpoint = '/api/system/install/v1/progress';

const title = document.querySelector('#status-title');
const badge = document.querySelector('#status-badge');
const message = document.querySelector('#status-message');
const details = document.querySelector('#status-details');
const installerVersion = document.querySelector('#installer-version');
const selectedUi = document.querySelector('#selected-ui');
const selectedMode = document.querySelector('#selected-mode');
const retryButton = document.querySelector('#retry-button');
const platformSummary = document.querySelector('#platform-summary');
const capabilityList = document.querySelector('#capability-list');
const selectionPanel = document.querySelector('#selection-panel');
const planForm = document.querySelector('#plan-form');
const uiChoice = document.querySelector('#ui-choice');
const modeChoice = document.querySelector('#mode-choice');
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
const confirmCleanup = document.querySelector('#confirm-cleanup');
const applyButton = document.querySelector('#apply-button');
const applyResult = document.querySelector('#apply-result');
const applyProgress = document.querySelector('#apply-progress');
const applySteps = document.querySelector('#apply-steps');

const uiLabels = { antd: 'Ant Design Vue', ele: 'Element Plus', naive: 'Naive UI' };
const modeLabels = {
  embedded: '嵌入式单包',
  standalone: '静态资源独立部署',
  api_only: '仅 API',
  dev: '开发调试',
};
const stepLabels = {
  queued: '等待执行',
  plan: '复核目录权限',
  database: '验证数据库',
  redis: '验证 Redis',
  schema: '执行数据库迁移',
  assets: '构建并暂存界面资源',
  identity: '初始化管理员',
  environment: '写入运行配置',
  lock: '写入安装锁',
  complete: '安装完成',
  failed: '安装失败',
};

let currentPlan = null;
let databaseCheckPassed = false;
let redisCheckPassed = false;

async function loadStatus() {
  setPending();
  try {
    const response = await fetch(endpoint, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('status request failed');
    }
    renderStatus(envelope.data);
  } catch {
    renderError();
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
  title.textContent = '正在检查安装状态';
  badge.textContent = '检查中';
  badge.dataset.tone = 'pending';
  message.textContent = '正在连接本机安装服务，请稍候。';
  message.setAttribute('aria-live', 'polite');
  details.hidden = true;
  retryButton.hidden = true;
}

function renderStatus(status) {
  installerVersion.textContent = status.installerVersion || '—';
  details.hidden = false;
  retryButton.hidden = true;
  message.setAttribute('aria-live', 'polite');

  if (status.installed) {
    title.textContent = '系统已完成安装';
    badge.textContent = '已安装';
    badge.dataset.tone = 'success';
    message.textContent = '当前实例已锁定安装流程，可以前往管理端登录。';
    selectedUi.textContent = uiLabels[status.selectedUi] || status.selectedUi || '—';
    selectedMode.textContent = modeLabels[status.mode] || status.mode || '—';
    selectionPanel.hidden = true;
    connectionPanel.hidden = true;
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
    return;
  }

  title.textContent = '安装服务已就绪';
  badge.textContent = '待安装';
  badge.dataset.tone = 'ready';
  message.textContent = '本机安装状态可用，接下来将检查运行环境和目录权限。';
  selectedUi.textContent = '尚未选择';
  selectedMode.textContent = '尚未选择';
  selectionPanel.hidden = false;
  connectionPanel.hidden = true;
  currentPlan = null;
  databaseCheckPassed = false;
  redisCheckPassed = false;
  updateApplyButton();
}

function renderError() {
  title.textContent = '安装服务暂不可用';
  badge.textContent = '检查失败';
  badge.dataset.tone = 'error';
  message.textContent = '请确认服务已启动，然后重新检查。';
  message.setAttribute('aria-live', 'assertive');
  details.hidden = true;
  retryButton.hidden = false;
  selectionPanel.hidden = true;
  connectionPanel.hidden = true;
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
  updateApplyButton();
  planButton.disabled = true;
  planButton.textContent = '正在检查';
  planMessage.textContent = '正在验证目录的读取、写入、创建、重命名与删除能力。';
  planMessage.dataset.tone = 'pending';
  planPanel.hidden = true;
  try {
    const selectedUi = uiChoice.value;
    const mode = modeChoice.value;
    const response = await fetch(planEndpoint, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ selectedUi, mode }),
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('plan request failed');
    }
    renderPlan(envelope.data);
  } catch {
    planMessage.textContent = '目录预检未完成，请检查服务与目录权限后重试。';
    planMessage.dataset.tone = 'error';
  } finally {
    planButton.disabled = false;
    planButton.textContent = '检查目录权限';
  }
}

function renderPlan(plan) {
  currentPlan = plan;
  databaseCheckPassed = false;
  redisCheckPassed = false;
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
  applyButton.disabled = !currentPlan || !currentPlan.canBuild || !currentPlan.canWriteEnv || !currentPlan.canCleanup || !databaseCheckPassed || !redisCheckPassed || !confirmCleanup.checked;
}

function invalidatePlanIfSelectionChanged() {
  if (currentPlan && (currentPlan.selectedUi !== uiChoice.value || currentPlan.mode !== modeChoice.value)) {
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
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
  applyProgress.value = 100;
  applyProgress.textContent = '100%';
  applyResult.textContent = '安装已完成。请按提示重启服务后进入管理端。';
  applyResult.dataset.tone = 'success';
  applySteps.replaceChildren();
  for (const step of Array.isArray(result.steps) ? result.steps : []) {
    const item = document.createElement('li');
    item.textContent = `${step.id || 'step'}：${step.status || 'completed'}`;
    applySteps.append(item);
  }
}

function renderJobProgress(job) {
  const progress = Math.max(0, Math.min(100, Number(job.progress || 0)));
  const step = stepLabels[job.currentStep] || '正在执行安装任务';
  applyProgress.value = progress;
  applyProgress.textContent = `${progress}%`;
  applyResult.textContent = `${step}（${progress}%）`;
  applyResult.dataset.tone = job.state === 'failed' ? 'error' : job.state === 'completed' ? 'success' : 'pending';
  applySteps.replaceChildren();
  for (const completed of Array.isArray(job.steps) ? job.steps : []) {
    const item = document.createElement('li');
    item.textContent = `${stepLabels[completed.id] || completed.id}：已完成`;
    applySteps.append(item);
  }
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

function applyErrorMessage(status) {
  if (status === 409) return '安装已完成或已有安装事务正在执行。';
  if (status === 422) return '安装前置检查未通过，请返回检查目录和依赖。';
  if (status === 503) return '安装服务暂不可用，请确认服务端处于源码安装模式。';
  return '安装未完成，服务端已保留可回滚状态，请检查后重试。';
}

async function requestInstallation(event) {
  event.preventDefault();
  applyProgress.value = 0;
  applyProgress.textContent = '0%';
  if (!currentPlan || !databaseCheckPassed || !redisCheckPassed) {
    applyResult.textContent = '请先完成目录、数据库和 Redis 检查。';
    applyResult.dataset.tone = 'error';
    return;
  }
  if (adminPassword.value !== adminPasswordConfirm.value) {
    applyResult.textContent = '两次输入的管理员密码不一致。';
    applyResult.dataset.tone = 'error';
    adminPasswordConfirm.focus();
    return;
  }

  applyButton.disabled = true;
  applyButton.textContent = '安装中';
  applyResult.textContent = '服务端正在按顺序执行迁移、管理员初始化、配置写入和安装锁定。';
  applyResult.dataset.tone = 'pending';
  try {
    const dependencies = dependencyFormValues();
    const response = await fetch(applyEndpoint, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({
        selectedUi: uiChoice.value,
        mode: modeChoice.value,
        database: dependencies.database,
        redis: dependencies.redis,
        admin: { username: adminUsername.value.trim(), password: adminPassword.value },
        confirmCleanup: confirmCleanup.checked,
      }),
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      applyResult.textContent = applyErrorMessage(response.status);
      applyResult.dataset.tone = 'error';
      return;
    }
    let result = envelope.data;
    if (response.status === 202 && result.id) {
      clearInstallSecrets();
      renderJobProgress(result);
      result = await pollInstallation(result.id);
      if (result.state === 'failed') {
        applyResult.textContent = result.canRetry
          ? '安装未完成，已自动回滚本次副作用。请重新输入凭据后重试。'
          : '安装未完成，请检查实例状态后再继续。';
        applyResult.dataset.tone = 'error';
        return;
      }
    }
    renderApplyResult(result);
    renderStatus({
      installed: true,
      installerVersion: 'current',
      selectedUi: result.selectedUi,
      mode: result.mode,
    });
    currentPlan = { ...currentPlan, installed: true };
  } catch {
    applyResult.textContent = '安装请求未完成，请确认服务仍在运行后重试。';
    applyResult.dataset.tone = 'error';
  } finally {
    clearInstallSecrets();
    if (!currentPlan || !currentPlan.installed) {
      applyButton.disabled = false;
      applyButton.textContent = '开始安装';
      updateApplyButton();
    }
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
planForm.addEventListener('submit', requestPlan);
databaseForm.addEventListener('submit', (event) => requestDependencyCheck(event, databaseCheckEndpoint, databaseResult));
redisForm.addEventListener('submit', (event) => requestDependencyCheck(event, redisCheckEndpoint, redisResult));
adminForm.addEventListener('submit', requestInstallation);
databaseForm.addEventListener('input', () => { databaseCheckPassed = false; updateApplyButton(); });
redisForm.addEventListener('input', () => { redisCheckPassed = false; updateApplyButton(); });
uiChoice.addEventListener('change', invalidatePlanIfSelectionChanged);
modeChoice.addEventListener('change', invalidatePlanIfSelectionChanged);
confirmCleanup.addEventListener('change', updateApplyButton);
databaseDriver.addEventListener('change', () => {
  databasePort.value = databaseDriver.value === 'postgres' ? '5432' : '3306';
});
loadAll();
