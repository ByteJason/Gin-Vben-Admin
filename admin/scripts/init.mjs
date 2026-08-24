#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import {
  closeSync,
  mkdirSync,
} from 'node:fs';
import { createInterface } from 'node:readline/promises';
import { stdin, stdout } from 'node:process';
import { fileURLToPath } from 'node:url';

import {
  STATES,
  STATE_REASONS,
  UI_PROFILES,
  acquireAdminInitLease,
  completeDependencyPreparation,
  dependenciesPrepared,
  formatStatus,
  ensureInstallerApplyIdle,
  initialize,
  inspectState,
  installURL,
  migrateLegacyPreparedState,
  preflightInitialization,
  recoverSafeLocalState,
  reset,
  rootFromScript,
  statePaths,
} from './init-state.mjs';
import { buildDependencySupervisorCommand } from './dependency-launch.mjs';
import { openDependencyLog } from './dependency-log.mjs';
import { buildPnpmCommand } from './pnpm-command.mjs';

function usage() {
  return 'Usage: pnpm run init -- [--check|--reset] [--ui antd|ele|naive] [--confirm-cleanup] [--confirm-reset] [--no-open] [--port 1..65535]';
}

function parseArgs(argv) {
  const options = { selectedUi: '', check: false, reset: false, confirmCleanup: false, confirmReset: false, noOpen: false, port: 8080 };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--' && index === 0) continue;
    if (argument === '--ui') options.selectedUi = argv[++index] ?? '';
    else if (argument === '--port') options.port = Number(argv[++index]);
    else if (argument === '--check') options.check = true;
    else if (argument === '--reset') options.reset = true;
    else if (argument === '--confirm-cleanup') options.confirmCleanup = true;
    else if (argument === '--confirm-reset') options.confirmReset = true;
    else if (argument === '--no-open') options.noOpen = true;
    else if (argument === '--help' || argument === '-h') return { help: true };
    else throw new Error('ARGUMENT_INVALID');
  }
  if (options.check && options.reset) throw new Error('ARGUMENT_INVALID');
  if (options.selectedUi && !UI_PROFILES[options.selectedUi]) throw new Error('UI_INVALID');
  if (!Number.isInteger(options.port) || options.port < 1 || options.port > 65535) throw new Error('PORT_INVALID');
  return options;
}

async function confirm(message, explicit) {
  if (explicit) return;
  if (!stdin.isTTY) throw new Error(message.includes('reset') ? 'RESET_CONFIRMATION_REQUIRED' : 'CLEANUP_CONFIRMATION_REQUIRED');
  const reader = createInterface({ input: stdin, output: stdout });
  try {
    const answer = (await reader.question(`${message} Type yes to continue: `)).trim().toLowerCase();
    if (answer !== 'yes') throw new Error(message.includes('reset') ? 'RESET_CONFIRMATION_REQUIRED' : 'CLEANUP_CONFIRMATION_REQUIRED');
  } finally {
    reader.close();
  }
}

async function chooseUI() {
  if (!stdin.isTTY) throw new Error('UI_REQUIRED');
  const reader = createInterface({ input: stdin, output: stdout });
  try {
    const selectedUi = (await reader.question('Select UI (antd/ele/naive): ')).trim();
    if (!UI_PROFILES[selectedUi]) throw new Error('UI_INVALID');
    return selectedUi;
  } finally {
    reader.close();
  }
}

function print(status) {
  stdout.write(`${formatStatus(status)}\n`);
}

function printRecovery(recovery) {
  stdout.write('检测到上次初始化留下的本地状态，已安全备份并自动恢复。\n');
  stdout.write(`INIT_RECOVERY=completed\nINIT_RECOVERY_REASON=${recovery.recoveryReason}\nINIT_RECOVERY_BACKUP=${recovery.recoveryBackup}\n`);
}

function printStateGuidance(snapshot) {
  if ([STATE_REASONS.RECEIPT_WITHOUT_PROFILE, STATE_REASONS.RUNTIME_WITHOUT_PROFILE].includes(snapshot.reason)) {
    stdout.write('检测到可自动恢复的首次初始化状态。\n');
    stdout.write('直接重新运行 pnpm run init，程序会先备份现场再继续。\n');
    return;
  }
  stdout.write('初始化状态需要进一步确认；程序已保护现场，没有覆盖项目文件。\n');
  stdout.write(`请向维护者提供状态代码 ${snapshot.reason}，无需检查隐藏文件。\n`);
}

async function verifyOrdinaryAPI(port) {
  if (process.env.INIT_API_TEST_MODE === 'ready') return;
  const baseURL = (process.env.INIT_API_BASE_URL || `http://127.0.0.1:${port}`).replace(/\/$/, '');
  const paths = ['/health/live', '/api/system/install/v1/status', '/install'];
  if (process.env.INIT_API_PROBE_COMMAND) {
    let prefixArgs;
    try {
      prefixArgs = JSON.parse(process.env.INIT_API_PROBE_PREFIX_ARGS || '[]');
    } catch {
      throw new Error('API_UNAVAILABLE');
    }
    if (!Array.isArray(prefixArgs) || prefixArgs.some((value) => typeof value !== 'string')) throw new Error('API_UNAVAILABLE');
    for (const path of paths) {
      const result = spawnSync(process.env.INIT_API_PROBE_COMMAND, [...prefixArgs, 'GET', `${baseURL}${path}`], {
        env: process.env,
        shell: false,
        stdio: 'inherit',
      });
      if (result.error || result.status !== 0) throw new Error('API_UNAVAILABLE');
    }
    return;
  }
  try {
    for (const path of paths) {
      const response = await fetch(`${baseURL}${path}`, {
        method: 'GET',
        signal: AbortSignal.timeout(2_000),
      });
      if (!response.ok) throw new Error('API_UNAVAILABLE');
    }
  } catch {
    throw new Error('API_UNAVAILABLE');
  }
}

async function installDependencies() {
  if (process.env.INIT_DEPENDENCY_INSTALL_TEST_MODE === 'success') return;
  // Validate the same command construction before launching the detached
  // supervisor, which imports the JavaScript pnpm CLI in its own Worker.
  try {
    buildPnpmCommand(['install', '--frozen-lockfile']);
  } catch {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  stdout.write('正在安装所选 UI 的依赖；中断前台 init 后，后台监督进程仍会安全完成本次安装。\n');
  stdout.write('INIT_DEPENDENCY_LOG=.runtime/install/dependency-install.log\n');
  const location = statePaths(root);
  mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
  let logDescriptor;
  try {
    logDescriptor = openDependencyLog(location.dependencyLog);
  } catch {
    if (logDescriptor !== undefined) closeSync(logDescriptor);
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  const invocation = buildDependencySupervisorCommand({
    execPath: process.execPath,
    platform: process.platform,
    scriptsDirectory: fileURLToPath(new URL('.', import.meta.url)),
    stateRoot: statePaths(root).stateRoot,
  });
  let supervisor;
  try {
    supervisor = spawn(invocation.command, invocation.args, {
      cwd: root,
      detached: true,
      env: process.env,
      shell: false,
      stdio: ['ignore', logDescriptor, logDescriptor],
      windowsHide: true,
    });
  } finally {
    closeSync(logDescriptor);
  }
  const status = await new Promise((resolveExit, rejectExit) => {
    supervisor.once('error', rejectExit);
    supervisor.once('exit', (code) => resolveExit(code));
  }).catch(() => null);
  if (status === 3) throw new Error('INIT_BUSY');
  if (status !== 0) throw new Error('DEPENDENCY_INSTALL_FAILED');
}

const root = rootFromScript(import.meta.url);
let port = 8080;

function launchBrowser(url, noOpen) {
  if (noOpen) return false;
  if (process.env.INIT_LAUNCHER) {
    const result = spawnSync(process.execPath, [process.env.INIT_LAUNCHER, url], {
      env: process.env,
      shell: false,
      stdio: 'inherit',
    });
    if (result.error || result.status !== 0) throw new Error('LAUNCHER_FAILED');
    return true;
  }
  if (!stdout.isTTY) return false;
  const [command, args] = process.platform === 'darwin'
    ? ['open', [url]]
    : process.platform === 'win32'
      ? ['cmd.exe', ['/d', '/s', '/c', 'start', '', url]]
      : ['xdg-open', [url]];
  const result = spawnSync(command, args, { shell: false, stdio: 'ignore' });
  return !result.error && result.status === 0;
}

function openInstaller(prepared, options) {
  const opened = launchBrowser(installURL(options.port), options.noOpen);
  print({ ...prepared, next: opened ? 'INSTALLER_LAUNCHED' : 'OPEN_INSTALLER', port: options.port });
  return 0;
}

async function ensureDependenciesPrepared(profile) {
  if (!dependenciesPrepared(root, profile)) await installDependencies();
  const current = inspectState(root);
  if (current.state === STATES.INSTALLING && current.reason === STATE_REASONS.DEPENDENCIES_PENDING) {
    return completeDependencyPreparation(root);
  }
  if (dependenciesPrepared(root, profile)) return { state: STATES.UI_PREPARED, profile };
  if (current.state === STATES.UI_PREPARED) return current;
  throw new Error('DEPENDENCY_TRANSACTION_INVALID');
}

async function main() {
  let releaseAdminLease;
  try {
    const options = parseArgs(process.argv.slice(2));
    if (options.help) {
      stdout.write(`${usage()}\n`);
      return 0;
    }
    port = options.port;
    let current = inspectState(root);
    if (options.check) {
      if (current.state === STATES.INCONSISTENT) printStateGuidance(current);
      print({ ...current, next: 'CHECK_COMPLETE', error: current.state === STATES.INCONSISTENT ? 'STATE_INCONSISTENT' : 'NONE', port });
      return current.state === STATES.INCONSISTENT ? 3 : 0;
    }
    if (current.state === STATES.INSTALLED) {
      if (options.reset) throw new Error('RESET_UNAVAILABLE_INSTALLED');
      print({ ...current, next: 'ALREADY_INSTALLED', port });
      return 0;
    }
    releaseAdminLease = await acquireAdminInitLease(root);
    ensureInstallerApplyIdle(root);
    if (options.reset) {
      await confirm('Confirm reset: restore staged templates and remove the UI profile.', options.confirmReset);
    }
    const legacyMigration = await migrateLegacyPreparedState(root);
    if (legacyMigration.migrated) {
      stdout.write(`INIT_LEGACY_MIGRATION=${legacyMigration.resumed ? 'resumed' : 'completed'}\n`);
    }
    current = inspectState(root);
    if (!options.reset && current.state === STATES.INCONSISTENT) {
      const recovery = await recoverSafeLocalState(root);
      if (recovery.recovered) {
        printRecovery(recovery);
        current = inspectState(root);
      }
    }
    if (options.reset) {
      if (current.state === STATES.INSTALLED) throw new Error('RESET_UNAVAILABLE_INSTALLED');
      const result = await reset(root);
      print({ ...result, next: 'RESET_COMPLETE', port });
      return 0;
    }
    if (current.state === STATES.INSTALLED) {
      print({ ...current, next: 'ALREADY_INSTALLED', port });
      return 0;
    }
    if (current.state === STATES.INSTALLING && current.reason === STATE_REASONS.RESET_TRANSACTION_PRESENT) {
      stdout.write('检测到未完成的 UI 重置，请运行 pnpm run init -- --reset --confirm-reset 继续。\n');
      print({ ...current, next: 'RUN_RESET', error: 'RESET_IN_PROGRESS', port });
      return 3;
    }
    if (current.state === STATES.INSTALLING && [
      STATE_REASONS.SOURCE_MOVE_TRANSACTION_PRESENT,
      STATE_REASONS.DEPENDENCIES_PENDING,
    ].includes(current.reason)) {
      await verifyOrdinaryAPI(port);
      const resumed = await initialize(root, current.selectedUi ?? current.profile?.selectedUi);
      const prepared = await ensureDependenciesPrepared(resumed.profile);
      return openInstaller({ ...prepared, profile: resumed.profile }, options);
    }
    if (current.state === STATES.UI_PREPARED) {
      await verifyOrdinaryAPI(port);
      const prepared = await ensureDependenciesPrepared(current.profile);
      return openInstaller(prepared, options);
    }
    if (current.state !== STATES.PRISTINE) {
      const error = current.state === STATES.INCONSISTENT ? 'STATE_INCONSISTENT' : 'INITIALIZATION_IN_PROGRESS';
      if (current.state === STATES.INCONSISTENT) printStateGuidance(current);
      print({ ...current, next: 'RECOVER_INITIALIZATION', error, port });
      return 3;
    }

    await verifyOrdinaryAPI(port);
    const selectedUi = options.selectedUi || await chooseUI();
    const plan = preflightInitialization(root, selectedUi);
    stdout.write(`Selected: ${selectedUi}; retain: ${plan.retain}; stage: ${plan.stage.join(', ')}\n`);
    stdout.write(`INIT_PREFLIGHT=ok\nINIT_PLAN_RETAIN=${plan.retain}\nINIT_PLAN_STAGE=${plan.stage.join(',')}\nINIT_PLAN_BACKUP=${plan.backup}\n`);
    await confirm('Confirm cleanup:', options.confirmCleanup);
    const result = await initialize(root, selectedUi);
    if (result.state !== STATES.UI_PREPARED) {
      print({ ...result, next: 'RECOVER_INITIALIZATION', error: 'INITIALIZATION_IN_PROGRESS', port });
      return 3;
    }
    const prepared = await ensureDependenciesPrepared(result.profile);
    return openInstaller(prepared, options);
  } catch (error) {
    const current = inspectState(root);
    const code = error instanceof Error ? error.message : 'INIT_FAILED';
    if (code === 'PREFLIGHT_FAILED') stdout.write('INIT_PREFLIGHT=failed\n');
    const argumentError = ['UI_REQUIRED', 'UI_INVALID', 'ARGUMENT_INVALID', 'PORT_INVALID', 'CLEANUP_CONFIRMATION_REQUIRED', 'RESET_CONFIRMATION_REQUIRED'].includes(code);
    const stateError = code.startsWith('RESET_')
      || code === 'TEMPLATE_LAYOUT_INVALID'
      || code === 'PREFLIGHT_FAILED'
      || code === 'INITIALIZATION_RESUME_INVALID'
      || code === 'LEGACY_MIGRATION_INVALID'
      || code === 'INIT_BUSY';
    const next = argumentError ? 'PROVIDE_INPUT' : code === 'INIT_BUSY' ? 'WAIT_FOR_INITIALIZATION' : 'RECOVER_INITIALIZATION';
    print({ ...current, next, error: code, port });
    if (argumentError) stdout.write(`${usage()}\n`);
    return argumentError ? 2 : stateError ? 3 : 1;
  } finally {
    await releaseAdminLease?.();
  }
}

process.exitCode = await main();
