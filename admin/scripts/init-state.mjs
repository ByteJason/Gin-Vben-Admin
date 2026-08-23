import { randomUUID } from 'node:crypto';
import { accessSync, constants as fsConstants, existsSync, lstatSync, mkdirSync, readFileSync } from 'node:fs';
import { open, rename, rm, writeFile } from 'node:fs/promises';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export const UI_PROFILES = Object.freeze({
  antd: { packageName: '@vben/web-antd', appDirectory: 'apps/web-antd' },
  ele: { packageName: '@vben/web-ele', appDirectory: 'apps/web-ele' },
  naive: { packageName: '@vben/web-naive', appDirectory: 'apps/web-naive' },
});

export const STATES = Object.freeze({
  PRISTINE: 'pristine',
  UI_PREPARED: 'ui_prepared',
  INSTALLING: 'installing',
  INSTALLED: 'installed',
  INCONSISTENT: 'inconsistent',
});

export function installURL(port) {
  return `http://127.0.0.1:${port}/install`;
}

export function statePaths(root) {
  return {
    profile: join(root, '.ui-profile.json'),
    receipt: join(root, '.ui-init-receipt.json'),
    transaction: join(root, '.ui-init-transaction.json'),
    runtime: join(root, '.ui-init-runtime.json'),
    marker: join(root, 'apps', 'install', '.installed'),
    backupRoot: resolve(root, '..', '.runtime', 'init-backup'),
  };
}

function profileFor(selectedUi) {
  const entry = UI_PROFILES[selectedUi];
  return entry ? { schema: 1, selectedUi, ...entry } : null;
}

function parseJSON(file) {
  try {
    const stat = lstatSync(file);
    if (!stat.isFile() || stat.isSymbolicLink()) return null;
    return JSON.parse(readFileSync(file, 'utf8'));
  } catch {
    return null;
  }
}

function parseProfile(file) {
  const parsed = parseJSON(file);
  const expected = profileFor(parsed?.selectedUi);
  if (!expected || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
  const keys = Object.keys(parsed).sort();
  if (keys.join(',') !== 'appDirectory,packageName,schema,selectedUi') return null;
  return Object.entries(expected).every(([key, value]) => parsed[key] === value) ? expected : null;
}

function plainDirectory(target) {
  try {
    const stat = lstatSync(target);
    return stat.isDirectory() && !stat.isSymbolicLink();
  } catch {
    return false;
  }
}

function validReceipt(file, profile) {
  const receipt = parseJSON(file);
  if (!receipt || receipt.schema !== 1 || receipt.selectedUi !== profile.selectedUi || typeof receipt.transactionId !== 'string') return null;
  if (!/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(receipt.transactionId)) return null;
  const expectedMoves = Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== profile.selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
  if (!Array.isArray(receipt.moves) || JSON.stringify(receipt.moves) !== JSON.stringify(expectedMoves)) return null;
  return receipt;
}

function validTransaction(file) {
  const transaction = parseJSON(file);
  const profile = profileFor(transaction?.selectedUi);
  if (!transaction || transaction.schema !== 1 || !profile || typeof transaction.id !== 'string') return false;
  if (!/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(transaction.id)) return false;
  const expectedMoves = Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== transaction.selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
  return Array.isArray(transaction.moves) && JSON.stringify(transaction.moves) === JSON.stringify(expectedMoves);
}

function validMarker(file, profile) {
  const marker = parseJSON(file);
  if (!marker || marker.schema_version !== 1 || marker.selected_ui !== profile.selectedUi) return false;
  if (typeof marker.installer_version !== 'string' || typeof marker.installed_at !== 'string') return false;
  if (!['embedded', 'standalone', 'api_only', 'dev'].includes(marker.mode)) return false;
  return /^[a-f0-9]{64}$/i.test(marker.artifact_hash ?? '') && /^[a-f0-9]{64}$/i.test(marker.manifest_hash ?? '');
}

function validRuntime(file) {
  const runtime = parseJSON(file);
  if (!runtime || runtime.schema !== 1 || typeof runtime !== 'object' || Array.isArray(runtime)) return false;
  const keys = Object.keys(runtime);
  if (keys.some((key) => !['schema', 'port', 'pid'].includes(key))) return false;
  if ('port' in runtime && (!Number.isInteger(runtime.port) || runtime.port < 1 || runtime.port > 65535)) return false;
  return !('pid' in runtime) || (Number.isInteger(runtime.pid) && runtime.pid > 0);
}

export function inspectState(root) {
  const location = statePaths(root);
  const hasTransaction = existsSync(location.transaction);
  const hasRuntime = existsSync(location.runtime);
  const hasReceipt = existsSync(location.receipt);
  const hasMarker = existsSync(location.marker);
  const hasProfile = existsSync(location.profile);
  const profile = hasProfile ? parseProfile(location.profile) : null;

  if (hasTransaction) {
    return { state: validTransaction(location.transaction) && !hasMarker ? STATES.INSTALLING : STATES.INCONSISTENT, profile: null };
  }
  if (hasRuntime && !profile) return { state: STATES.INCONSISTENT, profile: null };
  if (hasMarker && !profile) return { state: STATES.INCONSISTENT, profile: null };
  if (hasProfile && !profile) return { state: STATES.INCONSISTENT, profile: null };
  if (hasReceipt && !profile) return { state: STATES.INCONSISTENT, profile: null };
  if (!profile) return { state: STATES.PRISTINE, profile: null };
  if (!plainDirectory(join(root, profile.appDirectory))) {
    return { state: STATES.INCONSISTENT, profile: null };
  }
  if (Object.entries(UI_PROFILES).some(([ui, entry]) => ui !== profile.selectedUi && existsSync(join(root, entry.appDirectory)))) {
    return { state: STATES.INCONSISTENT, profile: null };
  }
  if (hasReceipt && !validReceipt(location.receipt, profile)) {
    return { state: STATES.INCONSISTENT, profile: null };
  }
  if (hasRuntime) {
    if (!validRuntime(location.runtime) || hasMarker) return { state: STATES.INCONSISTENT, profile: null };
    return { state: STATES.INSTALLING, profile };
  }
  if (hasMarker && !validMarker(location.marker, profile)) return { state: STATES.INCONSISTENT, profile: null };
  return { state: hasMarker ? STATES.INSTALLED : STATES.UI_PREPARED, profile };
}

async function atomicWrite(file, contents) {
  const temporary = `${file}.tmp-${process.pid}-${randomUUID()}`;
  await writeFile(temporary, contents, { mode: 0o600 });
  await rename(temporary, file);
}

async function acquireTransaction(file, contents) {
  const handle = await open(file, 'wx', 0o600);
  try {
    await handle.writeFile(contents);
  } finally {
    await handle.close();
  }
}

function assertTemplateLayout(root) {
  for (const entry of Object.values(UI_PROFILES)) {
    if (!plainDirectory(join(root, entry.appDirectory))) throw new Error('TEMPLATE_LAYOUT_INVALID');
  }
  if (existsSync(join(root, 'apps', 'web'))) throw new Error('TEMPLATE_LAYOUT_INVALID');
}

function assertWritableDirectory(target) {
  if (!plainDirectory(target)) throw new Error('PREFLIGHT_FAILED');
  accessSync(target, fsConstants.W_OK | fsConstants.X_OK);
}

function assertBackupRootAvailable(root, backupRoot) {
  const repositoryRoot = resolve(root, '..');
  const fromRepository = relative(repositoryRoot, backupRoot);
  if (isAbsolute(fromRepository) || fromRepository === '..' || fromRepository.startsWith(`..${process.platform === 'win32' ? '\\' : '/'}`)) {
    throw new Error('PREFLIGHT_FAILED');
  }

  let existing = backupRoot;
  while (!existsSync(existing)) {
    const parent = dirname(existing);
    if (existing === repositoryRoot || parent === existing) throw new Error('PREFLIGHT_FAILED');
    existing = parent;
  }
  assertWritableDirectory(existing);
}

// preflightInitialization is intentionally read-only. It validates all fixed
// source paths and the nearest existing backup parent before the user confirms
// template movement, so a broken .runtime path cannot leave a transaction.
export function preflightInitialization(root, selectedUi) {
  const profile = profileFor(selectedUi);
  if (!profile) throw new Error('UI_INVALID');
  assertTemplateLayout(root);
  const location = statePaths(root);
  try {
    assertWritableDirectory(root);
    assertWritableDirectory(join(root, 'apps'));
    for (const entry of Object.values(UI_PROFILES)) {
      accessSync(join(root, entry.appDirectory), fsConstants.R_OK | fsConstants.X_OK);
    }
    assertBackupRootAvailable(root, location.backupRoot);
  } catch (error) {
    if (error instanceof Error && error.message === 'TEMPLATE_LAYOUT_INVALID') throw error;
    throw new Error('PREFLIGHT_FAILED');
  }
  return {
    profile,
    retain: profile.appDirectory,
    stage: Object.entries(UI_PROFILES).filter(([ui]) => ui !== selectedUi).map(([, entry]) => entry.appDirectory),
    backup: '.runtime/init-backup/<transaction>',
  };
}

async function rollbackMoves(root, transaction) {
  const location = statePaths(root);
  for (const move of [...transaction.moves].reverse()) {
    const from = join(location.backupRoot, transaction.id, move.backup);
    const to = join(root, move.source);
    if (existsSync(from) && !existsSync(to)) await rename(from, to);
  }
  await rm(join(location.backupRoot, transaction.id), { force: true, recursive: true });
  await rm(location.transaction, { force: true });
}

export async function initialize(root, selectedUi) {
  const profile = profileFor(selectedUi);
  if (!profile) throw new Error('UI_INVALID');
  const current = inspectState(root);
  if (current.state !== STATES.PRISTINE) return { ...current, repeated: true };
  preflightInitialization(root, selectedUi);

  const location = statePaths(root);
  const id = randomUUID();
  const moves = Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
  const transaction = { schema: 1, id, selectedUi, moves };
  try {
    await acquireTransaction(location.transaction, `${JSON.stringify(transaction)}\n`);
  } catch (error) {
    if (error && typeof error === 'object' && error.code === 'EEXIST') return { ...inspectState(root), repeated: true };
    throw error;
  }
  try {
    for (const move of moves) {
      const target = join(location.backupRoot, id, move.backup);
      mkdirSync(dirname(target), { recursive: true, mode: 0o700 });
      await rename(join(root, move.source), target);
    }
    await atomicWrite(location.profile, `${JSON.stringify(profile, null, 2)}\n`);
    await atomicWrite(location.receipt, `${JSON.stringify({ schema: 1, transactionId: id, selectedUi, moves }, null, 2)}\n`);
    await rm(location.transaction, { force: true });
  } catch (error) {
    try {
      await rollbackMoves(root, transaction);
    } catch {
      // The persistent transaction is the recovery authority after interruption.
    }
    throw error;
  }
  return { state: STATES.UI_PREPARED, profile, repeated: false };
}

export async function reset(root) {
  const current = inspectState(root);
  if (current.state === STATES.INSTALLED) throw new Error('RESET_UNAVAILABLE_INSTALLED');
  if (current.state !== STATES.UI_PREPARED || !current.profile) throw new Error('RESET_UNAVAILABLE');
  const location = statePaths(root);
  if (!existsSync(location.receipt)) throw new Error('RESET_RECEIPT_UNAVAILABLE');
  const receipt = validReceipt(location.receipt, current.profile);
  if (!receipt) throw new Error('RESET_RECEIPT_INVALID');
  for (const move of receipt.moves) {
    const from = join(location.backupRoot, receipt.transactionId, move.backup);
    const to = join(root, move.source);
    if (!plainDirectory(from) || existsSync(to)) throw new Error('RESET_LAYOUT_INVALID');
  }
  for (const move of receipt.moves) {
    await rename(join(location.backupRoot, receipt.transactionId, move.backup), join(root, move.source));
  }
  await rm(join(location.backupRoot, receipt.transactionId), { force: true, recursive: true });
  await rm(location.profile, { force: true });
  await rm(location.receipt, { force: true });
  return { state: STATES.PRISTINE, profile: null };
}

// The runtime receipt belongs to the process launcher, not to the committed UI
// profile. A launcher publishes it before spawning the installer and removes it
// after the child exits, so source move transactions stay short-lived.
export async function publishRuntime(root, runtime) {
  if (!runtime || runtime.schema !== 1 || !Number.isInteger(runtime.port) || runtime.port < 1 || runtime.port > 65535) {
    throw new Error('RUNTIME_INVALID');
  }
  if ('pid' in runtime && (!Number.isInteger(runtime.pid) || runtime.pid <= 0)) throw new Error('RUNTIME_INVALID');
  await atomicWrite(statePaths(root).runtime, `${JSON.stringify(runtime)}\n`);
}

export async function clearRuntime(root) {
  await rm(statePaths(root).runtime, { force: true });
}

export function formatStatus({ state, profile, next, error = 'NONE', port = 8080 }) {
  return [
    `INIT_STATE=${state}`,
    `INIT_SELECTED_UI=${profile?.selectedUi ?? 'none'}`,
    `INIT_URL=${installURL(port)}`,
    `INIT_NEXT=${next}`,
    `INIT_ERROR=${error}`,
  ].join('\n');
}

export function rootFromScript(scriptURL) {
  return resolve(dirname(fileURLToPath(scriptURL)), '..');
}
