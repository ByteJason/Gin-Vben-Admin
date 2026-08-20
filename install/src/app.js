const endpoint = '/api/system/install/v1/status';

const title = document.querySelector('#status-title');
const badge = document.querySelector('#status-badge');
const message = document.querySelector('#status-message');
const details = document.querySelector('#status-details');
const installerVersion = document.querySelector('#installer-version');
const selectedUi = document.querySelector('#selected-ui');
const selectedMode = document.querySelector('#selected-mode');
const retryButton = document.querySelector('#retry-button');

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
    return;
  }

  title.textContent = '安装服务已就绪';
  badge.textContent = '待安装';
  badge.dataset.tone = 'ready';
  message.textContent = '本机安装状态可用，接下来将检查运行环境和目录权限。';
  selectedUi.textContent = '尚未选择';
  selectedMode.textContent = '尚未选择';
}

function renderError() {
  title.textContent = '安装服务暂不可用';
  badge.textContent = '检查失败';
  badge.dataset.tone = 'error';
  message.textContent = '请确认服务已启动，然后重新检查。';
  message.setAttribute('aria-live', 'assertive');
  details.hidden = true;
  retryButton.hidden = false;
}

retryButton.addEventListener('click', loadStatus);
loadStatus();
