const endpoint = '/api/system/install/v1/status';
const capabilitiesEndpoint = '/api/system/install/v1/capabilities';
const planEndpoint = '/api/system/install/v1/plan';

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

const uiLabels = { antd: 'Ant Design Vue', ele: 'Element Plus', naive: 'Naive UI' };
const modeLabels = {
  embedded: '嵌入式单包',
  standalone: '静态资源独立部署',
  api_only: '仅 API',
  dev: '开发调试',
};

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
    return;
  }

  title.textContent = '安装服务已就绪';
  badge.textContent = '待安装';
  badge.dataset.tone = 'ready';
  message.textContent = '本机安装状态可用，接下来将检查运行环境和目录权限。';
  selectedUi.textContent = '尚未选择';
  selectedMode.textContent = '尚未选择';
  selectionPanel.hidden = false;
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
}

function yesNo(value) {
  return value ? '是' : '否';
}

function actionLabel(action) {
  return { keep: '保留', remove: '待移除', create: '待创建', write: '待写入' }[action] || '检查';
}

async function loadAll() {
  await Promise.allSettled([loadStatus(), loadCapabilities()]);
}

retryButton.addEventListener('click', loadAll);
planForm.addEventListener('submit', requestPlan);
loadAll();
