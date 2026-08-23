#!/usr/bin/env node
import { createInterface } from 'node:readline/promises';
import { stdin, stdout } from 'node:process';

import {
  STATES,
  STATE_REASONS,
  UI_PROFILES,
  formatStatus,
  initialize,
  inspectState,
  preflightInitialization,
  recoverSafeLocalState,
  reset,
  rootFromScript,
} from './init-state.mjs';
import { runInstallerRuntime } from './init-runtime.mjs';

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

const root = rootFromScript(import.meta.url);
let port = 8080;

async function serveInstaller(prepared, options) {
  const runtime = await runInstallerRuntime({
    root,
    port: options.port,
    noOpen: options.noOpen,
    onReady(opened) {
      print({ ...prepared, next: opened ? 'INSTALLER_LAUNCHED' : 'OPEN_INSTALLER', port: options.port });
    },
  });
  if (runtime.simulated) return 0;

  const final = inspectState(root);
  if (final.state === STATES.INSTALLED) {
    print({ ...final, next: 'RESTART_SERVER_THEN_RUN_DEV_OR_BUILD', port: options.port });
    return 0;
  }
  if (runtime.interrupted) {
    print({ ...final, next: 'RESUME_INSTALLER', error: 'INTERRUPTED', port: options.port });
    return 130;
  }
  throw new Error(runtime.exitCode === 0 ? 'INSTALLER_STOPPED' : 'INSTALLER_RUNTIME_FAILED');
}

async function main() {
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
    if (!options.reset && current.state === STATES.INCONSISTENT) {
      const recovery = await recoverSafeLocalState(root);
      if (recovery.recovered) {
        printRecovery(recovery);
        current = inspectState(root);
      }
    }
    if (options.reset) {
      if (current.state === STATES.INSTALLED) throw new Error('RESET_UNAVAILABLE_INSTALLED');
      await confirm('Confirm reset: restore staged templates and remove the UI profile.', options.confirmReset);
      const result = await reset(root);
      print({ ...result, next: 'RESET_COMPLETE', port });
      return 0;
    }
    if (current.state === STATES.INSTALLED) {
      print({ ...current, next: 'ALREADY_INSTALLED', port });
      return 0;
    }
    if (current.state === STATES.UI_PREPARED) {
      return serveInstaller(current, options);
    }
    if (current.state !== STATES.PRISTINE) {
      const error = current.state === STATES.INCONSISTENT ? 'STATE_INCONSISTENT' : 'INITIALIZATION_IN_PROGRESS';
      if (current.state === STATES.INCONSISTENT) printStateGuidance(current);
      print({ ...current, next: 'RECOVER_INITIALIZATION', error, port });
      return 3;
    }

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
    return serveInstaller(result, options);
  } catch (error) {
    const current = inspectState(root);
    const code = error instanceof Error ? error.message : 'INIT_FAILED';
    if (code === 'PREFLIGHT_FAILED') stdout.write('INIT_PREFLIGHT=failed\n');
    const argumentError = ['UI_REQUIRED', 'UI_INVALID', 'ARGUMENT_INVALID', 'PORT_INVALID', 'CLEANUP_CONFIRMATION_REQUIRED', 'RESET_CONFIRMATION_REQUIRED'].includes(code);
    const stateError = code.startsWith('RESET_') || code === 'TEMPLATE_LAYOUT_INVALID' || code === 'PREFLIGHT_FAILED';
    print({ ...current, next: argumentError ? 'PROVIDE_INPUT' : 'RECOVER_INITIALIZATION', error: code, port });
    if (argumentError) stdout.write(`${usage()}\n`);
    return argumentError ? 2 : stateError ? 3 : 1;
  }
}

process.exitCode = await main();
