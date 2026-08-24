import { randomUUID } from 'node:crypto';
import { existsSync, lstatSync, readFileSync } from 'node:fs';
import { join, win32 } from 'node:path';

const TRANSACTION_ID_PATTERN = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i;

export function parseWindowsJobGate(contents, expectedToken) {
  if (
    typeof contents !== 'string'
    || contents.length > 256
    || !TRANSACTION_ID_PATTERN.test(expectedToken ?? '')
    || !contents.endsWith('\n')
  ) return null;
  try {
    const gate = JSON.parse(contents);
    if (!gate || typeof gate !== 'object' || Array.isArray(gate)) return null;
    if (Object.keys(gate).sort().join(',') !== 'guardianPid,owner,schema,token') return null;
    if (
      gate.schema !== 1
      || gate.owner !== 'admin-dependency-job'
      || gate.token !== expectedToken
      || !Number.isInteger(gate.guardianPid)
      || gate.guardianPid <= 0
    ) return null;
    return { guardianPid: gate.guardianPid };
  } catch {
    return null;
  }
}

export async function waitForWindowsJobGate(gatePath, expectedToken, options = {}) {
  const exists = options.existsSync ?? existsSync;
  const inspect = options.lstatSync ?? lstatSync;
  const read = options.readFileSync ?? readFileSync;
  const now = options.now ?? Date.now;
  const delay = options.delay ?? ((milliseconds) => new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds)));
  const timeoutMs = options.timeoutMs ?? 10_000;
  const deadline = now() + timeoutMs;
  while (now() <= deadline) {
    if (exists(gatePath)) {
      try {
        const stat = inspect(gatePath);
        if (!stat.isFile() || stat.isSymbolicLink() || stat.size > 256) {
          throw new Error('DEPENDENCY_INSTALL_FAILED');
        }
        const contents = read(gatePath, 'utf8');
        const parsed = parseWindowsJobGate(contents, expectedToken);
        if (parsed) return parsed;
        // A newline is the last published byte. If it is already visible, the
        // payload is complete but invalid rather than a partial write.
        if (contents.endsWith('\n')) throw new Error('DEPENDENCY_INSTALL_FAILED');
      } catch (error) {
        if (error instanceof Error && error.message === 'DEPENDENCY_INSTALL_FAILED') throw error;
        // File sharing and visibility can briefly fail while CreateNew/Write/
        // Flush is in progress. Retry until the same bounded deadline.
      }
    }
    if (now() >= deadline) break;
    await delay(25);
  }
  throw new Error('DEPENDENCY_INSTALL_FAILED');
}

export function buildDependencySupervisorCommand(options) {
  const {
    execPath,
    platform,
    scriptsDirectory,
    stateRoot,
    token = randomUUID(),
  } = options;
  const joinPath = platform === 'win32' ? win32.join : join;
  const supervisor = joinPath(scriptsDirectory, 'dependency-supervisor.mjs');
  if (platform !== 'win32') return { command: execPath, args: [supervisor] };

  const gate = joinPath(stateRoot, `dependency-job-gate-${token}.json`);
  return {
    command: 'powershell.exe',
    args: [
      '-NoLogo',
      '-NoProfile',
      '-NonInteractive',
      '-ExecutionPolicy',
      'Bypass',
      '-File',
      joinPath(scriptsDirectory, 'dependency-supervisor-windows.ps1'),
      execPath,
      supervisor,
      gate,
      token,
    ],
  };
}
