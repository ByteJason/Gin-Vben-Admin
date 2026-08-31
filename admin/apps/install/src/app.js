const endpoint = '/api/system/install/v1/status';
const capabilitiesEndpoint = '/api/system/install/v1/capabilities';
const planEndpoint = '/api/system/install/v1/plan';
const databaseCheckEndpoint = '/api/system/install/v1/check/database';
const redisCheckEndpoint = '/api/system/install/v1/check/redis';
const applyEndpoint = '/api/system/install/v1/apply';
const progressEndpoint = '/api/system/install/v1/progress';
const retryEndpoint = '/api/system/install/v1/retry';
const rollbackEndpoint = '/api/system/install/v1/rollback';
const uiPrepareEndpoint = '/api/system/install/v1/ui/prepare';
const uiProgressEndpoint = '/api/system/install/v1/ui/progress';
const uiResetEndpoint = '/api/system/install/v1/ui/reset';
const missingUIToolsMessage =
  '准备管理界面需要 Node.js ^22.18.0 或 ^24.12.0，以及 pnpm >=11.0.0；升级后重新检查运行能力。';
const installationCompletedMessage =
  '安装已完成。请停止旧服务端，回到仓库根目录，并按下方两个终端命令分别重启服务端和启动管理端；管理端先运行 pnpm run ui:install 安装所选工作区闭包，再运行 pnpm run dev。';

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
const uiPreparePanel = document.querySelector('#ui-prepare-panel');
const uiPrepareTitle = document.querySelector('#ui-prepare-title');
const uiPrepareHint = document.querySelector('#ui-prepare-hint');
const uiPrepareForm = document.querySelector('#ui-prepare-form');
const uiChoice = document.querySelector('#ui-choice');
const uiChoiceInputs = [
  ...uiChoice.querySelectorAll('input[name="selectedUi"]'),
];
const confirmCleanup = document.querySelector('#confirm-cleanup');
const prepareUIButton = document.querySelector('#prepare-ui-button');
const resumeUIResetButton = document.querySelector('#resume-ui-reset-button');
const uiPrepareResult = document.querySelector('#ui-prepare-result');
const uiPrepareProgressPanel = document.querySelector(
  '#ui-prepare-progress-panel',
);
const uiPrepareProgress = document.querySelector('#ui-prepare-progress');
const uiPrepareDiagnostics = document.querySelector('#ui-prepare-diagnostics');
const uiPrepareJobId = document.querySelector('#ui-prepare-job-id');
const uiPrepareFailureStep = document.querySelector('#ui-prepare-failure-step');
const uiPrepareFailureReason = document.querySelector(
  '#ui-prepare-failure-reason',
);
const uiPrepareFailureScope = document.querySelector(
  '#ui-prepare-failure-scope',
);
const uiPrepareFailureOperation = document.querySelector(
  '#ui-prepare-failure-operation',
);
const uiPrepareErrorKey = document.querySelector('#ui-prepare-error-key');
const uiPrepareLogItem = document.querySelector('#ui-prepare-log-item');
const uiPrepareLogPath = document.querySelector('#ui-prepare-log-path');
const selectionPanel = document.querySelector('#selection-panel');
const resetUIButton = document.querySelector('#reset-ui-button');
const planForm = document.querySelector('#plan-form');
const modeChoice = document.querySelector('#mode-choice');
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
const installFailureDetails = document.querySelector(
  '#install-failure-details',
);
const failureReason = document.querySelector('#failure-reason');
const failureStep = document.querySelector('#failure-step');
const failureErrorCode = document.querySelector('#failure-error-code');
const failureErrorKey = document.querySelector('#failure-error-key');
const failureReasonKey = document.querySelector('#failure-reason-key');
const failureOperation = document.querySelector('#failure-operation');
const failureDatabaseCode = document.querySelector('#failure-database-code');
const failureResourceKind = document.querySelector('#failure-resource-kind');
const failureResourceId = document.querySelector('#failure-resource-id');
const failureJobId = document.querySelector('#failure-job-id');
const rollbackButton = document.querySelector('#rollback-button');
const applyProgress = document.querySelector('#apply-progress');
const applySteps = document.querySelector('#apply-steps');
const nextSteps = document.querySelector('#next-steps');

const uiLabels = {
  antd: 'Ant Design Vue',
  ele: 'Element Plus',
  naive: 'Naive UI',
};
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
const uiStepLabels = {
  queued: '等待准备界面',
  request: '提交或读取任务状态',
  launch: '启动本机准备命令',
  preflight: '检查模板与目录',
  workspace: '暂存模板并写入界面配置',
  dependencies: '安装所选界面依赖',
  reset: '恢复全部界面模板',
  complete: '界面准备完成',
  failed: '界面准备失败',
};
// `uiStepLabels` remains the wire-compatible vocabulary consumed by older
// status fixtures. The public copy below describes the workspace model: the
// workspace step writes only ignored metadata and never moves source trees.
const uiStepDisplayLabels = {
  ...uiStepLabels,
  workspace: '保留三套源码并写入本机 profile',
  reset: '清除本机选择（不改动源码）',
};
const uiFailureReasonLabels = {
  api_unavailable: '本机服务端自检接口不可访问',
  process_failed: '本机准备进程未正常启动或退出',
  dependency_install_failed: '所选界面的依赖安装失败',
  dependency_transaction_invalid: '依赖安装完成后的状态校验失败',
  init_busy: '已有初始化或安装任务正在执行',
  init_lease_failed: '初始化锁或心跳文件无法创建',
  install_state_dir_invalid: '初始化状态目录无效或不可访问',
  node_version_unsupported: 'Node.js 版本不满足项目要求',
  pnpm_version_unsupported: 'pnpm 版本不满足项目要求（需要 11 或更高版本）',
  preflight_failed: '目录或文件操作能力预检失败',
  request_unavailable: '界面任务状态暂时不可读取',
  reset_in_progress: '检测到未完成的管理界面重置任务',
  state_inconsistent: '初始化状态不一致，需要先按提示恢复',
  initialization_in_progress: '初始化事务仍在执行或等待恢复',
  initialization_operation_failed:
    '初始化文件操作失败，权限或文件占用可能在预检后发生了变化',
  template_layout_invalid: '三套管理界面源码结构不完整',
};
const uiFailureScopeLabels = {
  admin_apps: '管理界面源码父目录',
  admin_root: '管理端根目录（admin）',
  selected_ui: '所选管理界面目录',
  state_root: '初始化状态目录（.runtime/install）',
  ui_backup: '旧版界面迁移状态目录',
};
const uiFailureOperationLabels = {
  create: '创建临时文件或目录',
  cross_directory_rename: '跨目录重命名（需位于同一磁盘卷）',
  delete: '删除临时文件或目录',
  execute: '进入并编辑目录',
  link: '创建原子发布所需的硬链接',
  lock: '创建初始化锁',
  read: '读取文件或目录',
  rename: '重命名文件或目录',
  sync: '同步文件或目录元数据',
  write: '写入并同步文件',
};

let currentPlan = null;
let databaseCheckPassed = false;
let redisCheckPassed = false;
let retryJobId = null;
let rollbackJobId = null;
let statusRefreshTimer = null;
let uiCapabilitiesLoaded = false;
let requiredUIToolsAvailable = false;
let uiActionPending = false;
let uiSelectionLocked = false;

function browserLanguageHeader() {
  const languages =
    Array.isArray(navigator.languages) && navigator.languages.length
      ? navigator.languages
      : [navigator.language || 'en-US'];
  return languages.join(',');
}

function suggestBrowserLocale() {
  const browserLocale = /^zh(?:-|$)/i.test(navigator.language || '')
    ? 'zh-CN'
    : 'en-US';
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
  uiCapabilitiesLoaded = false;
  requiredUIToolsAvailable = false;
  updateUIPrepareButton();
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
    uiCapabilitiesLoaded = true;
    requiredUIToolsAvailable = false;
    platformSummary.textContent = '未识别';
    capabilityList.replaceChildren(
      createCapabilityMessage('暂未读取到运行工具信息'),
    );
    updateUIPrepareButton();
    announceMissingUITools();
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
  uiPreparePanel.hidden = true;
  selectionPanel.hidden = true;
  connectionPanel.hidden = true;
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
    message.textContent = installationCompletedMessage;
    selectedUi.textContent =
      uiLabels[status.selectedUi] || status.selectedUi || '—';
    selectedUiSummary.textContent = selectedUi.textContent;
    selectedMode.textContent = modeLabels[status.mode] || status.mode || '—';
    uiPreparePanel.hidden = true;
    uiPrepareForm.hidden = false;
    resumeUIResetButton.hidden = true;
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
    if (status.phase === 'ui_prepare') {
      renderRecoverableUIPreparation(status);
      return;
    }
    title.textContent = '安装任务正在执行';
    badge.textContent = '安装中';
    badge.dataset.tone = 'pending';
    message.textContent =
      '安装事务正在执行；若服务中断，重新启动服务端并在此页重新提交即可恢复。';
    selectedUi.textContent =
      uiLabels[status.selectedUi] || status.selectedUi || '—';
    selectedUiSummary.textContent = selectedUi.textContent;
    selectedMode.textContent = '安装中';
    uiPreparePanel.hidden = true;
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
    title.textContent = '选择管理界面';
    badge.textContent = '等待选择';
    badge.dataset.tone = 'ready';
    message.textContent =
      '请选择一套管理界面。确认后只写入本机 profile；开发仓库始终保留三套源码，依赖按所选工作区闭包安装。';
    details.hidden = true;
    uiPrepareTitle.textContent = '选择管理界面';
    uiPrepareHint.textContent = '按所选工作区闭包安装依赖';
    uiPreparePanel.hidden = false;
    uiPrepareForm.hidden = false;
    resumeUIResetButton.hidden = true;
    selectionPanel.hidden = true;
    connectionPanel.hidden = true;
    currentPlan = null;
    databaseCheckPassed = false;
    redisCheckPassed = false;
    clearFailedJobActions();
    retryButton.hidden = true;
    uiSelectionLocked = false;
    selectUIChoice('');
    confirmCleanup.checked = false;
    uiPrepareProgressPanel.hidden = true;
    uiPrepareDiagnostics.hidden = true;
    setUIActionPending(false);
    updateUIPrepareButton();
    announceMissingUITools();
    title.focus();
    return;
  }

  if (status.state === 'inconsistent') {
    renderError(
      '检测到未完成的初始化状态。请保留当前目录和运行现场，并在 /install 点击“重新检查”继续恢复；维护诊断可运行 pnpm run init -- --check。',
    );
    return;
  }

  title.textContent = '安装服务已就绪';
  badge.textContent = '待安装';
  badge.dataset.tone = 'ready';
  message.textContent = '本机安装状态可用，接下来将检查运行环境和目录权限。';
  selectedUi.textContent =
    uiLabels[status.selectedUi] || status.selectedUi || '—';
  selectedUiSummary.textContent = selectedUi.textContent;
  selectedMode.textContent = '尚未选择';
  uiPreparePanel.hidden = true;
  uiPrepareForm.hidden = false;
  resumeUIResetButton.hidden = true;
  selectionPanel.hidden = false;
  connectionPanel.hidden = true;
  currentPlan = null;
  databaseCheckPassed = false;
  redisCheckPassed = false;
  clearFailedJobActions();
  updateUIPrepareButton();
  updateApplyButton();
  announceMissingUITools();
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
  uiPreparePanel.hidden = true;
  selectionPanel.hidden = true;
  connectionPanel.hidden = true;
  nextSteps.hidden = true;
  title.focus();
}

function renderCapabilities(capabilities) {
  const platform = capabilities.platform || {};
  platformSummary.textContent =
    [platform.os, platform.arch].filter(Boolean).join(' / ') || '未识别';
  const items = Array.isArray(capabilities.tools) ? capabilities.tools : [];
  uiCapabilitiesLoaded = true;
  requiredUIToolsAvailable = ['node', 'pnpm'].every((id) =>
    items.some((tool) => tool.id === id && tool.available && tool.compatible),
  );
  const nodes = items.map((tool) => {
    const item = document.createElement('li');
    const name = document.createElement('strong');
    const state = document.createElement('span');
    name.textContent = String(tool.id || 'tool').toUpperCase();
    const unsupported =
      tool.available &&
      tool.compatible === false &&
      tool.reason === 'version_unsupported';
    state.textContent = !tool.available
      ? '未检测到'
      : unsupported
        ? `${tool.version || '未知版本'}（需要 ${tool.requiredVersion || '受支持版本'}）`
        : tool.version || '可用';
    state.dataset.available =
      tool.available && tool.compatible ? 'true' : 'false';
    item.append(name, state);
    return item;
  });
  capabilityList.replaceChildren(
    ...(nodes.length ? nodes : [createCapabilityMessage('未返回工具信息')]),
  );
  updateUIPrepareButton();
  if (requiredUIToolsAvailable) clearMissingUIToolsMessage();
  announceMissingUITools();
}

function createCapabilityMessage(value) {
  const item = document.createElement('li');
  item.className = 'capability-placeholder';
  item.textContent = value;
  return item;
}

function announceMissingUITools() {
  if (!uiCapabilitiesLoaded || requiredUIToolsAvailable) return;
  if (uiPreparePanel.hidden && selectionPanel.hidden) return;
  retryButton.hidden = false;
  if (uiPreparePanel.hidden) return;
  showUIActionMessage(missingUIToolsMessage, 'error');
}

function clearMissingUIToolsMessage() {
  if (uiPrepareResult.textContent !== missingUIToolsMessage) return;
  uiPrepareResult.textContent = '';
  delete uiPrepareResult.dataset.tone;
}

function selectedUIChoice() {
  return uiChoiceInputs.find((input) => input.checked)?.value || '';
}

function selectUIChoice(value) {
  for (const input of uiChoiceInputs) input.checked = input.value === value;
}

function updateUIPrepareButton() {
  prepareUIButton.disabled =
    uiActionPending ||
    !requiredUIToolsAvailable ||
    !selectedUIChoice() ||
    !confirmCleanup.checked;
  resetUIButton.disabled = uiActionPending || !requiredUIToolsAvailable;
  resumeUIResetButton.disabled = uiActionPending || !requiredUIToolsAvailable;
}

function setUIActionPending(pending) {
  uiActionPending = Boolean(pending);
  for (const input of uiChoiceInputs)
    input.disabled = uiActionPending || uiSelectionLocked;
  confirmCleanup.disabled = uiActionPending;
  retryButton.disabled = uiActionPending;
  prepareUIButton.textContent = uiActionPending
    ? '正在准备管理界面'
    : uiSelectionLocked
      ? '继续准备此界面'
      : '确认并准备此界面';
  updateUIPrepareButton();
}

function showUIActionMessage(value, tone = 'pending') {
  if (uiPrepareResult.textContent !== value)
    uiPrepareResult.textContent = value;
  if (uiPrepareResult.dataset.tone !== tone)
    uiPrepareResult.dataset.tone = tone;
}

function setUIActionProgress(value, description) {
  const progress = Math.max(0, Math.min(100, Number(value || 0)));
  uiPrepareProgressPanel.hidden = false;
  uiPrepareProgress.value = progress;
  uiPrepareProgress.textContent = `${progress}%`;
  uiPrepareProgress.setAttribute('aria-valuetext', description);
}

function safeUIActionLogPath(value) {
  if (typeof value !== 'string' || value.length > 240) return '—';
  const normalized = value.replaceAll('\\', '/');
  return normalized === '.runtime/install/dependency-install.log'
    ? normalized
    : '—';
}

function renderUIActionProgress(job) {
  const action = job?.action === 'reset' ? '重置管理界面' : '准备管理界面';
  const progress = Math.max(0, Math.min(100, Number(job?.progress || 0)));
  const step = uiStepDisplayLabels[job?.currentStep] || '正在执行界面任务';
  const updated = job?.lastUpdated ? `；更新于 ${String(job.lastUpdated)}` : '';
  setUIActionProgress(progress, `${action}：${step}，${progress}%`);
  showUIActionMessage(
    `${action}：${step}（${progress}%）${updated}`,
    'pending',
  );
  if (job?.selectedUi) {
    selectUIChoice(job.selectedUi);
    selectedUi.textContent = uiLabels[job.selectedUi] || job.selectedUi;
  }
  if (job?.state === 'failed') {
    const failedStep =
      uiStepDisplayLabels[job.failureStep] ||
      job.failureStep ||
      '未识别阶段';
    const failedReason =
      uiFailureReasonLabels[job.failureReason] ||
      job.failureReason ||
      '未返回稳定失败原因';
    const failureScope =
      uiFailureScopeLabels[job.failureScope] || job.failureScope || '—';
    const failureOperation =
      uiFailureOperationLabels[job.failureOperation] ||
      job.failureOperation ||
      '—';
    showUIActionMessage(
      `${action}失败：${failedReason}。请修正后重试。`,
      'error',
    );
    uiPrepareJobId.textContent = String(job.id || '—');
    uiPrepareFailureStep.textContent = failedStep;
    uiPrepareFailureReason.textContent = failedReason;
    uiPrepareFailureScope.textContent = failureScope;
    uiPrepareFailureOperation.textContent = failureOperation;
    uiPrepareErrorKey.textContent = String(job.errorKey || 'unknown');
    const logPath =
      job.failureStep === 'dependencies'
        ? safeUIActionLogPath(job.logPath)
        : '—';
    uiPrepareLogPath.textContent = logPath;
    uiPrepareLogItem.hidden = logPath === '—';
    uiPrepareDiagnostics.hidden = false;
    return;
  }
  uiPrepareDiagnostics.hidden = true;
  uiPrepareJobId.textContent = '—';
  uiPrepareFailureStep.textContent = '—';
  uiPrepareFailureReason.textContent = '—';
  uiPrepareFailureScope.textContent = '—';
  uiPrepareFailureOperation.textContent = '—';
  uiPrepareErrorKey.textContent = '—';
  uiPrepareLogPath.textContent = '—';
  uiPrepareLogItem.hidden = true;
  if (job?.state === 'completed' || job?.state === 'succeeded') {
    setUIActionProgress(100, `${action}完成，100%`);
    showUIActionMessage(`${action}已完成。`, 'success');
  }
}

function completedUIAction(job) {
  return job?.state === 'completed' || job?.state === 'succeeded';
}

function renderUIRequestUnavailable(action, selectedUiChoice = '') {
  const label = action === 'reset' ? '重置管理界面' : '准备管理界面';
  if (selectedUiChoice) {
    selectUIChoice(selectedUiChoice);
    selectedUi.textContent =
      uiLabels[selectedUiChoice] || selectedUiChoice;
  }
  showUIActionMessage(
    `${label}的任务状态暂时不可读取；任务可能仍在执行，请重新检查。`,
    'error',
  );
  uiPrepareJobId.textContent = '—';
  uiPrepareFailureStep.textContent = '任务请求或进度读取';
  uiPrepareFailureReason.textContent =
    uiFailureReasonLabels.request_unavailable;
  uiPrepareFailureScope.textContent = '—';
  uiPrepareFailureOperation.textContent = '—';
  uiPrepareErrorKey.textContent = 'request_unavailable';
  uiPrepareLogPath.textContent = '—';
  uiPrepareLogItem.hidden = true;
  uiPrepareDiagnostics.hidden = false;
}

async function postUIActionRequest(target, payload, fetcher = fetch) {
  const response = await fetcher(target, {
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

async function pollUIAction(jobId, fetcher = fetch, sleeper = wait) {
  for (let attempt = 0; attempt < 1800; attempt += 1) {
    if (attempt > 0) await sleeper(1000);
    const response = await fetcher(
      `${uiProgressEndpoint}/${encodeURIComponent(jobId)}`,
      {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      },
    );
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('ui progress unavailable');
    }
    const job = envelope.data;
    renderUIActionProgress(job);
    if (completedUIAction(job) || job.state === 'failed') return job;
  }
  throw new Error('ui progress timed out');
}

async function finishUIAction(job) {
  renderUIActionProgress(job);
  if (job.state === 'failed') {
    const resetting = job.action === 'reset';
    uiSelectionLocked = !resetting && Boolean(job.selectedUi);
    uiPrepareForm.hidden = resetting;
    resumeUIResetButton.hidden = !resetting;
    retryButton.hidden = false;
    if (resetting) {
      uiPrepareTitle.textContent = '继续清除本机选择';
      uiPrepareHint.textContent = '清除本机选择（三套源码保持不变）';
    }
    setUIActionPending(false);
    uiPrepareResult.focus();
    return false;
  }
  if (!completedUIAction(job)) throw new Error('ui action incomplete');
  setUIActionPending(false);
  await loadStatus();
  return true;
}

async function runUIAction(target, payload) {
  const { response, envelope } = await postUIActionRequest(target, payload);
  const accepted = envelope?.data;
  const jobId = accepted?.id || accepted?.jobId;
  if (response.status !== 202 || envelope?.code !== 0 || !accepted || !jobId) {
    const failure = {
      action: target === uiResetEndpoint ? 'reset' : 'prepare',
      state: 'failed',
      selectedUi: payload.selectedUi,
      currentStep: 'request',
      failureStep: 'request',
      failureReason: 'request_unavailable',
      errorKey:
        accepted?.errorKey || envelope?.errorKey || 'request_unavailable',
      logPath: accepted?.logPath,
    };
    return finishUIAction(failure);
  }
  renderUIActionProgress(accepted);
  const completed = await pollUIAction(jobId);
  return finishUIAction(completed);
}

async function requestUIPreparation(event) {
  event.preventDefault();
  if (uiActionPending) return;
  const selectedUi = selectedUIChoice();
  if (!requiredUIToolsAvailable) {
    showUIActionMessage(missingUIToolsMessage, 'error');
    uiPrepareResult.focus();
    return;
  }
  if (!selectedUi || !confirmCleanup.checked) {
    showUIActionMessage('请选择管理界面并确认本机选择方案（不会删除三套源码）。', 'error');
    uiPrepareResult.focus();
    return;
  }
  uiSelectionLocked = true;
  setUIActionPending(true);
  uiPrepareDiagnostics.hidden = true;
  showUIActionMessage('正在提交界面准备任务。', 'pending');
  try {
    await runUIAction(uiPrepareEndpoint, {
      selectedUi: selectedUi,
      confirmCleanup: true,
    });
  } catch {
    renderUIRequestUnavailable('prepare', selectedUi);
    retryButton.hidden = false;
    setUIActionPending(false);
    uiPrepareResult.focus();
  }
}

async function requestUIReset(confirmFirst = true) {
  if (uiActionPending) return;
  if (!requiredUIToolsAvailable) {
    showUIActionMessage(missingUIToolsMessage, 'error');
    uiPrepareResult.focus();
    return;
  }
  if (
    confirmFirst &&
    !window.confirm(
      '确认清除本机 UI 选择？三套源码不会改动；数据库安装尚未开始。',
    )
  )
    return;
  uiPrepareTitle.textContent = '重置管理界面';
  uiPrepareHint.textContent = '清除本机选择（三套源码保持不变）';
  uiPreparePanel.hidden = false;
  uiPrepareForm.hidden = true;
  resumeUIResetButton.hidden = true;
  selectionPanel.hidden = true;
  connectionPanel.hidden = true;
  nextSteps.hidden = true;
  setUIActionPending(true);
  showUIActionMessage('正在提交界面重置任务。', 'pending');
  uiPrepareResult.focus();
  try {
    await runUIAction(uiResetEndpoint, { confirmReset: true });
  } catch {
    renderUIRequestUnavailable('reset');
    uiPrepareTitle.textContent = '继续清除本机选择';
    uiPrepareHint.textContent = '清除本机选择（三套源码保持不变）';
    resumeUIResetButton.hidden = false;
    retryButton.hidden = false;
    setUIActionPending(false);
    uiPrepareResult.focus();
  }
}

function renderRecoverableUIPreparation(status) {
  clearStatusRefresh();
  const recoveringReset = status.uiAction === 'reset';
  title.textContent = recoveringReset
    ? '继续清除本机选择'
    : '继续准备管理界面';
  badge.textContent = '可恢复';
  badge.dataset.tone = 'pending';
  message.textContent = recoveringReset
    ? '检测到尚未完成的界面重置任务，可继续清除本机选择；三套源码保持不变。'
    : '检测到尚未完成的界面准备任务。当前选择已保留，可继续执行同一任务。';
  details.hidden = false;
  selectedUi.textContent =
    uiLabels[status.selectedUi] || status.selectedUi || '—';
  selectedMode.textContent = recoveringReset ? '重置界面' : '准备界面';
  uiPrepareTitle.textContent = recoveringReset
    ? '继续清除本机选择'
    : '继续准备管理界面';
  uiPrepareHint.textContent = recoveringReset
    ? '清除本机选择（三套源码保持不变）'
    : '按所选工作区闭包安装依赖';
  uiPreparePanel.hidden = false;
  uiPrepareForm.hidden = recoveringReset;
  resumeUIResetButton.hidden = !recoveringReset;
  selectionPanel.hidden = true;
  connectionPanel.hidden = true;
  nextSteps.hidden = true;
  retryButton.hidden = false;
  selectUIChoice(recoveringReset ? '' : status.selectedUi);
  confirmCleanup.checked = !recoveringReset;
  uiSelectionLocked = !recoveringReset && Boolean(status.selectedUi);
  setUIActionPending(false);
  if (Number.isFinite(Number(status.progress))) {
    renderUIActionProgress({
      action: recoveringReset ? 'reset' : 'prepare',
      state: 'running',
      selectedUi: status.selectedUi,
      currentStep: status.currentStep,
      progress: status.progress,
      lastUpdated: status.lastUpdated,
    });
  } else {
    showUIActionMessage(
      recoveringReset
        ? '点击“继续清除本机选择”恢复任务；三套源码保持不变。'
        : '点击“继续准备此界面”恢复任务。',
      'pending',
    );
  }
  announceMissingUITools();
  title.focus();
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
  planMessage.textContent =
    '正在验证目录的读取、写入、创建、重命名与删除能力。';
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
    const permitted =
      permission.canRead && permission.canRename && permission.canDelete;
    ready.textContent =
      permitted || entry.action === 'keep' ? '可用' : '需处理';
    ready.dataset.ready =
      permitted || entry.action === 'keep' ? 'true' : 'false';
    item.append(path, action, ready);
    return item;
  });
  planEntries.replaceChildren(...nodes);
  const ready = plan.canBuild && plan.canWriteEnv && plan.canCleanup;
  planMessage.textContent = ready
    ? '预检通过，可以继续填写服务连接信息。'
    : '部分目录权限需要处理，尚未执行任何文件变更。';
  planMessage.dataset.tone = ready ? 'success' : 'error';
  planPanel.hidden = false;
  connectionPanel.hidden = !ready;
  updateApplyButton();
}

function yesNo(value) {
  return value ? '是' : '否';
}

function actionLabel(action) {
  return (
    { keep: '保留', remove: '待移除', create: '待创建', write: '待写入' }[
      action
    ] || '检查'
  );
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
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(formValues),
    });
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('dependency check failed');
    }
    const result = envelope.data;
    if (endpoint === databaseCheckEndpoint)
      databaseCheckPassed = Boolean(result.ok);
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
    submit.textContent =
      endpoint === databaseCheckEndpoint ? '测试数据库连接' : '测试 Redis 连接';
    updateApplyButton();
  }
}

function updateApplyButton() {
  applyButton.disabled =
    !currentPlan ||
    !currentPlan.canBuild ||
    !currentPlan.canWriteEnv ||
    !currentPlan.canCleanup ||
    !databaseCheckPassed ||
    !redisCheckPassed;
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
  applyResult.textContent = installationCompletedMessage;
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
  applyResult.dataset.tone =
    job.state === 'failed'
      ? 'error'
      : job.state === 'completed'
        ? 'success'
        : 'pending';
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
    const response = await fetch(
      `${progressEndpoint}/${encodeURIComponent(jobId)}`,
      {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      },
    );
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
    tls_mode_mismatch:
      '数据库未启用 TLS，且数据库连接测试与迁移采用了不同模式；请统一 TLS 配置。当前输入已保留。',
    tls_configuration_failed:
      '数据库 TLS 证书、CA 或主机名校验失败，请检查加密连接配置。当前输入已保留。',
    authentication_failed:
      '数据库身份验证失败，请检查账号、密码和访问规则。当前输入已保留。',
    permission_denied:
      '数据库账号缺少建表、索引或约束变更权限。当前输入已保留。',
    database_unavailable:
      '数据库迁移连接不可用，请检查地址、服务状态及 TLS 模式。当前输入已保留。',
    database_busy:
      '数据库当前被锁定或事务发生冲突，请等待占用结束后重试。当前输入已保留。',
    schema_unavailable:
      '目标数据库 schema 不存在或当前账号不可访问。当前输入已保留。',
    schema_conflict:
      '目标数据库已存在冲突的表、字段或约束，请核对现有结构。当前输入已保留。',
    migration_dirty:
      '数据库迁移版本处于 dirty 状态，需要先核对失败版本再继续。当前输入已保留。',
    migration_statement_failed:
      '数据库迁移语句执行失败，请按任务 ID 和数据库代码定位。当前输入已保留。',
    migration_status_failed:
      '数据库迁移状态读取或校验失败，请按任务 ID 定位。当前输入已保留。',
    migration_close_failed:
      '数据库迁移已经执行，但连接收尾失败，请核对迁移状态后重试。当前输入已保留。',
    invalid_configuration:
      '数据库迁移配置不完整，请重新检查连接参数。当前输入已保留。',
    navigation_seed_conflict:
      '数据库中已有同 ID 但定义不同的菜单或权限种子；请根据下方冲突资源定位并核对历史数据。当前输入已保留。',
    unknown:
      '数据库迁移出现未分类故障，请按任务 ID 在服务端日志中定位。当前输入已保留。',
  };
  if (databaseReasons[job?.failureReason])
    return databaseReasons[job.failureReason];
  if (job?.errorKey === 'installation_running') {
    return '检测到另一项初始化或安装任务正在执行。请保留当前输入，并在 /install 点击“重新检查”；任务结束后可直接继续。当前输入已保留。';
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
      database:
        '数据库连接复核未通过，请检查数据库服务和当前配置。当前输入已保留。',
      redis:
        'Redis 连接复核未通过，请检查 Redis 服务和当前配置。当前输入已保留。',
      journal:
        '安装事务状态校验未通过，请保留当前目录并重新检查初始化状态。当前输入已保留。',
      coordination: '安装执行权释放或校验未完成，请稍后重试。当前输入已保留。',
    };
    return (
      reasons[job?.failureStep] ||
      '安装前置校验未通过，请根据失败位置检查配置。当前输入已保留。'
    );
  }
  if (job?.errorKey === 'request_unavailable') {
    return '安装请求或进度查询未完成，请确认服务端仍在运行。当前输入已保留。';
  }
  if (job?.errorKey === 'internal_error') {
    const reasons = {
      schema:
        '数据库结构迁移执行失败，请查看失败任务定位信息。当前输入已保留。',
      identity: '初始管理员创建失败，请查看失败任务定位信息。当前输入已保留。',
      environment: '运行配置写入失败，请检查目录权限。当前输入已保留。',
      journal: '安装事务记录写入失败，请检查安装状态目录。当前输入已保留。',
      marker: '安装标记生成失败，请检查安装配置。当前输入已保留。',
      lock: '安装锁写入失败，请检查安装状态目录。当前输入已保留。',
      recovery: '上次安装事务恢复失败，请查看服务端终端。当前输入已保留。',
    };
    return (
      reasons[job?.failureStep] ||
      '服务端安装执行失败，请查看失败任务定位信息。当前输入已保留。'
    );
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
    resourceKind: String(job?.failureResourceKind || '—'),
    resourceId: String(job?.failureResourceId || '—'),
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
  failureResourceKind.textContent = diagnostics.resourceKind;
  failureResourceId.textContent = diagnostics.resourceId;
  failureJobId.textContent = diagnostics.jobId;
  installFailureDetails.hidden = false;
  installFailureDetails.focus();
}

function clearInstallationFailure() {
  installFailureDetails.hidden = true;
  for (const output of [
    failureReason,
    failureStep,
    failureErrorCode,
    failureErrorKey,
    failureReasonKey,
    failureOperation,
    failureDatabaseCode,
    failureResourceKind,
    failureResourceId,
    failureJobId,
  ]) {
    output.textContent = '—';
  }
}

function announceApplyError(detail, diagnosticsAvailable = false) {
  applyResult.textContent = detail;
  applyResult.dataset.tone = 'error';
  applyResult.setAttribute('role', diagnosticsAvailable ? 'status' : 'alert');
  applyResult.setAttribute(
    'aria-live',
    diagnosticsAvailable ? 'polite' : 'assertive',
  );
  if (diagnosticsAvailable) return;
  applyResult.focus();
}

async function postInstallationRequest(
  targetEndpoint,
  payload,
  fetcher = fetch,
) {
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

async function submitInstallationRequest(
  payload,
  failedJobId,
  fetcher = fetch,
) {
  const targetEndpoint = failedJobId
    ? `${retryEndpoint}/${encodeURIComponent(failedJobId)}`
    : applyEndpoint;
  let outcome = await postInstallationRequest(targetEndpoint, payload, fetcher);
  if (
    failedJobId &&
    outcome.response.status === 404 &&
    outcome.envelope?.code === 30000
  ) {
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

async function reconcileCompletedInstallation(
  statusReader = fetchInstallationStatus,
) {
  const status = await statusReader();
  if (!status?.installed) return false;
  commitCompletedInstallation(status);
  return true;
}

function validAdminPassword(value) {
  return /^(?=.*[A-Za-z])(?=.*[0-9])[A-Za-z0-9]{6,72}$/.test(value);
}

async function requestInstallation(event) {
  event.preventDefault();
  clearInstallationFailure();
  setProgress(0, '准备安装');
  if (!currentPlan || !databaseCheckPassed || !redisCheckPassed) {
    announceApplyError('请先完成目录、数据库和 Redis 检查。');
    return;
  }
  if (!validAdminPassword(adminPassword.value)) {
    announceApplyError(
      '管理员密码需为 6–72 个字符，仅限英文字母和数字，且至少各 1 个。',
    );
    adminPassword.focus();
    return;
  }
  if (adminPassword.value !== adminPasswordConfirm.value) {
    announceApplyError('两次输入的管理员密码不一致。');
    adminPasswordConfirm.focus();
    return;
  }

  applyButton.disabled = true;
  applyButton.textContent = '安装中';
  applyResult.textContent =
    '服务端正在按顺序执行迁移、管理员初始化、配置写入和安装锁定。';
  applyResult.dataset.tone = 'pending';
  applyResult.setAttribute('role', 'status');
  applyResult.setAttribute('aria-live', 'polite');
  let installationCompleted = false;
  try {
    const dependencies = dependencyFormValues();
    const payload = {
      mode: modeChoice.value,
      // The installer only asks for the default locale. The server keeps the
      // backwards-compatible locale mode default (single) when omitted.
      locale: localeChoice.value,
      database: dependencies.database,
      redis: dependencies.redis,
      admin: {
        username: adminUsername.value.trim(),
        password: adminPassword.value,
      },
    };
    const { response, envelope } = await submitInstallationRequest(
      payload,
      retryJobId,
    );
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      if (installationCompletionDetected(envelope)) {
        installationCompleted = await reconcileCompletedInstallation();
        if (installationCompleted) return;
      }
      const failure = {
        id: envelope?.traceId || envelope?.meta?.requestId,
        failureStep: 'request',
        errorCode: Number.isInteger(envelope?.code) ? envelope.code : undefined,
        errorKey:
          envelope?.code === 10007
            ? 'installation_running'
            : envelope?.code === 10006
              ? 'installation_completed'
              : envelope?.code === 10000
                ? 'invalid_request'
                : 'request_unavailable',
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
    const completedStatus = {
      installed: true,
      installerVersion: 'current',
      mode: result.mode,
    };
    completedStatus.selectedUi = result.selectedUi;
    commitCompletedInstallation(completedStatus, result);
    installationCompleted = true;
  } catch {
    announceApplyError(
      '安装请求未完成，请确认服务仍在运行。当前输入已保留；服务恢复后可直接重试，修改连接配置后再重新测试。',
      true,
    );
    renderInstallationFailure({
      failureStep: 'request',
      errorKey: 'request_unavailable',
    });
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
    const response = await fetch(
      `${rollbackEndpoint}/${encodeURIComponent(rollbackJobId)}`,
      {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ confirmRollback: true }),
      },
    );
    const envelope = await response.json();
    if (!response.ok || envelope.code !== 0 || !envelope.data) {
      throw new Error('rollback request failed');
    }
    renderJobProgress(envelope.data);
    clearInstallationFailure();
    applyResult.textContent =
      '本次失败事务已回滚，当前输入仍保留，可以直接重试或修改后再测试。';
    applyResult.dataset.tone = 'success';
    rollbackButton.hidden = true;
    rollbackJobId = null;
    retryJobId = envelope.data.canRetry ? envelope.data.jobId : null;
  } catch {
    applyResult.textContent =
      '回滚未完成，请保留当前安装目录并使用离线恢复流程。';
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
  for (const input of form.querySelectorAll('input[type="password"]'))
    input.value = '';
}

async function loadAll() {
  if (uiActionPending) return;
  await Promise.allSettled([loadStatus(), loadCapabilities()]);
}

retryButton.addEventListener('click', loadAll);
suggestBrowserLocale();
uiPrepareForm.addEventListener('submit', requestUIPreparation);
uiPrepareForm.addEventListener('change', updateUIPrepareButton);
resetUIButton.addEventListener('click', () => requestUIReset(true));
resumeUIResetButton.addEventListener('click', () => requestUIReset(false));
planForm.addEventListener('submit', requestPlan);
databaseForm.addEventListener('submit', (event) =>
  requestDependencyCheck(event, databaseCheckEndpoint, databaseResult),
);
redisForm.addEventListener('submit', (event) =>
  requestDependencyCheck(event, redisCheckEndpoint, redisResult),
);
adminForm.addEventListener('submit', requestInstallation);
rollbackButton.addEventListener('click', requestRollback);
databaseForm.addEventListener('input', () =>
  invalidateDependencyCheck(databaseResult),
);
redisForm.addEventListener('input', () =>
  invalidateDependencyCheck(redisResult),
);
modeChoice.addEventListener('change', invalidatePlanIfModeChanged);
databaseDriver.addEventListener('change', () => {
  databasePort.value = databaseDriver.value === 'postgres' ? '5432' : '3306';
});
loadAll();
