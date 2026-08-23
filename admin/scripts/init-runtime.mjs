import { spawn, spawnSync } from 'node:child_process';
import { resolve } from 'node:path';

import { clearRuntime, installURL, publishRuntime } from './init-state.mjs';

const LOOPBACK_HOST = '127.0.0.1';
const READY_TIMEOUT_MS = 30_000;

function commandName(name) {
  return process.platform === 'win32' ? `${name}.cmd` : name;
}

function buildInstaller(root) {
  const result = spawnSync(commandName('pnpm'), ['-F', '@gin-vben-admin/install', 'run', 'build:installer'], {
    cwd: root,
    env: process.env,
    shell: false,
    stdio: 'inherit',
  });
  if (result.error || result.status !== 0) throw new Error('INSTALLER_BUILD_FAILED');
}

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
  if (!process.stdout.isTTY) return false;
  const [command, args] = process.platform === 'darwin'
    ? ['open', [url]]
    : process.platform === 'win32'
      ? ['cmd.exe', ['/d', '/s', '/c', 'start', '', url]]
      : ['xdg-open', [url]];
  const result = spawnSync(command, args, { shell: false, stdio: 'ignore' });
  return !result.error && result.status === 0;
}

async function waitUntilReady(url, closed) {
  const deadline = Date.now() + READY_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (closed()) throw new Error('INSTALLER_RUNTIME_FAILED');
    try {
      const response = await fetch(url, { method: 'HEAD', signal: AbortSignal.timeout(1_000) });
      if (response.ok) return;
    } catch {
      // The Go command may still be compiling; retry until the bounded deadline.
    }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 100));
  }
  throw new Error('INSTALLER_START_TIMEOUT');
}

export async function runInstallerRuntime({ root, port, noOpen, onReady }) {
  const url = installURL(port);
  if (process.env.INIT_RUNTIME_TEST_MODE === 'simulate') {
    const opened = launchBrowser(url, noOpen);
    onReady?.(opened);
    return { exitCode: 0, interrupted: false, simulated: true };
  }

  buildInstaller(root);
  const serverRoot = resolve(root, '..', 'server');
  const assets = resolve(root, 'apps', 'install', 'dist');
  const workspaceRoot = resolve(root, '..');
  const stateDir = resolve(root, 'apps', 'install');
  const child = spawn('go', [
    '-C', serverRoot,
    'run', './cmd/init',
    '--assets', assets,
    '--port', String(port),
  ], {
    cwd: root,
    env: {
      ...process.env,
      INSTALL_STATE_DIR: stateDir,
      INSTALL_WORKSPACE_ROOT: workspaceRoot,
    },
    shell: false,
    stdio: 'inherit',
  });

  let closed = false;
  let interrupted = false;
  const completion = new Promise((resolveCompletion) => {
    child.once('error', () => {
      closed = true;
      resolveCompletion({ exitCode: 1, signal: null });
    });
    child.once('close', (exitCode, signal) => {
      closed = true;
      resolveCompletion({ exitCode: exitCode ?? 1, signal });
    });
  });
  const stopChild = () => {
    interrupted = true;
    if (!closed) child.kill('SIGINT');
  };
  process.once('SIGINT', stopChild);
  process.once('SIGTERM', stopChild);

  try {
    if (!Number.isInteger(child.pid) || child.pid <= 0) throw new Error('INSTALLER_RUNTIME_FAILED');
    await publishRuntime(root, { schema: 1, port, pid: child.pid });
    try {
      await waitUntilReady(url, () => closed);
    } catch (error) {
      if (!closed) child.kill('SIGTERM');
      await completion;
      throw error;
    }
    const opened = launchBrowser(url, noOpen);
    onReady?.(opened);
    const result = await completion;
    return { ...result, interrupted, simulated: false };
  } finally {
    process.removeListener('SIGINT', stopChild);
    process.removeListener('SIGTERM', stopChild);
    await clearRuntime(root);
  }
}

export const runtimeHost = LOOPBACK_HOST;
