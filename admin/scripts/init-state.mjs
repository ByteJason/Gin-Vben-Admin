import { createHash, randomUUID } from 'node:crypto';
import { accessSync, constants as fsConstants, existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync } from 'node:fs';
import { link, mkdir, open, rename, rm, rmdir } from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Worker } from 'node:worker_threads';

import { processStartToken, validProcessStartToken } from './process-identity.mjs';

const TRANSACTION_ID_PATTERN = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i;
const RECEIPT_TEMP_PATTERN = /^receipt\.json\.tmp-[1-9][0-9]*-[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i;
const PREFLIGHT_FILE_PATTERN = /^\.gin-vben-preflight-[1-9][0-9]*-[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}(?:\.(?:linked|renamed))?$/i;
const PREFLIGHT_DIRECTORY_PATTERN = /^\.gin-vben-preflight-(?:transfer|target)-[1-9][0-9]*-[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i;
const PREFLIGHT_FILE_CONTENTS = 'gin-vben-admin-preflight\n';
// A second init process may inspect a directory while the first process is
// still using its reversible probe. Keep the active paths in-process and use
// the PID embedded in the name to avoid deleting another process's probe.
const ACTIVE_PREFLIGHT_ARTIFACTS = new Set();
const ADMIN_INIT_LEASE_MAX_UNKNOWN_AGE_MS = 60_000;
const ADMIN_INIT_HEARTBEAT_INTERVAL_MS = 5_000;
const ADMIN_INIT_HEARTBEAT_STALE_MS = 60_000;
const ADMIN_INIT_CLOCK_SKEW_MS = 5_000;
const ADMIN_INIT_IDENTITY_UNAVAILABLE_MAX_AGE_MS = 86_400_000;
const DEPENDENCY_INSTALL_OWNER = 'admin-dependency-install';
const DEPENDENCY_IDENTITY_UNAVAILABLE_MAX_AGE_MS = 86_400_000;
const INSTALL_STATE_DIRECTORY_ENV = 'GIN_VBEN_INSTALL_STATE_DIR';
export const UI_SELECTION_MODE_ENV = 'GIN_VBEN_UI_SELECTION_MODE';
export const WORKSPACE_SELECTION_MODE = 'workspace';
export const LEGACY_SELECTION_MODE = 'legacy';
const LOCAL_PROFILE_FILE = '.ui-profile.local.json';
const WORKSPACE_TRANSACTION_OWNER = 'admin-init-workspace';
const WORKSPACE_TRANSACTION_SCHEMA = 1;
const WORKSPACE_RECEIPT_SCHEMA = 1;
const STABLE_INITIALIZATION_ERROR_CODES = new Set([
  'API_UNAVAILABLE',
  'ARGUMENT_INVALID',
  'CLEANUP_CONFIRMATION_REQUIRED',
  'DEPENDENCY_INSTALL_BUSY',
  'DEPENDENCY_INSTALL_FAILED',
  'DEPENDENCY_TRANSACTION_INVALID',
  'INITIALIZATION_IN_PROGRESS',
  'INITIALIZATION_OPERATION_FAILED',
  'INITIALIZATION_RESUME_INVALID',
  'INIT_BUSY',
  'INIT_LEASE_FAILED',
  'INSTALL_STATE_DIR_INVALID',
  'LEGACY_MIGRATION_INVALID',
  'NODE_VERSION_UNSUPPORTED',
  'PNPM_VERSION_UNSUPPORTED',
  'PORT_INVALID',
  'PREFLIGHT_FAILED',
  'RECOVERY_VALIDATION_FAILED',
  'RESET_CONFIRMATION_REQUIRED',
  'RESET_IN_PROGRESS',
  'RESET_LAYOUT_INVALID',
  'RESET_RECEIPT_UNAVAILABLE',
  'RESET_TRANSACTION_INVALID',
  'RESET_UNAVAILABLE',
  'RESET_UNAVAILABLE_INSTALLED',
  'RUNTIME_ENV_APP_INVALID',
  'RUNTIME_ENV_PROFILE_INVALID',
  'RUNTIME_ENV_TARGET_INVALID',
  'RUNTIME_ENV_TEMPLATE_INVALID',
  'SOURCE_MOVE_STATE_INVALID',
  'STATE_INCONSISTENT',
  'TEMPLATE_LAYOUT_INVALID',
  'UI_INVALID',
  'UI_PACKAGE_MISMATCH',
  'UI_PROFILE_INVALID',
  'UI_PROFILE_MISMATCH',
  'UI_PROFILE_REQUIRED',
  'UI_SWITCH_FAILED',
  'WORKSPACE_LAYOUT_INVALID',
  'WORKSPACE_TRANSACTION_INVALID',
]);

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

export const STATE_REASONS = Object.freeze({
  NONE: 'NONE',
  SOURCE_MOVE_TRANSACTION_PRESENT: 'SOURCE_MOVE_TRANSACTION_PRESENT',
  DEPENDENCIES_PENDING: 'DEPENDENCIES_PENDING',
  UI_SWITCH_PENDING: 'UI_SWITCH_PENDING',
  RESET_TRANSACTION_PRESENT: 'RESET_TRANSACTION_PRESENT',
  LEGACY_PREPARED_MIGRATION_PENDING: 'LEGACY_PREPARED_MIGRATION_PENDING',
  LEGACY_PREPARED_STATE_INVALID: 'LEGACY_PREPARED_STATE_INVALID',
  SERVER_INSTALL_TRANSACTION_PRESENT: 'SERVER_INSTALL_TRANSACTION_PRESENT',
  TRANSACTION_INVALID: 'TRANSACTION_INVALID',
  WORKSPACE_TRANSACTION_INVALID: 'WORKSPACE_TRANSACTION_INVALID',
  TRANSACTION_MARKER_CONFLICT: 'TRANSACTION_MARKER_CONFLICT',
  RUNTIME_WITHOUT_PROFILE: 'RUNTIME_WITHOUT_PROFILE',
  MARKER_WITHOUT_PROFILE: 'MARKER_WITHOUT_PROFILE',
  PROFILE_INVALID: 'PROFILE_INVALID',
  RECEIPT_WITHOUT_PROFILE: 'RECEIPT_WITHOUT_PROFILE',
  SELECTED_APP_MISSING: 'SELECTED_APP_MISSING',
  EXTRA_TEMPLATE_PRESENT: 'EXTRA_TEMPLATE_PRESENT',
  RECEIPT_INVALID: 'RECEIPT_INVALID',
  RUNTIME_INVALID: 'RUNTIME_INVALID',
  RUNTIME_RECORD_PRESENT: 'RUNTIME_RECORD_PRESENT',
  RUNTIME_MARKER_CONFLICT: 'RUNTIME_MARKER_CONFLICT',
  MARKER_INVALID: 'MARKER_INVALID',
  MARKER_LOCK_PRESENT: 'MARKER_LOCK_PRESENT',
  INSTALL_STATE_DIR_INVALID: 'INSTALL_STATE_DIR_INVALID',
});

export function stableInitializationErrorCode(error) {
  const candidate = error instanceof Error ? error.message : error?.message;
  return STABLE_INITIALIZATION_ERROR_CODES.has(candidate)
    ? candidate
    : 'INITIALIZATION_OPERATION_FAILED';
}

export function installURL(port) {
  return `http://127.0.0.1:${port}/install`;
}

function pathInside(parent, candidate) {
  const fromParent = relative(parent, candidate);
  return fromParent === '' || (
    !isAbsolute(fromParent)
    && fromParent !== '..'
    && !fromParent.startsWith(`..${process.platform === 'win32' ? '\\' : '/'}`)
  );
}

function canonicalProspectivePath(target) {
  let existing = target;
  const missing = [];
  while (true) {
    try {
      return resolve(realpathSync(existing), ...missing);
    } catch (error) {
      if (error?.code !== 'ENOENT') throw new Error('INSTALL_STATE_DIR_INVALID');
      const parent = dirname(existing);
      if (parent === existing) throw new Error('INSTALL_STATE_DIR_INVALID');
      missing.unshift(basename(existing));
      existing = parent;
    }
  }
}

function installStateRoot(root, environment = process.env) {
  const repositoryRoot = resolve(root, '..');
  if (!Object.hasOwn(environment, INSTALL_STATE_DIRECTORY_ENV)) {
    return resolve(repositoryRoot, '.runtime', 'install');
  }
  const configured = environment[INSTALL_STATE_DIRECTORY_ENV];
  if (typeof configured !== 'string' || configured.trim() === '' || !isAbsolute(configured)) {
    throw new Error('INSTALL_STATE_DIR_INVALID');
  }
  const canonicalRepositoryRoot = realpathSync(repositoryRoot);
  const canonicalAdminRoot = realpathSync(resolve(root));
  const stateRoot = canonicalProspectivePath(resolve(configured));
  if (
    dirname(stateRoot) === stateRoot
    || stateRoot === canonicalRepositoryRoot
    || pathInside(canonicalAdminRoot, stateRoot)
  ) {
    throw new Error('INSTALL_STATE_DIR_INVALID');
  }
  return stateRoot;
}

export function statePaths(root, environment = process.env) {
  const stateRoot = installStateRoot(root, environment);
  return {
    profile: join(root, '.ui-profile.json'),
    // Local selection is deliberately ignored by Git. The tracked profile is
    // retained only as a compatibility read path for older checkouts.
    localProfile: join(root, LOCAL_PROFILE_FILE),
    // Kept only for recognizing and quarantining state written by older releases.
    receipt: join(root, '.ui-init-receipt.json'),
    runtime: join(root, '.ui-init-runtime.json'),
    stateRoot,
    applyLock: join(stateRoot, 'apply.lock'),
    transaction: join(stateRoot, 'transaction.json'),
    workspaceTransaction: join(stateRoot, 'workspace-transaction.json'),
    workspaceReceipt: join(stateRoot, 'workspace-dependencies.json'),
    workspaceSwitchReport: join(stateRoot, 'ui-switch-report.json'),
    workspaceHistoryRoot: join(stateRoot, 'ui-switch-history'),
    adminLease: join(stateRoot, 'admin-init.lock'),
    adminLeaseReclaim: join(stateRoot, 'admin-init.lock.reclaim'),
    adminHeartbeatRoot: join(stateRoot, 'admin-init-heartbeat'),
    dependencyLease: join(stateRoot, 'dependency-install.lock'),
    dependencyLeaseReclaim: join(stateRoot, 'dependency-install.lock.reclaim'),
    dependencyHeartbeatRoot: join(stateRoot, 'dependency-install-heartbeat'),
    dependencyLog: join(stateRoot, 'dependency-install.log'),
    marker: join(stateRoot, '.installed'),
    lock: join(stateRoot, '.installed.lock'),
    backupRoot: join(stateRoot, 'ui-backup'),
    recoveryRoot: join(stateRoot, 'recovery'),
    legacyMigration: join(stateRoot, 'legacy-prepared-migration.json'),
    legacyReceiptIsolationRoot: join(stateRoot, 'legacy-recovery'),
    legacyTransaction: join(root, '.ui-init-transaction.json'),
    legacyMarker: join(root, 'apps', 'install', '.installed'),
    legacyBackupRoot: resolve(root, '..', '.runtime', 'init-backup'),
    legacyRecoveryRoot: resolve(root, '..', '.runtime', 'init-recovery'),
  };
}

function profileFor(selectedUi) {
  const entry = UI_PROFILES[selectedUi];
  return entry ? { schema: 1, selectedUi, ...entry } : null;
}

/**
 * Public profile factory used by the non-destructive selector and by build
 * entrypoints. Keeping the canonical mapping in this module prevents a UI
 * name, package name, and directory from drifting apart across scripts.
 */
export function profileForUI(selectedUi) {
  return profileFor(selectedUi);
}

function configuredUISelection(environment = process.env) {
  const values = ['ADMIN_UI', 'APP_UI']
    .map((key) => ({ key, value: typeof environment?.[key] === 'string' ? environment[key].trim() : '' }))
    .filter(({ value }) => value !== '');
  if (values.length > 1 && new Set(values.map(({ value }) => value)).size !== 1) {
    throw new Error('UI_PROFILE_MISMATCH');
  }
  const selectedUi = values[0]?.value ?? '';
  if (selectedUi && !UI_PROFILES[selectedUi]) throw new Error('UI_INVALID');
  return selectedUi;
}

function profileCandidate(file) {
  const present = pathPresent(file);
  if (!present) return { present: false, profile: null };
  return { present: true, profile: parseProfile(file) };
}

/**
 * Resolve a workspace UI without mutating the repository. Environment values
 * (`ADMIN_UI` and the backwards-compatible `APP_UI` alias) are ephemeral and
 * take precedence over the ignored local profile, which in turn shadows the
 * legacy tracked profile. A malformed profile is reported instead of being
 * silently replaced.
 */
export function resolveWorkspaceProfile(root, environment = process.env) {
  const location = statePaths(root, environment);
  const explicitUi = configuredUISelection(environment);
  const local = profileCandidate(location.localProfile);
  if (local.present && !local.profile) throw new Error('UI_PROFILE_INVALID');
  if (explicitUi) {
    const legacy = profileCandidate(location.profile);
    return {
      profile: profileFor(explicitUi),
      source: 'environment',
      explicitUi,
      localProfile: local.profile,
      legacyProfile: legacy.profile,
    };
  }
  if (local.profile) {
    // A legacy tracked profile may be stale or malformed after an upstream
    // migration. The ignored local selector is authoritative, so do not let
    // that compatibility file shadow a valid local choice.
    const legacy = profileCandidate(location.profile);
    return {
      profile: local.profile,
      source: 'local',
      explicitUi: '',
      localProfile: local.profile,
      legacyProfile: legacy.profile,
    };
  }
  const legacy = profileCandidate(location.profile);
  if (legacy.present && !legacy.profile) throw new Error('UI_PROFILE_INVALID');
  if (legacy.profile) {
    return {
      profile: legacy.profile,
      source: 'legacy',
      explicitUi: '',
      localProfile: null,
      legacyProfile: legacy.profile,
    };
  }
  return {
    profile: null,
    source: 'none',
    explicitUi: '',
    localProfile: null,
    legacyProfile: null,
  };
}

/** Return true when the checkout opts into the non-destructive state model. */
export function workspaceSelectionSignal(root, environment = process.env) {
  const location = statePaths(root, environment);
  const explicitUi = configuredUISelection(environment);
  const currentTransactionPresent = pathPresent(location.transaction);
  const currentTransaction = currentTransactionPresent ? validTransaction(location.transaction) : null;

  // A source-moving journal always wins over new workspace hints. This keeps
  // an upgraded checkout on the compatibility recovery path until its old
  // move transaction has been completed or reset.
  if (currentTransactionPresent && currentTransaction?.owner !== 'server-installer') return false;
  const activeLegacyEvidence = [
    location.legacyMigration,
    location.legacyTransaction,
    location.legacyMarker,
  ].some(pathPresent);
  if (activeLegacyEvidence) return false;

  const hasWorkspaceState = pathPresent(location.localProfile)
    || pathPresent(location.workspaceTransaction)
    || pathPresent(location.workspaceReceipt);
  if (hasWorkspaceState) return true;

  // A checkout upgraded from the source-moving initializer can contain a
  // durable legacy journal/receipt or a non-empty backup root. Let the legacy
  // recovery path finish that transaction instead of treating the manifest as
  // permission to overwrite it with a fresh zero-move profile.
  const passiveLegacyEvidence = [
    location.receipt,
    location.runtime,
    location.backupRoot,
  ].some(pathPresent) || nonEmptyDirectory(location.legacyBackupRoot) || nonEmptyDirectory(location.legacyRecoveryRoot);
  if (passiveLegacyEvidence) return false;
  if (explicitUi) return true;

  // A checked-in workspace manifest opts a real checkout into the
  // non-destructive state model even before a local selector is written.
  // Presence routes malformed/symlinked manifests into the workspace
  // inspector, which fails closed instead of falling back to source moves.
  return pathPresent(join(root, 'pnpm-workspace.yaml'));
}

function nonEmptyDirectory(target) {
  if (!plainDirectory(target)) return false;
  try {
    return readdirSync(target).length > 0;
  } catch {
    return true;
  }
}

const RUNTIME_ENV_MODES = Object.freeze(['development', 'production']);

function preflightSelectedUIRuntimeEnv(root, profile) {
  const expected = profileFor(profile?.selectedUi);
  if (
    !expected
    || profile?.appDirectory !== expected.appDirectory
    || profile?.packageName !== expected.packageName
  ) throw new Error('RUNTIME_ENV_PROFILE_INVALID');
  const appDirectory = join(root, expected.appDirectory);
  if (!plainDirectory(appDirectory)) throw new Error('RUNTIME_ENV_APP_INVALID');
  try {
    accessSync(appDirectory, fsConstants.R_OK | fsConstants.W_OK | fsConstants.X_OK);
  } catch {
    throw new Error('RUNTIME_ENV_TARGET_INVALID');
  }
  for (const mode of RUNTIME_ENV_MODES) {
    const template = join(appDirectory, `.env.${mode}.example`);
    const target = join(appDirectory, `.env.${mode}`);
    if (!plainFile(template)) throw new Error('RUNTIME_ENV_TEMPLATE_INVALID');
    try {
      readFileSync(template);
    } catch {
      throw new Error('RUNTIME_ENV_TEMPLATE_INVALID');
    }
    const targetState = strictPathState(target);
    if (targetState.kind === 'error') throw new Error('RUNTIME_ENV_TARGET_INVALID');
    if (targetState.kind === 'present') {
      if (!targetState.stat.isFile() || targetState.stat.isSymbolicLink()) {
        throw new Error('RUNTIME_ENV_TARGET_INVALID');
      }
      try {
        readFileSync(target);
      } catch {
        throw new Error('RUNTIME_ENV_TARGET_INVALID');
      }
    }
  }
  return expected;
}

/**
 * Materialize the selected UI's ignored runtime env files from tracked,
 * non-secret examples. Existing local configuration always wins. The writes
 * are atomic so an interrupted init or dispatch can safely retry.
 */
export async function ensureSelectedUIRuntimeEnv(root, profile, options = {}) {
  const expected = preflightSelectedUIRuntimeEnv(root, profile);
  const appDirectory = join(root, expected.appDirectory);
  const publish = options.publish ?? publishExclusive;

  for (const mode of RUNTIME_ENV_MODES) {
    const template = join(appDirectory, `.env.${mode}.example`);
    const target = join(appDirectory, `.env.${mode}`);
    if (!plainFile(template)) throw new Error('RUNTIME_ENV_TEMPLATE_INVALID');
    let templateContents;
    try {
      templateContents = readFileSync(template, 'utf8');
    } catch {
      throw new Error('RUNTIME_ENV_TEMPLATE_INVALID');
    }
    const targetState = strictPathState(target);
    if (targetState.kind === 'error') throw new Error('RUNTIME_ENV_TARGET_INVALID');
    if (targetState.kind === 'present') {
      if (!targetState.stat.isFile() || targetState.stat.isSymbolicLink()) {
        throw new Error('RUNTIME_ENV_TARGET_INVALID');
      }
      try {
        readFileSync(target);
      } catch {
        throw new Error('RUNTIME_ENV_TARGET_INVALID');
      }
      continue;
    }
    try {
      await publish(target, templateContents, 'runtime-env');
    } catch (error) {
      if (error?.code !== 'EEXIST') throw new Error('RUNTIME_ENV_TARGET_INVALID');
      const racedTarget = strictPathState(target);
      if (
        racedTarget.kind !== 'present'
        || !racedTarget.stat.isFile()
        || racedTarget.stat.isSymbolicLink()
      ) {
        throw new Error('RUNTIME_ENV_TARGET_INVALID');
      }
      try {
        readFileSync(target);
      } catch {
        throw new Error('RUNTIME_ENV_TARGET_INVALID');
      }
    }
  }
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

function plainFile(target) {
  try {
    const stat = lstatSync(target);
    return stat.isFile() && !stat.isSymbolicLink();
  } catch {
    return false;
  }
}

function pathPresent(target) {
  try {
    lstatSync(target);
    return true;
  } catch {
    return false;
  }
}

function strictPathState(target, inspect = lstatSync) {
  try {
    return { kind: 'present', stat: inspect(target) };
  } catch (error) {
    return error?.code === 'ENOENT' ? { kind: 'missing' } : { kind: 'error', error };
  }
}

// Git can re-materialize only the tracked files changed by an upstream pull
// below a UI directory that init previously moved away. Such a partial path is
// not a pnpm workspace unless its package manifest also exists. Keep malformed
// paths fail-closed, but do not mistake harmless source fragments for another
// installed UI template.
function unselectedWorkspaceSurfacePresent(root, entry) {
  const directory = join(root, entry.appDirectory);
  const directoryState = strictPathState(directory);
  if (directoryState.kind === 'missing') return false;
  if (
    directoryState.kind === 'error'
    || !directoryState.stat.isDirectory()
    || directoryState.stat.isSymbolicLink()
  ) return true;
  return strictPathState(join(directory, 'package.json')).kind !== 'missing';
}

function strictPathPresent(target, code, inspect = lstatSync) {
  const state = strictPathState(target, inspect);
  if (state.kind === 'error') throw new Error(code);
  return state.kind === 'present';
}

function validReceipt(file, profile) {
  const receipt = parseJSON(file);
  if (!receipt || receipt.schema !== 1 || receipt.selectedUi !== profile.selectedUi || typeof receipt.transactionId !== 'string') return null;
  if (!TRANSACTION_ID_PATTERN.test(receipt.transactionId)) return null;
  const expectedMoves = Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== profile.selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
  if (!movesEqual(receipt.moves, expectedMoves)) return null;
  return receipt;
}

function expectedMoves(selectedUi) {
  return Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
}

function movesEqual(actual, expected) {
  return Array.isArray(actual)
    && Array.isArray(expected)
    && actual.length === expected.length
    && actual.every((move, index) => (
      move
      && typeof move === 'object'
      && !Array.isArray(move)
      && Object.keys(move).sort().join(',') === 'backup,source'
      && move.source === expected[index].source
      && move.backup === expected[index].backup
    ));
}

function parseLegacyPreparedReceipt(file, profile) {
  const receipt = parseJSON(file);
  if (
    !receipt
    || Object.keys(receipt).sort().join(',') !== 'moves,schema,selectedUi,transactionId'
    || receipt.schema !== 1
    || receipt.selectedUi !== profile.selectedUi
    || !TRANSACTION_ID_PATTERN.test(receipt.transactionId ?? '')
    || !movesEqual(receipt.moves, expectedMoves(profile.selectedUi))
  ) return null;
  return receipt;
}

function exactPlainDirectoryEntries(directory, expected) {
  if (!plainDirectory(directory)) return false;
  try {
    const entries = readdirSync(directory, { withFileTypes: true });
    const actual = entries.map((entry) => entry.name).sort();
    if (JSON.stringify(actual) !== JSON.stringify([...expected].sort())) return false;
    return entries.every((entry) => entry.isDirectory() && !entry.isSymbolicLink());
  } catch {
    return false;
  }
}

function legacyPreparedCandidate(root) {
  const location = statePaths(root);
  if (strictDirectoryState(location.backupRoot) === 'invalid') return null;
  const profile = parseProfile(location.profile);
  const receipt = profile ? parseLegacyPreparedReceipt(location.receipt, profile) : null;
  if (!profile || !receipt) return null;
  if (
    pathPresent(location.legacyMigration)
    || pathPresent(location.legacyTransaction)
    || pathPresent(location.legacyMarker)
    || pathPresent(location.runtime)
    || pathPresent(location.transaction)
    || pathPresent(location.marker)
    || pathPresent(location.lock)
  ) return null;
  if (!plainDirectory(join(root, profile.appDirectory))) return null;
  if (receipt.moves.some((move) => pathPresent(join(root, move.source)))) return null;
  const transactionDirectory = join(location.legacyBackupRoot, receipt.transactionId);
  if (!exactPlainDirectoryEntries(location.legacyBackupRoot, [receipt.transactionId])) return null;
  if (!exactPlainDirectoryEntries(transactionDirectory, ['apps'])) return null;
  if (!exactPlainDirectoryEntries(join(transactionDirectory, 'apps'), receipt.moves.map((move) => move.backup.slice('apps/'.length)))) return null;
  if (!emptyPlainDirectoryOrMissing(location.backupRoot)) return null;
  if (!plainDirectoryOrMissing(location.legacyReceiptIsolationRoot)) return null;
  if (pathPresent(join(location.legacyReceiptIsolationRoot, receipt.transactionId))) return null;
  return { profile, receipt };
}

function legacyBackupSurface(location) {
  if (!pathPresent(location.legacyBackupRoot)) return false;
  if (!plainDirectory(location.legacyBackupRoot)) return true;
  try {
    return readdirSync(location.legacyBackupRoot).length > 0;
  } catch {
    return true;
  }
}

function inspectLegacyPreparedState(root, profile) {
  const location = statePaths(root);
  const hasReceipt = pathPresent(location.receipt);
  const hasLegacyTransaction = pathPresent(location.legacyTransaction);
  const hasLegacyMarker = pathPresent(location.legacyMarker);
  const hasBackup = legacyBackupSurface(location);
  const preparedSurface = hasLegacyTransaction || hasLegacyMarker || hasBackup || Boolean(profile && hasReceipt);
  if (!preparedSurface) return null;
  const candidate = legacyPreparedCandidate(root);
  if (!candidate) {
    return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.LEGACY_PREPARED_STATE_INVALID };
  }
  return {
    state: STATES.INSTALLING,
    profile: candidate.profile,
    selectedUi: candidate.profile.selectedUi,
    reason: STATE_REASONS.LEGACY_PREPARED_MIGRATION_PENDING,
  };
}

function legacyMigrationFor(receipt) {
  return {
    schema: 1,
    owner: 'admin-init-legacy-migration',
    transactionId: receipt.transactionId,
    selectedUi: receipt.selectedUi,
    moves: receipt.moves,
  };
}

function parseLegacyMigration(file) {
  const migration = parseJSON(file);
  const profile = profileFor(migration?.selectedUi);
  if (
    !migration
    || Object.keys(migration).sort().join(',') !== 'moves,owner,schema,selectedUi,transactionId'
    || migration.schema !== 1
    || migration.owner !== 'admin-init-legacy-migration'
    || !profile
    || !TRANSACTION_ID_PATTERN.test(migration.transactionId ?? '')
    || !movesEqual(migration.moves, expectedMoves(migration.selectedUi))
  ) return null;
  return migration;
}

function migrationCurrentTransaction(migration) {
  return {
    schema: 1,
    owner: 'admin-init',
    id: migration.transactionId,
    selectedUi: migration.selectedUi,
    phase: 'dependencies_pending',
    moves: migration.moves,
  };
}

function legacyReceiptMatchesMigration(receipt, migration) {
  return Boolean(
    receipt
    && receipt.schema === 1
    && receipt.transactionId === migration.transactionId
    && receipt.selectedUi === migration.selectedUi
    && movesEqual(receipt.moves, migration.moves)
  );
}

function legacyMigrationMatches(left, right) {
  return Boolean(
    left
    && right
    && left.schema === right.schema
    && left.owner === right.owner
    && left.transactionId === right.transactionId
    && left.selectedUi === right.selectedUi
    && movesEqual(left.moves, right.moves)
  );
}

function currentTransactionMatchesMigration(file, migration) {
  const transaction = validTransaction(file);
  return Boolean(
    transaction
    && transaction.owner === 'admin-init'
    && transaction.schema === 1
    && transaction.id === migration.transactionId
    && transaction.selectedUi === migration.selectedUi
    && transaction.phase === 'dependencies_pending'
    && movesEqual(transaction.moves, migration.moves)
  );
}

function emptyPlainDirectoryOrMissing(directory) {
  if (!pathPresent(directory)) return true;
  if (!plainDirectory(directory)) return false;
  try {
    return readdirSync(directory).length === 0;
  } catch {
    return false;
  }
}

function plainDirectoryOrMissing(directory) {
  return !pathPresent(directory) || plainDirectory(directory);
}

function strictDirectoryState(directory, inspect = lstatSync) {
  const state = strictPathState(directory, inspect);
  if (state.kind === 'missing') return 'missing';
  if (state.kind === 'error') return 'invalid';
  return state.stat.isDirectory() && !state.stat.isSymbolicLink() ? 'directory' : 'invalid';
}

function assertAppsRoot(root, code) {
  if (strictDirectoryState(join(root, 'apps')) !== 'directory') throw new Error(code);
}

function ensurePlainDirectory(target, code) {
  const initial = strictDirectoryState(target);
  if (initial === 'invalid') throw new Error(code);
  if (initial === 'missing') {
    try {
      mkdirSync(target, { mode: 0o700 });
    } catch {
      throw new Error(code);
    }
  }
  if (strictDirectoryState(target) !== 'directory') throw new Error(code);
}

function exactIsolatedReceiptDirectory(directory) {
  if (!plainDirectory(directory)) return false;
  try {
    const entries = readdirSync(directory, { withFileTypes: true });
    return entries.length === 1
      && entries[0].name === '.ui-init-receipt.json'
      && entries[0].isFile()
      && !entries[0].isSymbolicLink();
  } catch {
    return false;
  }
}

function validLegacyMigrationCheckpoint(root, migration) {
  const location = statePaths(root);
  if (strictDirectoryState(location.backupRoot) === 'invalid') return false;
  const profile = parseProfile(location.profile);
  if (!profile || profile.selectedUi !== migration.selectedUi) return false;
  if (
    !plainDirectory(join(root, profile.appDirectory))
    || migration.moves.some((move) => pathPresent(join(root, move.source)))
    || pathPresent(location.legacyTransaction)
    || pathPresent(location.legacyMarker)
    || pathPresent(location.runtime)
    || !plainDirectoryOrMissing(location.legacyReceiptIsolationRoot)
    || pathPresent(location.marker)
    || pathPresent(location.lock)
  ) return false;

  const oldBackup = join(location.legacyBackupRoot, migration.transactionId);
  const newBackup = join(location.backupRoot, migration.transactionId);
  const oldBackupState = strictPathState(oldBackup);
  const newBackupState = strictPathState(newBackup);
  if (oldBackupState.kind === 'error' || newBackupState.kind === 'error') throw new Error('LEGACY_MIGRATION_INVALID');
  const oldBackupExists = oldBackupState.kind === 'present';
  const newBackupExists = newBackupState.kind === 'present';
  if (oldBackupExists === newBackupExists) return false;
  const activeBackup = oldBackupExists ? oldBackup : newBackup;
  if (
    !exactPlainDirectoryEntries(activeBackup, ['apps'])
    || !exactPlainDirectoryEntries(
      join(activeBackup, 'apps'),
      migration.moves.map((move) => move.backup.slice('apps/'.length)),
    )
  ) return false;
  if (oldBackupExists) {
    if (!exactPlainDirectoryEntries(location.legacyBackupRoot, [migration.transactionId])) return false;
    if (!emptyPlainDirectoryOrMissing(location.backupRoot)) return false;
  } else {
    if (!emptyPlainDirectoryOrMissing(location.legacyBackupRoot)) return false;
    if (!exactPlainDirectoryEntries(location.backupRoot, [migration.transactionId])) return false;
  }

  const isolatedDirectory = join(location.legacyReceiptIsolationRoot, migration.transactionId);
  const isolatedReceipt = join(isolatedDirectory, '.ui-init-receipt.json');
  const oldReceiptExists = pathPresent(location.receipt);
  const isolatedReceiptExists = pathPresent(isolatedReceipt);
  if (oldReceiptExists === isolatedReceiptExists) return false;
  if (oldReceiptExists) {
    if (!legacyReceiptMatchesMigration(parseLegacyPreparedReceipt(location.receipt, profile), migration)) return false;
    if (!emptyPlainDirectoryOrMissing(isolatedDirectory)) return false;
  } else {
    if (!exactIsolatedReceiptDirectory(isolatedDirectory)) return false;
    if (!legacyReceiptMatchesMigration(parseLegacyPreparedReceipt(isolatedReceipt, profile), migration)) return false;
  }
  if (oldBackupExists && !oldReceiptExists) return false;

  const hasCurrentTransaction = pathPresent(location.transaction);
  if (hasCurrentTransaction && !currentTransactionMatchesMigration(location.transaction, migration)) return false;
  if (hasCurrentTransaction && (oldBackupExists || oldReceiptExists)) return false;
  return true;
}

function backupReceipt(root, profile) {
  const location = statePaths(root);
  if (strictDirectoryState(location.backupRoot) !== 'directory') return null;
  let entries;
  try {
    entries = readdirSync(location.backupRoot, { withFileTypes: true });
  } catch {
    return null;
  }
  const expectedMoves = Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== profile.selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
  const matches = entries.flatMap((entry) => {
    if (!entry.isDirectory() || entry.isSymbolicLink()) return [];
    const transactionDirectory = join(location.backupRoot, entry.name);
    let transactionEntries;
    let appEntries;
    try {
      transactionEntries = readdirSync(transactionDirectory, { withFileTypes: true });
      appEntries = readdirSync(join(transactionDirectory, 'apps'), { withFileTypes: true });
    } catch {
      return [];
    }
    const topLevel = transactionEntries
      .map((candidate) => `${candidate.name}:${candidate.isDirectory() && !candidate.isSymbolicLink() ? 'directory' : candidate.isFile() && !candidate.isSymbolicLink() ? 'file' : 'invalid'}`)
      .sort();
    if (JSON.stringify(topLevel) !== JSON.stringify(['apps:directory', 'receipt.json:file'])) return [];
    const expectedApps = expectedMoves.map((move) => move.backup.slice('apps/'.length)).sort();
    const actualApps = appEntries
      .filter((candidate) => candidate.isDirectory() && !candidate.isSymbolicLink())
      .map((candidate) => candidate.name)
      .sort();
    if (appEntries.length !== expectedApps.length || JSON.stringify(actualApps) !== JSON.stringify(expectedApps)) return [];
    const receipt = parseJSON(join(transactionDirectory, 'receipt.json'));
    if (
      !receipt
      || Object.keys(receipt).sort().join(',') !== 'dependenciesReady,moves,owner,schema,selectedUi,transactionId'
      || !TRANSACTION_ID_PATTERN.test(entry.name)
      || receipt.schema !== 1
      || receipt.owner !== 'admin-init'
      || receipt.transactionId !== entry.name
      || receipt.selectedUi !== profile.selectedUi
      || receipt.dependenciesReady !== true
      || JSON.stringify(receipt.moves) !== JSON.stringify(expectedMoves)
    ) return [];
    return [{ ...receipt, directory: join(location.backupRoot, entry.name) }];
  });
  return matches.length === 1 ? matches[0] : null;
}

export function dependenciesPrepared(root, profile) {
  return Boolean(profile && backupReceipt(root, profile));
}

function validTransaction(file) {
  const transaction = parseJSON(file);
  if (transaction?.schema === 1 && transaction.owner === 'server-installer') return transaction;
  const profile = profileFor(transaction?.selectedUi);
  if (!transaction || transaction.schema !== 1 || transaction.owner !== 'admin-init' || !profile || typeof transaction.id !== 'string') return null;
  if (Object.keys(transaction).sort().join(',') !== 'id,moves,owner,phase,schema,selectedUi') return null;
  if (!['moving_ui', 'dependencies_pending', 'resetting_ui'].includes(transaction.phase)) return null;
  if (!TRANSACTION_ID_PATTERN.test(transaction.id)) return null;
  const expectedMoves = Object.entries(UI_PROFILES)
    .filter(([ui]) => ui !== transaction.selectedUi)
    .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
  return movesEqual(transaction.moves, expectedMoves)
    ? transaction
    : null;
}

function validMarker(file, profile) {
  const marker = parseJSON(file);
  return validMarkerContents(marker, profile);
}

function validMarkerContents(marker, profile) {
  if (!marker || marker.schema_version !== 1 || marker.selected_ui !== profile.selectedUi) return false;
  if (typeof marker.installer_version !== 'string' || typeof marker.installed_at !== 'string') return false;
  if (!['embedded', 'standalone', 'api_only', 'dev'].includes(marker.mode)) return false;
  return /^[a-f0-9]{64}$/i.test(marker.artifact_hash ?? '') && /^[a-f0-9]{64}$/i.test(marker.manifest_hash ?? '');
}

function validWorkspaceMarker(file) {
  const marker = parseJSON(file);
  const installationProfile = profileFor(marker?.selected_ui);
  return Boolean(installationProfile && validMarkerContents(marker, installationProfile));
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
  const hasRuntime = pathPresent(location.runtime);
  const hasReceipt = existsSync(location.receipt);
  const hasMarker = strictPathState(location.marker).kind !== 'missing';
  const hasLock = strictPathState(location.lock).kind !== 'missing';
  const hasProfile = strictPathState(location.profile).kind !== 'missing';
  const profile = hasProfile ? parseProfile(location.profile) : null;

  if (pathPresent(location.legacyMigration)) {
    const migration = parseLegacyMigration(location.legacyMigration);
    if (!migration || !validLegacyMigrationCheckpoint(root, migration)) {
      return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.LEGACY_PREPARED_STATE_INVALID };
    }
    return {
      state: STATES.INSTALLING,
      profile,
      selectedUi: migration.selectedUi,
      reason: STATE_REASONS.LEGACY_PREPARED_MIGRATION_PENDING,
    };
  }

  const legacyPrepared = inspectLegacyPreparedState(root, profile);
  if (legacyPrepared) return legacyPrepared;

  if (hasTransaction) {
    const transaction = validTransaction(location.transaction);
    if (!transaction) {
      return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.TRANSACTION_INVALID };
    }
    if (hasMarker) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.TRANSACTION_MARKER_CONFLICT };
    if (transaction.owner === 'server-installer') {
      return { state: STATES.INSTALLING, profile, reason: STATE_REASONS.SERVER_INSTALL_TRANSACTION_PRESENT };
    }
    return {
      state: STATES.INSTALLING,
      profile,
      selectedUi: transaction.selectedUi,
      reason: transaction.phase === 'moving_ui'
        ? STATE_REASONS.SOURCE_MOVE_TRANSACTION_PRESENT
        : transaction.phase === 'dependencies_pending'
          ? STATE_REASONS.DEPENDENCIES_PENDING
          : STATE_REASONS.RESET_TRANSACTION_PRESENT,
    };
  }
  if (hasLock) return { state: STATES.INSTALLING, profile, reason: STATE_REASONS.MARKER_LOCK_PRESENT };
  if (hasRuntime && !profile) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.RUNTIME_WITHOUT_PROFILE };
  if (hasMarker && !profile) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.MARKER_WITHOUT_PROFILE };
  if (hasProfile && !profile) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.PROFILE_INVALID };
  if (hasReceipt && !profile) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.RECEIPT_WITHOUT_PROFILE };
  if (!profile) return { state: STATES.PRISTINE, profile: null, reason: STATE_REASONS.NONE };
  if (!plainDirectory(join(root, profile.appDirectory))) {
    return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.SELECTED_APP_MISSING };
  }
  const extraTemplatePresent = Object.entries(UI_PROFILES)
    .some(([ui, entry]) => ui !== profile.selectedUi && unselectedWorkspaceSurfacePresent(root, entry));
  if (hasReceipt && !validReceipt(location.receipt, profile)) {
    return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.RECEIPT_INVALID };
  }
  if (hasRuntime) {
    if (!validRuntime(location.runtime)) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.RUNTIME_INVALID };
    if (hasMarker) return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.RUNTIME_MARKER_CONFLICT };
    return { state: STATES.INSTALLING, profile, reason: STATE_REASONS.RUNTIME_RECORD_PRESENT };
  }
  if (hasMarker && !validMarker(location.marker, profile)) {
    return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.MARKER_INVALID };
  }
  if (extraTemplatePresent) {
    return { state: STATES.INCONSISTENT, profile, reason: STATE_REASONS.EXTRA_TEMPLATE_PRESENT };
  }
  return { state: hasMarker ? STATES.INSTALLED : STATES.UI_PREPARED, profile, reason: STATE_REASONS.NONE };
}

function pristineTemplateLayout(root) {
  return Object.values(UI_PROFILES).every((entry) => plainDirectory(join(root, entry.appDirectory)))
    && !existsSync(join(root, 'apps', 'web'));
}

// Older releases wrote local receipt/runtime files beside the profile. When
// those files are orphaned in an otherwise pristine checkout, quarantine them
// rather than deleting them so migration remains reversible.
export async function recoverSafeLocalState(root, options = {}) {
  const current = inspectState(root);
  const recoverableReasons = new Set([
    STATE_REASONS.RECEIPT_WITHOUT_PROFILE,
    STATE_REASONS.RUNTIME_WITHOUT_PROFILE,
  ]);
  if (!recoverableReasons.has(current.reason)) return { ...current, recovered: false };

  const location = statePaths(root);
  if (
    existsSync(location.profile)
    || existsSync(location.transaction)
    || existsSync(location.marker)
    || !pristineTemplateLayout(root)
  ) {
    return { ...current, recovered: false };
  }

  const localState = [location.receipt, location.runtime].filter(pathPresent);
  if (localState.some((source) => !plainFile(source))) {
    return { ...current, recovered: false };
  }

  const inspect = options.lstat ?? lstatSync;
  const initialRecoveryState = strictDirectoryState(location.recoveryRoot, inspect);
  if (initialRecoveryState === 'invalid') return { ...current, recovered: false };
  if (initialRecoveryState === 'missing') {
    try {
      mkdirSync(location.recoveryRoot, { mode: 0o700 });
    } catch {
      return { ...current, recovered: false };
    }
  }
  if (strictDirectoryState(location.recoveryRoot, inspect) !== 'directory') {
    return { ...current, recovered: false };
  }

  const recoveryId = randomUUID();
  const recoveryDirectory = join(location.recoveryRoot, recoveryId);
  const candidates = [
    [location.receipt, '.ui-init-receipt.json'],
    [location.runtime, '.ui-init-runtime.json'],
  ].filter(([source]) => pathPresent(source));
  const moved = [];
  try {
    mkdirSync(recoveryDirectory, { mode: 0o700 });
  } catch {
    return { ...current, recovered: false };
  }
  if (
    strictDirectoryState(location.recoveryRoot, inspect) !== 'directory'
    || strictDirectoryState(recoveryDirectory, inspect) !== 'directory'
  ) {
    if (plainDirectory(recoveryDirectory)) await rm(recoveryDirectory, { force: true, recursive: true });
    return { ...current, recovered: false };
  }
  try {
    for (const [source, name] of candidates) {
      const destination = join(recoveryDirectory, name);
      await rename(source, destination);
      moved.push([source, destination]);
    }
    const recovered = inspectState(root);
    if (recovered.state !== STATES.PRISTINE) throw new Error('RECOVERY_VALIDATION_FAILED');
    const repositoryRoot = resolve(root, '..');
    const recoveryBackup = relative(repositoryRoot, recoveryDirectory).split('\\').join('/');
    return {
      ...recovered,
      recovered: true,
      recoveryReason: current.reason,
      recoveryBackup,
    };
  } catch (error) {
    for (const [source, destination] of moved.reverse()) {
      if (existsSync(destination) && !existsSync(source)) await rename(destination, source);
    }
    await rm(recoveryDirectory, { force: true, recursive: true });
    throw error;
  }
}

async function atomicWrite(file, contents) {
  const temporary = `${file}.tmp-${process.pid}-${randomUUID()}`;
  let handle;
  try {
    handle = await open(temporary, 'wx', 0o600);
    await handle.writeFile(contents);
    await handle.sync();
    await handle.close();
    handle = null;
    await rename(temporary, file);
    await syncDirectory(dirname(file));
  } catch (error) {
    await handle?.close().catch(() => {});
    await rm(temporary, { force: true }).catch(() => {});
    throw error;
  }
}

function workspaceFileSnapshot(file) {
  const state = strictPathState(file);
  if (state.kind === 'missing') return { present: false, contents: null };
  if (
    state.kind !== 'present'
    || !state.stat.isFile()
    || state.stat.isSymbolicLink()
  ) throw new Error('UI_SWITCH_FAILED');
  try {
    return { present: true, contents: readFileSync(file) };
  } catch {
    throw new Error('UI_SWITCH_FAILED');
  }
}

async function restoreWorkspaceFile(file, snapshot) {
  if (snapshot.present) {
    await atomicWrite(file, snapshot.contents);
  } else if (pathPresent(file)) {
    await rm(file, { force: true });
    await syncDirectory(dirname(file));
  }
}

function workspaceActiveUIEnvironment(contents, selectedUi) {
  const source = contents.toString('utf8');
  const pattern = /^[ \t]*(?:export[ \t]+)?APP_UI_ACTIVE[ \t]*=[^\r\n]*$/gm;
  const matches = source.match(pattern) ?? [];
  if (matches.length > 1) throw new Error('UI_SWITCH_FAILED');
  const newline = source.includes('\r\n') ? '\r\n' : '\n';
  if (matches.length === 1) {
    return source.replace(/^[ \t]*(?:export[ \t]+)?APP_UI_ACTIVE[ \t]*=[^\r\n]*$/m, `APP_UI_ACTIVE="${selectedUi}"`);
  }
  const separator = source === '' || source.endsWith('\n') ? '' : newline;
  return `${source}${separator}APP_UI_ACTIVE="${selectedUi}"${newline}`;
}

function workspaceLockfileHash(root) {
  const lockfile = join(root, 'pnpm-lock.yaml');
  if (!plainFile(lockfile)) return '';
  try {
    return createHash('sha256').update(readFileSync(lockfile)).digest('hex');
  } catch {
    throw new Error('WORKSPACE_LAYOUT_INVALID');
  }
}

export function requiredWorkspaceLockfileHash(root) {
  const lockfile = join(root, 'pnpm-lock.yaml');
  if (!plainFile(lockfile)) throw new Error('WORKSPACE_LAYOUT_INVALID');
  try {
    const contents = readFileSync(lockfile);
    if (contents.length === 0) throw new Error('WORKSPACE_LAYOUT_INVALID');
    return createHash('sha256').update(contents).digest('hex');
  } catch (error) {
    if (error?.message === 'WORKSPACE_LAYOUT_INVALID') throw error;
    throw new Error('WORKSPACE_LAYOUT_INVALID');
  }
}

function workspaceTemplateLayout(root) {
  const manifest = join(root, 'pnpm-workspace.yaml');
  if (!plainFile(manifest)) return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID' };
  try {
    const contents = readFileSync(manifest, 'utf8');
    if (!/^[ \t]*-[ \t]+['"]?apps\/\*['"]?[ \t]*(?:#.*)?$/m.test(contents)) {
      return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID' };
    }
  } catch {
    return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID' };
  }
  const appsRoot = join(root, 'apps');
  if (!plainDirectory(appsRoot)) return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID' };
  const entries = {};
  for (const [ui, profile] of Object.entries(UI_PROFILES)) {
    const directory = join(root, profile.appDirectory);
    if (!plainDirectory(directory)) {
      return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID', missingUi: ui };
    }
    const packageFile = join(directory, 'package.json');
    if (!plainFile(packageFile)) {
      return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID', missingUi: ui };
    }
    let packageJSON;
    try {
      packageJSON = JSON.parse(readFileSync(packageFile, 'utf8'));
    } catch {
      return { valid: false, allTemplatesPresent: false, reason: 'WORKSPACE_LAYOUT_INVALID', missingUi: ui };
    }
    if (packageJSON?.name !== profile.packageName) {
      return { valid: false, allTemplatesPresent: false, reason: 'UI_PACKAGE_MISMATCH', missingUi: ui };
    }
    entries[ui] = { ...profile, directory };
  }
  return { valid: true, allTemplatesPresent: true, entries };
}

function workspaceTransactionFor(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const keys = Object.keys(value).sort().join(',');
  if (keys !== 'id,moves,owner,phase,schema,selectedUi') return null;
  const profile = profileFor(value.selectedUi);
  if (
    !profile
    || value.schema !== WORKSPACE_TRANSACTION_SCHEMA
    || value.owner !== WORKSPACE_TRANSACTION_OWNER
    || !['dependencies_pending', 'switching_ui'].includes(value.phase)
    || !TRANSACTION_ID_PATTERN.test(value.id ?? '')
    || !Array.isArray(value.moves)
    || value.moves.length !== 0
  ) return null;
  return { ...value, profile };
}

function readWorkspaceTransaction(file) {
  if (!pathPresent(file)) return null;
  return workspaceTransactionFor(parseJSON(file));
}

function workspaceReceiptShape(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const keys = Object.keys(value).sort().join(',');
  if (keys !== 'dependenciesReady,lockfileHash,packageName,schema,selectedUi') return null;
  const receiptProfile = profileFor(value.selectedUi);
  if (
    !receiptProfile
    || value.schema !== WORKSPACE_RECEIPT_SCHEMA
    || value.dependenciesReady !== true
    || value.packageName !== receiptProfile.packageName
    || typeof value.lockfileHash !== 'string'
  ) return null;
  return value;
}

function workspaceReceiptStatus(file, root, profile) {
  if (!pathPresent(file)) return { state: 'missing', receipt: null };
  if (!plainFile(file)) return { state: 'invalid', receipt: null };
  const value = workspaceReceiptShape(parseJSON(file));
  if (!value) return { state: 'invalid', receipt: null };
  if (!profileFor(profile?.selectedUi)) return { state: 'invalid', receipt: null };
  if (value.selectedUi !== profile.selectedUi) return { state: 'foreign', receipt: value };
  let currentHash;
  try {
    currentHash = workspaceLockfileHash(root);
  } catch {
    return { state: 'invalid', receipt: null };
  }
  if (value.lockfileHash !== currentHash) return { state: 'stale', receipt: value };
  return { state: 'ready', receipt: value };
}

export function buildWorkspaceInstallArgs(profile) {
  const expected = profileFor(profile?.selectedUi);
  if (!expected || profile.packageName !== expected.packageName || profile.appDirectory !== expected.appDirectory) {
    throw new Error('UI_PROFILE_INVALID');
  }
  return ['install', '--filter', `${expected.packageName}...`, '--frozen-lockfile'];
}

export function workspaceDependenciesPrepared(root, profile, environment = process.env) {
  if (!profile) return false;
  const location = statePaths(root, environment);
  return workspaceReceiptStatus(location.workspaceReceipt, root, profile).state === 'ready';
}

/**
 * Read-only state projection for the single-repository model. All three UI
 * directories remain tracked; only the selected package is dispatched by the
 * runtime scripts. This projection intentionally does not inspect or create
 * backup directories, so a pull can fast-forward normally.
 */
export function inspectWorkspaceState(root, environment = process.env) {
  let resolved;
  try {
    resolved = resolveWorkspaceProfile(root, environment);
  } catch (error) {
    return { state: STATES.INCONSISTENT, profile: null, reason: error?.message ?? 'UI_PROFILE_INVALID', source: 'invalid' };
  }
  const location = statePaths(root, environment);
  const layout = workspaceTemplateLayout(root);
  const serverTransactionPresent = pathPresent(location.transaction);
  if (serverTransactionPresent) {
    const transaction = validTransaction(location.transaction);
    if (!transaction || transaction.owner !== 'server-installer') {
      return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile?.selectedUi, reason: STATE_REASONS.WORKSPACE_TRANSACTION_INVALID, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
    }
    if (!layout.valid) {
      return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile?.selectedUi, reason: layout.reason, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
    }
    return {
      state: STATES.INSTALLING,
      profile: resolved.profile,
      selectedUi: resolved.profile?.selectedUi,
      reason: STATE_REASONS.SERVER_INSTALL_TRANSACTION_PRESENT,
      source: resolved.source,
      allTemplatesPresent: true,
    };
  }
  const transactionPresent = pathPresent(location.workspaceTransaction);
  if (transactionPresent) {
    const transaction = readWorkspaceTransaction(location.workspaceTransaction);
    if (!transaction) {
      return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile?.selectedUi, reason: STATE_REASONS.WORKSPACE_TRANSACTION_INVALID, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
    }
    if (transaction.phase === 'dependencies_pending' && resolved.profile && resolved.profile.selectedUi !== transaction.selectedUi) {
      return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile.selectedUi, reason: STATE_REASONS.WORKSPACE_TRANSACTION_INVALID, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
    }
    if (!layout.valid) {
      return { state: STATES.INCONSISTENT, profile: transaction.profile, selectedUi: transaction.selectedUi, reason: layout.reason, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
    }
    return {
      state: STATES.INSTALLING,
      profile: transaction.profile,
      selectedUi: transaction.selectedUi,
      reason: transaction.phase === 'switching_ui'
        ? STATE_REASONS.UI_SWITCH_PENDING
        : STATE_REASONS.DEPENDENCIES_PENDING,
      source: resolved.source,
      allTemplatesPresent: true,
      transactionId: transaction.id,
    };
  }
  const markerState = strictPathState(location.marker, lstatSync);
  const markerPresent = markerState.kind !== 'missing';
  const markerLockState = strictPathState(location.lock, lstatSync);
  const markerLockPresent = markerLockState.kind !== 'missing';
  const receiptPresent = strictPathState(location.workspaceReceipt, lstatSync).kind !== 'missing';
  if (!resolved.profile) {
    if (!layout.valid) {
      return { state: STATES.INCONSISTENT, profile: null, reason: layout.reason, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
    }
    if (markerLockPresent) {
      return { state: STATES.INSTALLING, profile: null, reason: STATE_REASONS.MARKER_LOCK_PRESENT, source: resolved.source, allTemplatesPresent: true };
    }
    if (markerPresent) {
      return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.MARKER_WITHOUT_PROFILE, source: resolved.source, allTemplatesPresent: true };
    }
    if (receiptPresent) {
      return { state: STATES.INCONSISTENT, profile: null, reason: STATE_REASONS.RECEIPT_WITHOUT_PROFILE, source: resolved.source, allTemplatesPresent: true };
    }
    return { state: STATES.PRISTINE, profile: null, reason: STATE_REASONS.NONE, source: resolved.source, allTemplatesPresent: true };
  }
  if (!layout.valid) {
    return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile.selectedUi, reason: layout.reason, source: resolved.source, allTemplatesPresent: layout.allTemplatesPresent };
  }
  if (markerLockPresent) {
    return { state: STATES.INSTALLING, profile: resolved.profile, selectedUi: resolved.profile.selectedUi, reason: STATE_REASONS.MARKER_LOCK_PRESENT, source: resolved.source, allTemplatesPresent: true };
  }
  // In workspace mode the immutable installation marker proves backend setup;
  // the ignored profile independently selects the active frontend. A switch
  // therefore keeps the valid marker even when its installation-time UI
  // differs from the current local selection.
  if (markerPresent && !validWorkspaceMarker(location.marker)) {
    return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile.selectedUi, reason: STATE_REASONS.MARKER_INVALID, source: resolved.source, allTemplatesPresent: true };
  }
  const receiptStatus = workspaceReceiptStatus(location.workspaceReceipt, root, resolved.profile);
  if (receiptStatus.state === 'invalid') {
    return { state: STATES.INCONSISTENT, profile: resolved.profile, selectedUi: resolved.profile.selectedUi, reason: STATE_REASONS.RECEIPT_INVALID, source: resolved.source, allTemplatesPresent: true };
  }
  return {
    state: markerPresent ? STATES.INSTALLED : STATES.UI_PREPARED,
    profile: resolved.profile,
    selectedUi: resolved.profile.selectedUi,
    reason: STATE_REASONS.NONE,
    source: resolved.source,
    allTemplatesPresent: true,
    dependenciesReady: receiptStatus.state === 'ready',
    dependenciesStale: receiptStatus.state === 'stale',
    dependenciesForeign: receiptStatus.state === 'foreign',
  };
}

function workspaceSelectionReport(previousUi, selectedUi, changed) {
  const sourceProfile = profileFor(previousUi);
  const targetProfile = profileFor(selectedUi);
  return {
    schema: 1,
    previousUi: previousUi || null,
    selectedUi,
    changed,
    changedBranch: 'selectedUi',
    commonLayer: 'preserved',
    uiSpecific: previousUi && previousUi !== selectedUi ? 'revalidate-adapter' : 'unchanged',
    dependencies: 'selected-only',
    allTemplatesPreserved: true,
    sourceAdapter: sourceProfile?.appDirectory ?? null,
    targetAdapter: targetProfile?.appDirectory ?? null,
    adapterChecks: ['route', 'theme', 'form', 'component'],
    manualVerification: changed && Boolean(previousUi) ? 'required' : 'not-required',
    backendInstallation: 'preserved',
  };
}

function readWorkspaceSelectionReport(file, selectedUi) {
  const report = parseJSON(file);
  if (
    !report
    || report.schema !== 1
    || report.selectedUi !== selectedUi
    || typeof report.changed !== 'boolean'
    || !(report.previousUi === null || Boolean(profileFor(report.previousUi)))
    || report.changedBranch !== 'selectedUi'
    || report.commonLayer !== 'preserved'
    || report.dependencies !== 'selected-only'
    || report.allTemplatesPreserved !== true
    || report.backendInstallation !== 'preserved'
    || JSON.stringify(report.adapterChecks) !== JSON.stringify(['route', 'theme', 'form', 'component'])
  ) return null;
  return report;
}

function preflightWorkspaceSelection(root, location, profile, selectedUi, changed, persistedProfile, options = {}) {
  const stateRootState = strictPathState(location.stateRoot);
  if (
    stateRootState.kind === 'error'
    || (stateRootState.kind === 'present' && (
      !stateRootState.stat.isDirectory()
      || stateRootState.stat.isSymbolicLink()
    ))
  ) throw new Error('UI_SWITCH_FAILED');
  if (strictPathState(location.lock).kind !== 'missing') throw new Error('UI_SWITCH_FAILED');
  if (strictPathState(location.applyLock).kind !== 'missing') throw new Error('INIT_BUSY');
  // The Go installer journal survives a crashed apply owner. Its mere
  // presence is authoritative even when apply.lock has already disappeared;
  // changing UI underneath that recovery would split selectedUi across the
  // backend transaction and the local workspace profile.
  if (strictPathState(location.transaction).kind !== 'missing') throw new Error('INIT_BUSY');
  if (!options.dependencyLeaseOwned && strictPathState(location.dependencyLease).kind !== 'missing') {
    throw new Error('INIT_BUSY');
  }
  if (options.readOnly && !options.leaseOwned && strictPathState(location.adminLease).kind !== 'missing') {
    throw new Error('INIT_BUSY');
  }

  const transactionState = strictPathState(location.workspaceTransaction);
  if (transactionState.kind === 'error' || (transactionState.kind === 'present' && (
    !transactionState.stat.isFile()
    || transactionState.stat.isSymbolicLink()
  ))) throw new Error('WORKSPACE_TRANSACTION_INVALID');
  const transaction = transactionState.kind === 'present'
    ? readWorkspaceTransaction(location.workspaceTransaction)
    : null;
  if (transactionState.kind === 'present' && !transaction) throw new Error('WORKSPACE_TRANSACTION_INVALID');
  if (transaction?.selectedUi !== undefined && transaction.selectedUi !== selectedUi) {
    throw new Error('INITIALIZATION_IN_PROGRESS');
  }
  if (options.readOnly && !options.leaseOwned && transaction) throw new Error('INITIALIZATION_IN_PROGRESS');
  if (transaction?.phase === 'dependencies_pending' && changed) throw new Error('INITIALIZATION_IN_PROGRESS');
  if (transaction?.phase === 'switching_ui' && persistedProfile?.selectedUi === selectedUi) {
    if (!readWorkspaceSelectionReport(location.workspaceSwitchReport, selectedUi)) {
      throw new Error('UI_SWITCH_FAILED');
    }
  }

  const repositoryEnvironment = resolve(root, '..', '.env');
  for (const file of [
    location.localProfile,
    location.workspaceReceipt,
    location.workspaceSwitchReport,
    location.marker,
    repositoryEnvironment,
  ]) workspaceFileSnapshot(file);
  if (changed && pathPresent(location.marker) && !validWorkspaceMarker(location.marker)) {
    throw new Error('UI_SWITCH_FAILED');
  }
  if (changed && pathPresent(location.marker)) {
    const historyRootState = strictPathState(location.workspaceHistoryRoot);
    if (historyRootState.kind === 'error' || (historyRootState.kind === 'present' && (
      !historyRootState.stat.isDirectory()
      || historyRootState.stat.isSymbolicLink()
    ))) throw new Error('UI_SWITCH_FAILED');
  }
  preflightSelectedUIRuntimeEnv(root, profile);
  return transaction;
}

/**
 * Select or switch the active UI in-place. The only repository-local source
 * write is the ignored `.ui-profile.local.json`; no tracked template is moved,
 * deleted, or renamed. `check`/`dryRun` returns the plan without writing.
 */
export async function selectWorkspaceUI(root, selectedUi, options = {}) {
  const profile = profileFor(selectedUi);
  if (!profile) throw new Error('UI_INVALID');
  const location = statePaths(root, options.environment ?? process.env);
  const layout = workspaceTemplateLayout(root);
  if (!layout.valid) throw new Error(layout.reason);
  const resolved = resolveWorkspaceProfile(root, options.environment ?? process.env);
  if (resolved.explicitUi && resolved.explicitUi !== selectedUi) {
    throw new Error('UI_PROFILE_MISMATCH');
  }
  // Environment selection controls this invocation but is not persisted. A
  // switch must compare against the durable local/legacy choice so old
  // dependency metadata and markers are invalidated for the real previous UI.
  const persistedProfile = resolved.localProfile ?? resolved.legacyProfile;
  const previousUi = persistedProfile?.selectedUi ?? '';
  const changed = previousUi !== selectedUi;
  const report = workspaceSelectionReport(previousUi, selectedUi, changed);
  const result = {
    profile,
    previousUi,
    changed,
    source: 'local',
    report,
    plan: {
      retain: profile.appDirectory,
      preserve: Object.values(UI_PROFILES).map((entry) => entry.appDirectory),
      dependencyArgs: buildWorkspaceInstallArgs(profile),
    },
  };
  const readOnly = options.check === true || options.dryRun === true;
  const continueToDependencies = options.continueToDependencies === true;
  preflightWorkspaceSelection(root, location, profile, selectedUi, changed, persistedProfile, {
    readOnly,
    leaseOwned: options.leaseOwned === true,
    dependencyLeaseOwned: options.dependencyLeaseOwned === true,
  });
  if (readOnly) return { ...result, dryRun: true };

  const stateRootState = strictPathState(location.stateRoot);
  if (stateRootState.kind === 'error' || (stateRootState.kind === 'present' && (!stateRootState.stat.isDirectory() || stateRootState.stat.isSymbolicLink()))) {
    throw new Error('UI_SWITCH_FAILED');
  }
  mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
  if (strictPathState(location.lock).kind !== 'missing') throw new Error('UI_SWITCH_FAILED');

  const transactionSnapshot = workspaceFileSnapshot(location.workspaceTransaction);
  const existingTransaction = transactionSnapshot.present
    ? readWorkspaceTransaction(location.workspaceTransaction)
    : null;
  if (transactionSnapshot.present && !existingTransaction) throw new Error('WORKSPACE_TRANSACTION_INVALID');
  if (existingTransaction?.selectedUi !== undefined && existingTransaction.selectedUi !== selectedUi) {
    throw new Error('INITIALIZATION_IN_PROGRESS');
  }
  if (existingTransaction?.phase === 'dependencies_pending') {
    if (changed) throw new Error('INITIALIZATION_IN_PROGRESS');
    return { ...result, dependencyPreparationPending: true };
  }
  if (existingTransaction?.phase === 'switching_ui' && persistedProfile?.selectedUi === selectedUi) {
    // The local selector is committed last. A matching selector proves that
    // the report, environment and receipt changes completed before a crash.
    const recoveredReport = readWorkspaceSelectionReport(location.workspaceSwitchReport, selectedUi);
    if (!recoveredReport) throw new Error('UI_SWITCH_FAILED');
    if (continueToDependencies) {
      await atomicWrite(location.workspaceTransaction, `${JSON.stringify({
        ...existingTransaction,
        phase: 'dependencies_pending',
      }, null, 2)}\n`);
    } else {
      await rm(location.workspaceTransaction, { force: true });
      await syncDirectory(location.stateRoot);
    }
    return {
      ...result,
      previousUi: recoveredReport.previousUi ?? '',
      changed: recoveredReport.changed,
      report: recoveredReport,
      resumed: true,
      dependencyPreparationPending: continueToDependencies,
    };
  }
  const switchTransaction = existingTransaction?.phase === 'switching_ui'
    ? existingTransaction
    : changed || continueToDependencies
      ? {
          schema: WORKSPACE_TRANSACTION_SCHEMA,
          owner: WORKSPACE_TRANSACTION_OWNER,
          id: randomUUID(),
          selectedUi,
          phase: 'switching_ui',
          moves: [],
        }
      : null;

  // Prepare ignored app-local env files before changing the authoritative
  // selector. A validation failure therefore leaves the active UI untouched.
  await ensureSelectedUIRuntimeEnv(root, profile);

  const repositoryEnvironment = resolve(root, '..', '.env');
  const profileSnapshot = workspaceFileSnapshot(location.localProfile);
  const receiptSnapshot = workspaceFileSnapshot(location.workspaceReceipt);
  const reportSnapshot = workspaceFileSnapshot(location.workspaceSwitchReport);
  const markerSnapshot = workspaceFileSnapshot(location.marker);
  const environmentSnapshot = workspaceFileSnapshot(repositoryEnvironment);
  const receiptStatus = workspaceReceiptStatus(location.workspaceReceipt, root, profile);
  const historyFile = switchTransaction && markerSnapshot.present
    ? join(location.workspaceHistoryRoot, `${switchTransaction.id}.installed.json`)
    : '';
  const historySnapshot = historyFile ? workspaceFileSnapshot(historyFile) : null;
  try {
    if (switchTransaction && !existingTransaction) {
      await atomicWrite(location.workspaceTransaction, `${JSON.stringify(switchTransaction, null, 2)}\n`);
    }
    await options.afterSwitchTransactionWrite?.();

    if (changed && markerSnapshot.present) {
      // The backend installation marker is immutable audit evidence. Archive a
      // byte-identical copy, but keep the original in place: workspace UI
      // selection and backend installation are deliberately separate states.
      if (!validWorkspaceMarker(location.marker)) throw new Error('UI_SWITCH_FAILED');
      const historyRootState = strictPathState(location.workspaceHistoryRoot);
      if (historyRootState.kind === 'error' || (historyRootState.kind === 'present' && (!historyRootState.stat.isDirectory() || historyRootState.stat.isSymbolicLink()))) {
        throw new Error('UI_SWITCH_FAILED');
      }
      mkdirSync(location.workspaceHistoryRoot, { recursive: true, mode: 0o700 });
      await atomicWrite(historyFile, markerSnapshot.contents);
      result.markerArchived = relative(resolve(root, '..'), historyFile).split('\\').join('/');
      result.installationMarkerPreserved = true;
    }

    await atomicWrite(location.workspaceSwitchReport, `${JSON.stringify(report, null, 2)}\n`);
    await options.afterReportWrite?.();

    if (changed && markerSnapshot.present && environmentSnapshot.present) {
      await atomicWrite(
        repositoryEnvironment,
        workspaceActiveUIEnvironment(environmentSnapshot.contents, selectedUi),
      );
      result.runtimeEnvironmentUpdated = true;
    }
    await options.afterEnvironmentWrite?.();

    if (receiptSnapshot.present && (changed || receiptStatus.state !== 'ready')) {
      await rm(location.workspaceReceipt, { force: true });
      await syncDirectory(location.stateRoot);
    }
    await options.afterReceiptRemoval?.();

    // Commit the selector last. Every earlier mutation is derived metadata, so
    // an interruption before this atomic rename still dispatches the old UI.
    await atomicWrite(location.localProfile, `${JSON.stringify(profile, null, 2)}\n`);
    await options.afterProfileWrite?.();
    if (switchTransaction) {
      const owned = readWorkspaceTransaction(location.workspaceTransaction);
      if (!owned || owned.id !== switchTransaction.id || owned.phase !== 'switching_ui') {
        throw new Error('UI_SWITCH_FAILED');
      }
      if (continueToDependencies) {
        await atomicWrite(location.workspaceTransaction, `${JSON.stringify({
          ...switchTransaction,
          phase: 'dependencies_pending',
        }, null, 2)}\n`);
        result.dependencyPreparationPending = true;
      } else {
        await rm(location.workspaceTransaction, { force: true });
        await syncDirectory(location.stateRoot);
      }
    }
  } catch (error) {
    // Restore every primary file byte-for-byte when an in-process failure is
    // observed. The archived marker is additive evidence and is removed only
    // after the old coherent state has been restored.
    let rollbackFailed = false;
    for (const [file, snapshot] of [
      [location.localProfile, profileSnapshot],
      [location.workspaceReceipt, receiptSnapshot],
      [location.workspaceSwitchReport, reportSnapshot],
      [repositoryEnvironment, environmentSnapshot],
      [location.marker, markerSnapshot],
    ]) {
      try {
        await restoreWorkspaceFile(file, snapshot);
      } catch {
        rollbackFailed = true;
      }
    }
    if (historyFile) {
      try {
        await restoreWorkspaceFile(historyFile, historySnapshot);
      } catch {
        rollbackFailed = true;
      }
    }
    // Restore the blocking authority last. Removing the switching journal while
    // a selector/report compensation failed would expose a partial switch as a
    // completed state to public build/dev commands.
    if (!rollbackFailed) {
      try {
        await restoreWorkspaceFile(location.workspaceTransaction, transactionSnapshot);
      } catch {
        rollbackFailed = true;
      }
    }
    if (rollbackFailed && switchTransaction) {
      try {
        const blockingTransaction = { ...switchTransaction, phase: 'switching_ui' };
        const currentTransaction = readWorkspaceTransaction(location.workspaceTransaction);
        if (
          !currentTransaction
          || currentTransaction.id !== blockingTransaction.id
          || currentTransaction.phase !== 'switching_ui'
        ) {
          await atomicWrite(
            location.workspaceTransaction,
            `${JSON.stringify(blockingTransaction, null, 2)}\n`,
          );
        }
      } catch {
        // Keep the stable failure and every remaining artifact as recovery
        // evidence when even the blocking journal cannot be republished.
      }
    }
    if (error?.message === 'UI_SWITCH_FAILED') throw error;
    throw new Error('UI_SWITCH_FAILED');
  }
  return result;
}

export async function initializeWorkspace(root, selectedUi, options = {}) {
  const environment = options.environment ?? process.env;
  const profile = profileFor(selectedUi || configuredUISelection(environment));
  if (!profile) throw new Error('UI_INVALID');
  const location = statePaths(root, environment);
  const layout = workspaceTemplateLayout(root);
  if (!layout.valid) throw new Error(layout.reason);
  const current = inspectWorkspaceState(root, environment);
  if (current.state === STATES.INCONSISTENT && current.reason !== STATE_REASONS.RECEIPT_INVALID) {
    throw new Error(current.reason || 'WORKSPACE_LAYOUT_INVALID');
  }
  const resolved = resolveWorkspaceProfile(root, environment);
  const persistedProfile = resolved.localProfile ?? resolved.legacyProfile;
  const receiptStatus = workspaceReceiptStatus(location.workspaceReceipt, root, profile);
  if (
    current.state === STATES.INSTALLED
    && current.selectedUi === profile.selectedUi
    && persistedProfile?.selectedUi === profile.selectedUi
    && ['missing', 'ready'].includes(receiptStatus.state)
  ) {
    return { ...current, profile, repeated: true, mode: WORKSPACE_SELECTION_MODE };
  }
  const pendingBeforeSelection = readWorkspaceTransaction(location.workspaceTransaction);
  if (pathPresent(location.workspaceTransaction) && !pendingBeforeSelection) throw new Error('WORKSPACE_TRANSACTION_INVALID');
  if (pendingBeforeSelection && pendingBeforeSelection.selectedUi !== profile.selectedUi) throw new Error('INITIALIZATION_IN_PROGRESS');
  await selectWorkspaceUI(root, profile.selectedUi, {
    environment,
    leaseOwned: options.leaseOwned === true,
    dependencyLeaseOwned: options.dependencyLeaseOwned === true,
    continueToDependencies: receiptStatus.state !== 'ready',
  });
  await options.afterSelection?.();
  const existing = readWorkspaceTransaction(location.workspaceTransaction);
  if (pathPresent(location.workspaceTransaction) && !existing) throw new Error('WORKSPACE_TRANSACTION_INVALID');
  let transaction = existing;
  if (!transaction && workspaceDependenciesPrepared(root, profile, environment)) {
    return { state: STATES.UI_PREPARED, profile, repeated: true, transactionId: '', mode: WORKSPACE_SELECTION_MODE };
  }
  if (!transaction) {
    transaction = {
      schema: WORKSPACE_TRANSACTION_SCHEMA,
      owner: WORKSPACE_TRANSACTION_OWNER,
      id: randomUUID(),
      selectedUi: profile.selectedUi,
      phase: 'dependencies_pending',
      moves: [],
    };
    await atomicWrite(location.workspaceTransaction, `${JSON.stringify(transaction, null, 2)}\n`);
  }
  try {
    await ensureSelectedUIRuntimeEnv(root, profile);
  } catch (error) {
    if (error?.message !== 'RUNTIME_ENV_TEMPLATE_INVALID') throw error;
  }
  return {
    state: STATES.UI_PREPARED,
    profile,
    repeated: Boolean(pendingBeforeSelection),
    transactionId: transaction.id,
    mode: WORKSPACE_SELECTION_MODE,
  };
}

export async function completeWorkspaceDependencyPreparation(root, profile, options = {}) {
  const expected = profileFor(profile?.selectedUi);
  if (
    !expected
    || profile?.schema !== expected.schema
    || profile?.packageName !== expected.packageName
    || profile?.appDirectory !== expected.appDirectory
  ) throw new Error('UI_PROFILE_INVALID');
  const environment = options.environment ?? process.env;
  const location = statePaths(root, environment);
  const transactionPresent = pathPresent(location.workspaceTransaction);
  const transaction = transactionPresent ? readWorkspaceTransaction(location.workspaceTransaction) : null;
  if (transactionPresent && (!transaction || (
    transaction.selectedUi !== expected.selectedUi
    || transaction.phase !== 'dependencies_pending'
  ))) throw new Error('WORKSPACE_TRANSACTION_INVALID');
  const layout = workspaceTemplateLayout(root);
  if (!layout.valid) throw new Error(layout.reason);
  const active = resolveWorkspaceProfile(root, environment).profile;
  if (
    !active
    || active.selectedUi !== expected.selectedUi
    || active.packageName !== expected.packageName
    || active.appDirectory !== expected.appDirectory
  ) throw new Error('UI_PROFILE_MISMATCH');
  const lockfileHash = requiredWorkspaceLockfileHash(root);
  if (options.expectedLockfileHash && options.expectedLockfileHash !== lockfileHash) {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
  const receipt = {
    schema: WORKSPACE_RECEIPT_SCHEMA,
    selectedUi: expected.selectedUi,
    packageName: expected.packageName,
    dependenciesReady: true,
    lockfileHash,
  };
  await atomicWrite(location.workspaceReceipt, `${JSON.stringify(receipt, null, 2)}\n`);
  if (pathPresent(location.workspaceTransaction)) {
    await rm(location.workspaceTransaction, { force: true });
    await syncDirectory(location.stateRoot);
  }
  return inspectWorkspaceState(root, environment);
}

export async function resetWorkspaceSelection(root, options = {}) {
  const location = statePaths(root, options.environment ?? process.env);
  // Presence is authoritative here: a malformed or orphaned marker must not
  // turn reset into a false-success path that leaves backend state behind.
  if (strictPathState(location.marker).kind !== 'missing') {
    throw new Error('RESET_UNAVAILABLE_INSTALLED');
  }
  const current = inspectWorkspaceState(root, options.environment ?? process.env);
  // An installed application owns additional server-side state. UI selection
  // reset is intentionally limited to the pre-install phase; use the explicit
  // selector to switch a post-install checkout, which archives the marker and
  // forces dependency revalidation without deleting source trees.
  if (current.state === STATES.INSTALLED) throw new Error('RESET_UNAVAILABLE_INSTALLED');
  if (current.state === STATES.INSTALLING) throw new Error('INITIALIZATION_IN_PROGRESS');
  if (current.state === STATES.INCONSISTENT) {
    throw new Error(current.reason || 'WORKSPACE_LAYOUT_INVALID');
  }
  for (const file of [location.localProfile, location.workspaceTransaction, location.workspaceReceipt, location.workspaceSwitchReport]) {
    if (pathPresent(file)) await rm(file, { force: true });
  }
  await syncDirectory(location.stateRoot).catch(() => {});
  return inspectWorkspaceState(root, options.environment ?? process.env);
}

export async function syncDirectory(directory, options = {}) {
  const platform = options.platform ?? process.platform;
  const openDirectory = options.openDirectory ?? open;
  let handle;
  try {
    handle = await openDirectory(directory, 'r');
  } catch (error) {
    if (platform === 'win32' && ['EACCES', 'EINVAL', 'EISDIR', 'EPERM'].includes(error?.code)) return;
    throw error;
  }
  try {
    await handle.sync();
  } catch (error) {
    if (platform !== 'win32' || !['EACCES', 'EINVAL', 'EISDIR', 'EPERM'].includes(error?.code)) throw error;
  } finally {
    await handle.close();
  }
}

async function publishExclusive(file, contents, label) {
  const temporary = `${file}.${label}-${process.pid}-${randomUUID()}`;
  let handle;
  try {
    handle = await open(temporary, 'wx', 0o600);
    await handle.writeFile(contents);
    await handle.sync();
    await handle.close();
    handle = null;
    await link(temporary, file);
    await syncDirectory(dirname(file));
  } catch (error) {
    await handle?.close().catch(() => {});
    await rm(temporary, { force: true }).catch(() => {});
    throw error;
  }
  await rm(temporary);
  await syncDirectory(dirname(file));
}

async function acquireTransaction(file, contents) {
  await publishExclusive(file, contents, 'claim');
}

export function ensureInstallerApplyIdle(root, options = {}) {
  // The caller already owns admin-init.lock. Go publishes apply.lock first and
  // then checks that admin lease; this reciprocal lstat closes both acquisition
  // orders without Node creating, deleting, or reclaiming Go's guarded lease.
  const inspect = options.lstat ?? lstatSync;
  try {
    inspect(statePaths(root).applyLock);
  } catch (error) {
    if (error?.code === 'ENOENT') return;
    throw new Error('INIT_BUSY');
  }
  throw new Error('INIT_BUSY');
}

export async function migrateLegacyPreparedState(root, options = {}) {
  const location = statePaths(root);
  const hasMigration = pathPresent(location.legacyMigration);
  let migration = hasMigration ? parseLegacyMigration(location.legacyMigration) : null;
  const resumed = Boolean(migration);
  if (hasMigration && (!migration || !validLegacyMigrationCheckpoint(root, migration))) {
    throw new Error('LEGACY_MIGRATION_INVALID');
  }
  if (migration) assertAppsRoot(root, 'LEGACY_MIGRATION_INVALID');
  if (!migration) {
    const candidate = legacyPreparedCandidate(root);
    if (!candidate) return { migrated: false };
    assertAppsRoot(root, 'LEGACY_MIGRATION_INVALID');
    migration = legacyMigrationFor(candidate.receipt);
    mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
    try {
      await acquireTransaction(location.legacyMigration, `${JSON.stringify(migration, null, 2)}\n`);
    } catch (error) {
      if (error?.code === 'EEXIST') throw new Error('INIT_BUSY');
      throw error;
    }
    if (!validLegacyMigrationCheckpoint(root, migration)) throw new Error('LEGACY_MIGRATION_INVALID');
  }

  const profile = parseProfile(location.profile);
  if (!profile || profile.selectedUi !== migration.selectedUi) throw new Error('LEGACY_MIGRATION_INVALID');
  if (
    !plainDirectory(join(root, profile.appDirectory))
    || migration.moves.some((move) => pathPresent(join(root, move.source)))
    || pathPresent(location.legacyTransaction)
    || pathPresent(location.legacyMarker)
    || pathPresent(location.runtime)
    || !plainDirectoryOrMissing(location.legacyReceiptIsolationRoot)
    || pathPresent(location.marker)
    || pathPresent(location.lock)
  ) throw new Error('LEGACY_MIGRATION_INVALID');
  const oldBackup = join(location.legacyBackupRoot, migration.transactionId);
  const newBackup = join(location.backupRoot, migration.transactionId);
  const oldBackupExists = pathPresent(oldBackup);
  const newBackupExists = pathPresent(newBackup);
  if (oldBackupExists === newBackupExists) throw new Error('LEGACY_MIGRATION_INVALID');
  if (oldBackupExists) {
    if (!exactPlainDirectoryEntries(oldBackup, ['apps'])) throw new Error('LEGACY_MIGRATION_INVALID');
    if (!exactPlainDirectoryEntries(join(oldBackup, 'apps'), migration.moves.map((move) => move.backup.slice('apps/'.length)))) {
      throw new Error('LEGACY_MIGRATION_INVALID');
    }
    ensurePlainDirectory(location.backupRoot, 'LEGACY_MIGRATION_INVALID');
    await rename(oldBackup, newBackup);
    await syncDirectory(location.legacyBackupRoot);
    await syncDirectory(location.backupRoot);
    await options.afterBackupMove?.();
  } else {
    if (!exactPlainDirectoryEntries(newBackup, ['apps'])) throw new Error('LEGACY_MIGRATION_INVALID');
    if (!exactPlainDirectoryEntries(join(newBackup, 'apps'), migration.moves.map((move) => move.backup.slice('apps/'.length)))) {
      throw new Error('LEGACY_MIGRATION_INVALID');
    }
  }

  const isolatedDirectory = join(location.legacyReceiptIsolationRoot, migration.transactionId);
  const isolatedReceipt = join(isolatedDirectory, '.ui-init-receipt.json');
  const oldReceiptExists = pathPresent(location.receipt);
  const isolatedReceiptExists = pathPresent(isolatedReceipt);
  let receiptIsolated = false;
  if (oldReceiptExists === isolatedReceiptExists) throw new Error('LEGACY_MIGRATION_INVALID');
  if (oldReceiptExists) {
    const receipt = parseLegacyPreparedReceipt(location.receipt, profile);
    if (!legacyReceiptMatchesMigration(receipt, migration)) throw new Error('LEGACY_MIGRATION_INVALID');
    if (pathPresent(isolatedDirectory) && (!plainDirectory(isolatedDirectory) || readdirSync(isolatedDirectory).length !== 0)) {
      throw new Error('LEGACY_MIGRATION_INVALID');
    }
    if (!plainDirectoryOrMissing(location.legacyReceiptIsolationRoot)) throw new Error('LEGACY_MIGRATION_INVALID');
    if (!pathPresent(location.legacyReceiptIsolationRoot)) {
      mkdirSync(location.legacyReceiptIsolationRoot, { recursive: true, mode: 0o700 });
    }
    if (!plainDirectory(location.legacyReceiptIsolationRoot)) throw new Error('LEGACY_MIGRATION_INVALID');
    if (!pathPresent(isolatedDirectory)) mkdirSync(isolatedDirectory, { mode: 0o700 });
    if (
      !plainDirectory(location.legacyReceiptIsolationRoot)
      || !plainDirectory(isolatedDirectory)
      || readdirSync(isolatedDirectory).length !== 0
    ) throw new Error('LEGACY_MIGRATION_INVALID');
    await rename(location.receipt, isolatedReceipt);
    await syncDirectory(dirname(location.receipt));
    await syncDirectory(isolatedDirectory);
    if (
      !plainDirectory(location.legacyReceiptIsolationRoot)
      || !exactIsolatedReceiptDirectory(isolatedDirectory)
    ) throw new Error('LEGACY_MIGRATION_INVALID');
    receiptIsolated = true;
  } else {
    const isolated = parseLegacyPreparedReceipt(isolatedReceipt, profile);
    if (!legacyReceiptMatchesMigration(isolated, migration)) throw new Error('LEGACY_MIGRATION_INVALID');
    if (
      !plainDirectory(location.legacyReceiptIsolationRoot)
      || !exactIsolatedReceiptDirectory(isolatedDirectory)
    ) {
      throw new Error('LEGACY_MIGRATION_INVALID');
    }
  }
  if (receiptIsolated) await options.afterReceiptIsolation?.();

  const transaction = migrationCurrentTransaction(migration);
  const encodedTransaction = `${JSON.stringify(transaction, null, 2)}\n`;
  let transactionPublished = false;
  if (pathPresent(location.transaction)) {
    if (!currentTransactionMatchesMigration(location.transaction, migration)) throw new Error('LEGACY_MIGRATION_INVALID');
  } else {
    try {
      await acquireTransaction(location.transaction, encodedTransaction);
    } catch (error) {
      if (error?.code === 'EEXIST') throw new Error('INIT_BUSY');
      throw error;
    }
    transactionPublished = true;
  }
  if (transactionPublished) await options.afterTransactionPublish?.();

  const persistedMigration = parseLegacyMigration(location.legacyMigration);
  if (!legacyMigrationMatches(persistedMigration, migration)) {
    throw new Error('LEGACY_MIGRATION_INVALID');
  }
  await rm(location.legacyMigration);
  await syncDirectory(location.stateRoot);
  return { migrated: true, profile, resumed, transactionId: migration.transactionId };
}

function parseAdminLease(contents) {
  try {
    const lease = JSON.parse(contents);
    if (!lease || typeof lease !== 'object' || Array.isArray(lease)) return null;
    const keys = Object.keys(lease).sort().join(',');
    const legacy = lease.schema === 1 && keys === 'createdAt,id,owner,pid,schema';
    const current = lease.schema === 2 && keys === 'createdAt,id,owner,pid,pidStartToken,schema';
    if ((!legacy && !current) || lease.owner !== 'admin-init' || !TRANSACTION_ID_PATTERN.test(lease.id ?? '')) return null;
    if (!Number.isInteger(lease.pid) || lease.pid <= 0 || typeof lease.createdAt !== 'string') return null;
    if (current && !validProcessStartToken(lease.pidStartToken)) return null;
    const createdAt = Date.parse(lease.createdAt);
    if (!Number.isFinite(createdAt) || new Date(createdAt).toISOString() !== lease.createdAt) return null;
    return lease;
  } catch {
    return null;
  }
}

function parseAdminHeartbeat(contents) {
  try {
    const heartbeat = JSON.parse(contents);
    if (!heartbeat || typeof heartbeat !== 'object' || Array.isArray(heartbeat)) return null;
    const keys = Object.keys(heartbeat).sort().join(',');
    const legacy = heartbeat.schema === 1 && keys === 'id,owner,pid,schema,updatedAt';
    const current = heartbeat.schema === 2 && keys === 'id,owner,pid,pidStartToken,schema,updatedAt';
    if ((!legacy && !current) || heartbeat.owner !== 'admin-init' || !TRANSACTION_ID_PATTERN.test(heartbeat.id ?? '')) return null;
    if (!Number.isInteger(heartbeat.pid) || heartbeat.pid <= 0 || typeof heartbeat.updatedAt !== 'string') return null;
    if (current && !validProcessStartToken(heartbeat.pidStartToken)) return null;
    const updatedAt = Date.parse(heartbeat.updatedAt);
    if (!Number.isFinite(updatedAt) || new Date(updatedAt).toISOString() !== heartbeat.updatedAt) return null;
    return heartbeat;
  } catch {
    return null;
  }
}

function parseDependencyLease(contents) {
  try {
    const lease = JSON.parse(contents);
    if (!lease || typeof lease !== 'object' || Array.isArray(lease)) return null;
    if (Object.keys(lease).sort().join(',') !== 'childPid,childStartToken,createdAt,id,owner,schema,supervisorPid,supervisorStartToken') return null;
    if (lease.schema !== 2 || lease.owner !== DEPENDENCY_INSTALL_OWNER || !TRANSACTION_ID_PATTERN.test(lease.id ?? '')) return null;
    if (
      !Number.isInteger(lease.supervisorPid)
      || lease.supervisorPid <= 0
      || !validProcessStartToken(lease.supervisorStartToken)
      || !Number.isInteger(lease.childPid)
      || lease.childPid <= 0
      || !validProcessStartToken(lease.childStartToken)
      || typeof lease.createdAt !== 'string'
    ) return null;
    const createdAt = Date.parse(lease.createdAt);
    if (!Number.isFinite(createdAt) || new Date(createdAt).toISOString() !== lease.createdAt) return null;
    return lease;
  } catch {
    return null;
  }
}

function parseDependencyHeartbeat(contents) {
  try {
    const heartbeat = JSON.parse(contents);
    if (!heartbeat || typeof heartbeat !== 'object' || Array.isArray(heartbeat)) return null;
    if (Object.keys(heartbeat).sort().join(',') !== 'childPid,childStartToken,id,owner,schema,supervisorPid,supervisorStartToken,updatedAt') return null;
    if (
      heartbeat.schema !== 2
      || heartbeat.owner !== DEPENDENCY_INSTALL_OWNER
      || !TRANSACTION_ID_PATTERN.test(heartbeat.id ?? '')
      || !Number.isInteger(heartbeat.supervisorPid)
      || heartbeat.supervisorPid <= 0
      || !validProcessStartToken(heartbeat.supervisorStartToken)
      || !Number.isInteger(heartbeat.childPid)
      || heartbeat.childPid <= 0
      || !validProcessStartToken(heartbeat.childStartToken)
      || typeof heartbeat.updatedAt !== 'string'
    ) return null;
    const updatedAt = Date.parse(heartbeat.updatedAt);
    if (!Number.isFinite(updatedAt) || new Date(updatedAt).toISOString() !== heartbeat.updatedAt) return null;
    return heartbeat;
  } catch {
    return null;
  }
}

function readStateSnapshot(file, parser, options = {}) {
  const inspect = options.lstat ?? lstatSync;
  const read = options.readFile ?? readFileSync;
  let stat;
  try {
    stat = inspect(file);
  } catch (error) {
    if (error?.code === 'ENOENT') return null;
    return { contents: null, error, regular: false, value: null, identity: null };
  }
  const regular = stat.isFile() && !stat.isSymbolicLink();
  const identity = { dev: stat.dev, ino: stat.ino, size: stat.size, mtimeMs: stat.mtimeMs, ctimeMs: stat.ctimeMs };
  let contents = null;
  if (regular && stat.size <= 4_096) {
    try {
      contents = read(file, 'utf8');
    } catch (error) {
      return { contents: null, error, regular, value: null, identity };
    }
  }
  return {
    contents,
    value: contents === null ? null : parser(contents),
    regular,
    identity,
  };
}

function readLeaseSnapshot(file, options = {}) {
  const snapshot = readStateSnapshot(file, parseAdminLease, options);
  return snapshot ? { ...snapshot, lease: snapshot.value } : null;
}

function heartbeatPath(location, lease, channel = 'heartbeat') {
  const suffix = channel === 'owner' ? '.owner.json' : '.json';
  return join(location.adminHeartbeatRoot, `${lease.id}${suffix}`);
}

function readHeartbeatSnapshot(location, lease, channel = 'heartbeat', options = {}) {
  const file = heartbeatPath(location, lease, channel);
  const heartbeatRootState = strictDirectoryState(location.adminHeartbeatRoot, options.lstat ?? lstatSync);
  if (heartbeatRootState === 'missing') return null;
  if (heartbeatRootState !== 'directory') {
    return {
      contents: null,
      error: new Error('INIT_BUSY'),
      file,
      regular: false,
      identity: null,
      heartbeat: null,
    };
  }
  const snapshot = readLeaseSnapshot(file, options);
  if (!snapshot) return null;
  return {
    ...snapshot,
    file,
    heartbeat: snapshot.contents === null ? null : parseAdminHeartbeat(snapshot.contents),
  };
}

function processAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code !== 'ESRCH';
  }
}

function leaseSnapshotMatches(file, snapshot) {
  if (!snapshot?.regular) return false;
  try {
    const stat = lstatSync(file);
    if (!stat.isFile() || stat.isSymbolicLink()) return false;
    const identity = snapshot.identity;
    if (
      stat.dev !== identity.dev
      || stat.ino !== identity.ino
      || stat.size !== identity.size
      || stat.mtimeMs !== identity.mtimeMs
      || stat.ctimeMs !== identity.ctimeMs
    ) return false;
    return snapshot.contents === null || snapshot.contents === readFileSync(file, 'utf8');
  } catch {
    return false;
  }
}

async function removeLeaseSnapshot(file, snapshot) {
  if (!leaseSnapshotMatches(file, snapshot)) return false;
  try {
    await rm(file);
    await syncDirectory(dirname(file));
    return true;
  } catch {
    return false;
  }
}

function dependencyHeartbeatPath(location, lease) {
  return join(location.dependencyHeartbeatRoot, `${lease.id}.json`);
}

function readDependencyLeaseSnapshot(file, options = {}) {
  const snapshot = readStateSnapshot(file, parseDependencyLease, options);
  return snapshot ? { ...snapshot, lease: snapshot.value } : null;
}

function readDependencyHeartbeatSnapshot(location, lease, options = {}) {
  const file = dependencyHeartbeatPath(location, lease);
  const snapshot = readStateSnapshot(file, parseDependencyHeartbeat, options);
  return snapshot ? { ...snapshot, file, heartbeat: snapshot.value } : null;
}

function dependencyHeartbeatFresh(location, lease, now, staleMs, options = {}) {
  const heartbeatRootState = strictDirectoryState(location.dependencyHeartbeatRoot, options.lstat ?? lstatSync);
  if (heartbeatRootState === 'invalid') return true;
  if (heartbeatRootState === 'missing') return false;
  const snapshot = readDependencyHeartbeatSnapshot(location, lease, options);
  if (snapshot?.error) return true;
  if (!snapshot?.regular) return false;
  const heartbeat = snapshot.heartbeat;
  if (
    !heartbeat
    || heartbeat.id !== lease.id
    || heartbeat.supervisorPid !== lease.supervisorPid
    || heartbeat.childPid !== lease.childPid
    || heartbeat.supervisorStartToken !== lease.supervisorStartToken
    || heartbeat.childStartToken !== lease.childStartToken
  ) return now - snapshot.identity.mtimeMs <= staleMs;
  const updatedAt = Date.parse(heartbeat.updatedAt);
  if (updatedAt > now + ADMIN_INIT_CLOCK_SKEW_MS) return now - snapshot.identity.mtimeMs <= staleMs;
  return now - Math.max(updatedAt, snapshot.identity.mtimeMs) <= staleMs;
}

function processGroupAlive(pid) {
  if (process.platform === 'win32') return false;
  try {
    process.kill(-pid, 0);
    return true;
  } catch (error) {
    return error?.code !== 'ESRCH';
  }
}

function resolveProcessStartToken(resolver, pid) {
  try {
    const token = resolver(pid);
    return validProcessStartToken(token) ? token : null;
  } catch {
    return null;
  }
}

function dependencyLeaseActive(snapshot, location, now, heartbeatStaleMs, startTokenResolver, options = {}) {
  if (snapshot?.error || !snapshot?.regular) return true;
  const lease = snapshot.lease;
  if (!lease) return true;
  if (dependencyHeartbeatFresh(location, lease, now, heartbeatStaleMs, options)) return true;
  // The supervisor is a detached process-group leader. pnpm itself runs in its
  // Worker, and POSIX lifecycle descendants inherit this group, so even a
  // killed supervisor cannot make a still-running lifecycle command look idle.
  const supervisorToken = resolveProcessStartToken(startTokenResolver, lease.supervisorPid);
  const childToken = lease.childPid === lease.supervisorPid
    ? supervisorToken
    : resolveProcessStartToken(startTokenResolver, lease.childPid);
  const supervisorAlive = processAlive(lease.supervisorPid);
  const childAlive = lease.childPid === lease.supervisorPid ? supervisorAlive : processAlive(lease.childPid);
  if (supervisorAlive && supervisorToken === lease.supervisorStartToken) return true;
  if (childAlive && childToken === lease.childStartToken) return true;

  const age = now - Date.parse(lease.createdAt);
  const identityUnavailable = (
    (supervisorAlive && supervisorToken === null)
    || (childAlive && childToken === null)
    || (
      process.platform !== 'win32'
      && supervisorToken === null
      && processGroupAlive(lease.supervisorPid)
    )
  );
  return identityUnavailable && age <= DEPENDENCY_IDENTITY_UNAVAILABLE_MAX_AGE_MS;
}

async function cleanupDependencyHeartbeat(location, lease, options = {}) {
  const heartbeatRootState = strictDirectoryState(location.dependencyHeartbeatRoot, options.lstat ?? lstatSync);
  if (heartbeatRootState === 'missing') return;
  if (heartbeatRootState !== 'directory') throw new Error('INIT_BUSY');
  const heartbeat = readDependencyHeartbeatSnapshot(location, lease, options);
  if (heartbeat?.error) throw new Error('INIT_BUSY');
  if (
    heartbeat?.heartbeat?.id === lease.id
    && heartbeat.heartbeat.supervisorPid === lease.supervisorPid
    && heartbeat.heartbeat.childPid === lease.childPid
    && heartbeat.heartbeat.supervisorStartToken === lease.supervisorStartToken
    && heartbeat.heartbeat.childStartToken === lease.childStartToken
  ) await removeLeaseSnapshot(heartbeat.file, heartbeat);
}

async function recoverDependencyLeaseReclaim(location, now, graceMs, heartbeatStaleMs, startTokenResolver, options = {}) {
  const tombstone = readDependencyLeaseSnapshot(location.dependencyLeaseReclaim, options);
  if (!tombstone) return false;
  if (tombstone.error) throw new Error('INIT_BUSY');
  if (!tombstone.regular || now - tombstone.identity.ctimeMs <= graceMs) throw new Error('INIT_BUSY');

  const canonical = readDependencyLeaseSnapshot(location.dependencyLease, options);
  if (canonical) {
    if (canonical.error || dependencyLeaseActive(canonical, location, now, heartbeatStaleMs, startTokenResolver, options)) throw new Error('INIT_BUSY');
    if (!sameLeaseInodeAndContents(canonical, tombstone)) {
      await removeLeaseSnapshot(location.dependencyLeaseReclaim, tombstone);
      return true;
    }
    await rm(location.dependencyLease);
    await syncDirectory(location.stateRoot);
  }
  const currentTombstone = readDependencyLeaseSnapshot(location.dependencyLeaseReclaim, options);
  if (!currentTombstone || !sameLeaseInodeAndContents(currentTombstone, tombstone)) throw new Error('INIT_BUSY');
  if (!await removeLeaseSnapshot(location.dependencyLeaseReclaim, currentTombstone)) throw new Error('INIT_BUSY');
  if (tombstone.lease) await cleanupDependencyHeartbeat(location, tombstone.lease, options);
  return true;
}

async function ensureDependencyInstallIdle(root, options = {}) {
  const location = statePaths(root);
  const now = options.now?.() ?? Date.now();
  const graceMs = options.reclaimGraceMs ?? ADMIN_INIT_LEASE_MAX_UNKNOWN_AGE_MS;
  const heartbeatStaleMs = options.heartbeatStaleMs ?? ADMIN_INIT_HEARTBEAT_STALE_MS;
  const startTokenResolver = options.processStartToken ?? processStartToken;
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const reclaim = readDependencyLeaseSnapshot(location.dependencyLeaseReclaim, options);
    if (reclaim?.error) throw new Error('INIT_BUSY');
    if (reclaim) {
      await recoverDependencyLeaseReclaim(location, now, graceMs, heartbeatStaleMs, startTokenResolver, options);
      continue;
    }
    const existing = readDependencyLeaseSnapshot(location.dependencyLease, options);
    if (existing?.error) throw new Error('INIT_BUSY');
    if (!existing) return;
    if (dependencyLeaseActive(existing, location, now, heartbeatStaleMs, startTokenResolver, options)) throw new Error('INIT_BUSY');
    try {
      await link(location.dependencyLease, location.dependencyLeaseReclaim);
      await syncDirectory(location.stateRoot);
    } catch (error) {
      if (error?.code === 'ENOENT') continue;
      if (error?.code === 'EEXIST') throw new Error('INIT_BUSY');
      throw new Error('INIT_LEASE_FAILED');
    }
    const tombstone = readDependencyLeaseSnapshot(location.dependencyLeaseReclaim, options);
    const canonical = readDependencyLeaseSnapshot(location.dependencyLease, options);
    if (tombstone?.error || canonical?.error) throw new Error('INIT_BUSY');
    if (!sameLeaseInodeAndContents(existing, tombstone) || !sameLeaseInodeAndContents(canonical, tombstone)) {
      throw new Error('INIT_BUSY');
    }
    if (dependencyLeaseActive(canonical, location, Date.now(), heartbeatStaleMs, startTokenResolver, options)) {
      await removeLeaseSnapshot(location.dependencyLeaseReclaim, tombstone);
      throw new Error('INIT_BUSY');
    }
    await rm(location.dependencyLease);
    await syncDirectory(location.stateRoot);
    const currentTombstone = readDependencyLeaseSnapshot(location.dependencyLeaseReclaim, options);
    if (!currentTombstone || !sameLeaseInodeAndContents(currentTombstone, tombstone)) throw new Error('INIT_BUSY');
    if (!await removeLeaseSnapshot(location.dependencyLeaseReclaim, currentTombstone)) throw new Error('INIT_BUSY');
    if (existing.lease) await cleanupDependencyHeartbeat(location, existing.lease, options);
    return;
  }
  throw new Error('INIT_BUSY');
}

function dependencyHeartbeatContents(lease) {
  return `${JSON.stringify({
    schema: 2,
    owner: DEPENDENCY_INSTALL_OWNER,
    id: lease.id,
    supervisorPid: lease.supervisorPid,
    supervisorStartToken: lease.supervisorStartToken,
    childPid: lease.childPid,
    childStartToken: lease.childStartToken,
    updatedAt: new Date().toISOString(),
  })}\n`;
}

async function startDependencyHeartbeat(location, lease, intervalMs) {
  ensurePlainDirectory(location.dependencyHeartbeatRoot, 'DEPENDENCY_INSTALL_FAILED');
  const rootIdentity = directoryIdentity(location.dependencyHeartbeatRoot, 'DEPENDENCY_INSTALL_FAILED');
  assertDirectoryIdentity(location.dependencyHeartbeatRoot, rootIdentity, 'DEPENDENCY_INSTALL_FAILED');
  const file = dependencyHeartbeatPath(location, lease);
  const handle = await open(file, 'wx', 0o600);
  let ownedIdentity;
  let stopped = false;
  let writing = Promise.resolve();
  const writeHeartbeat = async () => {
    assertDirectoryIdentity(location.dependencyHeartbeatRoot, rootIdentity, 'DEPENDENCY_HEARTBEAT_ROOT_REPLACED');
    const pathStat = lstatSync(file);
    const handleStat = await handle.stat();
    if (
      !pathStat.isFile()
      || pathStat.isSymbolicLink()
      || pathStat.dev !== handleStat.dev
      || pathStat.ino !== handleStat.ino
    ) throw new Error('DEPENDENCY_HEARTBEAT_REPLACED');
    const buffer = Buffer.from(dependencyHeartbeatContents(lease));
    const { bytesWritten } = await handle.write(buffer, 0, buffer.length, 0);
    if (bytesWritten !== buffer.length) throw new Error('DEPENDENCY_HEARTBEAT_SHORT_WRITE');
    await handle.truncate(buffer.length);
    await handle.sync();
  };
  try {
    assertDirectoryIdentity(location.dependencyHeartbeatRoot, rootIdentity, 'DEPENDENCY_INSTALL_FAILED');
    await writeHeartbeat();
    const ownedStat = await handle.stat();
    ownedIdentity = { dev: ownedStat.dev, ino: ownedStat.ino };
    assertDirectoryIdentity(location.dependencyHeartbeatRoot, rootIdentity, 'DEPENDENCY_INSTALL_FAILED');
    await syncDirectory(location.dependencyHeartbeatRoot);
  } catch (error) {
    await handle.close().catch(() => {});
    await cleanupDependencyHeartbeat(location, lease);
    throw error;
  }
  const timer = setInterval(() => {
    writing = writing.then(writeHeartbeat).catch(() => {
      clearInterval(timer);
    });
  }, intervalMs);
  return async () => {
    if (stopped) return false;
    stopped = true;
    clearInterval(timer);
    await writing.catch(() => {});
    await handle.close().catch(() => {});
    const before = readDependencyHeartbeatSnapshot(location, lease);
    if (
      before?.heartbeat?.id !== lease.id
      || before.heartbeat.supervisorPid !== lease.supervisorPid
      || before.heartbeat.childPid !== lease.childPid
      || before.heartbeat.supervisorStartToken !== lease.supervisorStartToken
      || before.heartbeat.childStartToken !== lease.childStartToken
      || before.identity.dev !== ownedIdentity.dev
      || before.identity.ino !== ownedIdentity.ino
    ) return false;
    return removeLeaseSnapshot(file, before);
  };
}

export async function acquireDependencyInstallLease(root, options = {}) {
  const location = statePaths(root);
  const runtimeRoot = dirname(location.stateRoot);
  if (existsSync(runtimeRoot) && !plainDirectory(runtimeRoot)) throw new Error('PREFLIGHT_FAILED');
  mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
  if (!plainDirectory(location.stateRoot)) throw new Error('PREFLIGHT_FAILED');
  await ensureDependencyInstallIdle(root, options);

  const adminLeaseId = options.adminLeaseId ?? '';
  if (adminLeaseId && !TRANSACTION_ID_PATTERN.test(adminLeaseId)) {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  const assertAdminAdmission = () => {
    const admin = readLeaseSnapshot(location.adminLease, options);
    if (!admin) {
      if (adminLeaseId) throw new Error('INIT_BUSY');
      return;
    }
    if (
      admin.error
      || !admin.regular
      || !admin.lease
      || !adminLeaseId
      || admin.lease.id !== adminLeaseId
    ) throw new Error('INIT_BUSY');
  };
  // Dependency work and workspace mutation share one ordered lock protocol:
  // admin first, dependency second. A standalone supervisor may proceed only
  // while no admin writer exists; a supervisor launched by that writer must
  // present the exact lease id. Validate on both sides of publication to close
  // the check-then-create race with a newly admitted selector.
  assertAdminAdmission();
  await options.afterAdminLeaseAdmission?.();

  const supervisorPid = options.supervisorPid ?? process.pid;
  const childPid = options.childPid ?? process.pid;
  if (
    !Number.isInteger(supervisorPid)
    || supervisorPid <= 0
    || !Number.isInteger(childPid)
    || childPid <= 0
  ) throw new Error('DEPENDENCY_INSTALL_FAILED');
  const startTokenResolver = options.processStartToken ?? processStartToken;
  const supervisorStartToken = resolveProcessStartToken(startTokenResolver, supervisorPid);
  const childStartToken = childPid === supervisorPid
    ? supervisorStartToken
    : resolveProcessStartToken(startTokenResolver, childPid);
  if (!validProcessStartToken(supervisorStartToken) || !validProcessStartToken(childStartToken)) {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  const lease = {
    schema: 2,
    owner: DEPENDENCY_INSTALL_OWNER,
    id: randomUUID(),
    supervisorPid,
    supervisorStartToken,
    // The pnpm CLI runs in a Worker and shares childPid. On Windows the Job
    // wrapper is supervisorPid, so either live identity keeps recovery busy.
    childPid,
    childStartToken,
    createdAt: new Date().toISOString(),
  };
  const encoded = `${JSON.stringify(lease)}\n`;
  try {
    await publishExclusive(location.dependencyLease, encoded, 'dependency');
  } catch (error) {
    if (error?.code === 'EEXIST') {
      throw new Error(adminLeaseId ? 'INIT_BUSY' : 'DEPENDENCY_INSTALL_BUSY');
    }
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  const owned = readDependencyLeaseSnapshot(location.dependencyLease);
  if (!owned || owned.contents !== encoded) throw new Error('DEPENDENCY_INSTALL_FAILED');
  try {
    assertAdminAdmission();
  } catch (error) {
    await removeLeaseSnapshot(location.dependencyLease, owned);
    throw error;
  }

  let stopHeartbeat;
  try {
    const intervalMs = options.heartbeatIntervalMs ?? ADMIN_INIT_HEARTBEAT_INTERVAL_MS;
    if (!Number.isInteger(intervalMs) || intervalMs < 10) throw new Error('DEPENDENCY_INSTALL_FAILED');
    stopHeartbeat = await startDependencyHeartbeat(location, lease, intervalMs);
  } catch (error) {
    await removeLeaseSnapshot(location.dependencyLease, owned);
    throw error;
  }

  let finalized = false;
  const finalize = async (preserveLease) => {
    if (finalized) return false;
    finalized = true;
    const heartbeatRemoved = await stopHeartbeat();
    if (preserveLease) return heartbeatRemoved;
    return removeLeaseSnapshot(location.dependencyLease, owned);
  };
  return {
    lease,
    abandon: () => finalize(true),
    release: () => finalize(false),
  };
}

function adminHeartbeatMatchesLease(heartbeat, lease) {
  return Boolean(
    heartbeat
    && heartbeat.id === lease.id
    && heartbeat.pid === lease.pid
    && heartbeat.schema === lease.schema
    && (lease.schema === 1 || heartbeat.pidStartToken === lease.pidStartToken)
  );
}

function freshHeartbeat(location, lease, now, staleMs, options = {}) {
  return ['heartbeat', 'owner'].some((channel) => {
    const snapshot = readHeartbeatSnapshot(location, lease, channel, options);
    if (snapshot?.error) return true;
    if (!snapshot?.regular) return false;
    const heartbeat = snapshot.heartbeat;
    if (!adminHeartbeatMatchesLease(heartbeat, lease)) {
      return now - snapshot.identity.mtimeMs <= staleMs;
    }
    const updatedAt = Date.parse(heartbeat.updatedAt);
    if (updatedAt > now + ADMIN_INIT_CLOCK_SKEW_MS) return now - snapshot.identity.mtimeMs <= staleMs;
    return now - Math.max(updatedAt, snapshot.identity.mtimeMs) <= staleMs;
  });
}

function adminLeaseAge(snapshot, now) {
  const createdAt = Date.parse(snapshot.lease.createdAt);
  return createdAt > now + ADMIN_INIT_CLOCK_SKEW_MS
    ? now - snapshot.identity.mtimeMs
    : now - createdAt;
}

function reclaimableLease(
  snapshot,
  now = Date.now(),
  location,
  heartbeatStaleMs = ADMIN_INIT_HEARTBEAT_STALE_MS,
  startTokenResolver = processStartToken,
  options = {},
) {
  if (snapshot?.error || !snapshot?.regular) return false;
  if (location && strictDirectoryState(location.adminHeartbeatRoot, options.lstat ?? lstatSync) === 'invalid') {
    return false;
  }
  if (snapshot.lease) {
    if (
      location
      && ['heartbeat', 'owner'].some((channel) => readHeartbeatSnapshot(location, snapshot.lease, channel, options)?.error)
    ) return false;
    if (!processAlive(snapshot.lease.pid)) return true;
    if (location && freshHeartbeat(location, snapshot.lease, now, heartbeatStaleMs, options)) return false;
    const age = adminLeaseAge(snapshot, now);
    if (snapshot.lease.schema === 1) {
      return age > ADMIN_INIT_IDENTITY_UNAVAILABLE_MAX_AGE_MS;
    }
    const currentToken = resolveProcessStartToken(startTokenResolver, snapshot.lease.pid);
    if (currentToken === snapshot.lease.pidStartToken) return false;
    if (currentToken !== null) return true;
    return age > ADMIN_INIT_IDENTITY_UNAVAILABLE_MAX_AGE_MS;
  }
  return now - snapshot.identity.mtimeMs > ADMIN_INIT_LEASE_MAX_UNKNOWN_AGE_MS;
}

function sameLeaseInodeAndContents(left, right) {
  return Boolean(
    left?.regular
    && right?.regular
    && left.identity.dev === right.identity.dev
    && left.identity.ino === right.identity.ino
    && left.identity.size === right.identity.size
    && left.contents === right.contents,
  );
}

function directoryIdentity(directory, code) {
  const state = strictPathState(directory);
  if (
    state.kind !== 'present'
    || !state.stat.isDirectory()
    || state.stat.isSymbolicLink()
  ) throw new Error(code);
  return { dev: state.stat.dev, ino: state.stat.ino };
}

function assertDirectoryIdentity(directory, identity, code) {
  const current = directoryIdentity(directory, code);
  if (current.dev !== identity.dev || current.ino !== identity.ino) throw new Error(code);
}

async function startHeartbeatChannel(location, lease, intervalMs, channel, rootIdentity) {
  assertDirectoryIdentity(location.adminHeartbeatRoot, rootIdentity, 'INIT_LEASE_FAILED');
  const file = heartbeatPath(location, lease, channel);
  const worker = new Worker(new URL('./init-heartbeat.mjs', import.meta.url), {
    workerData: {
      file,
      heartbeatRoot: location.adminHeartbeatRoot,
      id: lease.id,
      intervalMs,
      pid: lease.pid,
      pidStartToken: lease.pidStartToken,
      rootDev: rootIdentity.dev,
      rootIno: rootIdentity.ino,
    },
  });
  try {
    await new Promise((resolveReady, rejectReady) => {
      let settled = false;
      const finish = (callback, value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timeout);
        worker.off('error', onError);
        worker.off('exit', onExit);
        worker.off('message', onMessage);
        callback(value);
      };
      const onError = () => finish(rejectReady, new Error('INIT_LEASE_FAILED'));
      const onExit = () => finish(rejectReady, new Error('INIT_LEASE_FAILED'));
      const onMessage = (message) => {
        if (message?.type === 'ready') finish(resolveReady);
        else if (message?.type === 'error') finish(rejectReady, new Error('INIT_LEASE_FAILED'));
      };
      const timeout = setTimeout(() => finish(rejectReady, new Error('INIT_LEASE_FAILED')), 5_000);
      worker.on('error', onError);
      worker.on('exit', onExit);
      worker.on('message', onMessage);
    });
    assertDirectoryIdentity(location.adminHeartbeatRoot, rootIdentity, 'INIT_LEASE_FAILED');
    const snapshot = readHeartbeatSnapshot(location, lease, channel);
    if (!adminHeartbeatMatchesLease(snapshot?.heartbeat, lease)) {
      throw new Error('INIT_LEASE_FAILED');
    }
    worker.on('error', () => {});
    return {
      channel,
      file,
      lease,
      ownedIdentity: { dev: snapshot.identity.dev, ino: snapshot.identity.ino },
      worker,
    };
  } catch (error) {
    await worker.terminate().catch(() => {});
    const snapshot = readHeartbeatSnapshot(location, lease, channel);
    if (adminHeartbeatMatchesLease(snapshot?.heartbeat, lease)) {
      await removeLeaseSnapshot(snapshot.file, snapshot);
    }
    throw error;
  }
}

async function startAdminHeartbeat(location, lease, options) {
  const intervalMs = options.heartbeatIntervalMs ?? ADMIN_INIT_HEARTBEAT_INTERVAL_MS;
  const staleMs = options.heartbeatStaleMs ?? ADMIN_INIT_HEARTBEAT_STALE_MS;
  if (!Number.isInteger(intervalMs) || intervalMs < 10 || !Number.isInteger(staleMs) || staleMs < intervalMs * 2) {
    throw new Error('INIT_LEASE_FAILED');
  }
  ensurePlainDirectory(location.adminHeartbeatRoot, 'INIT_LEASE_FAILED');
  const rootIdentity = directoryIdentity(location.adminHeartbeatRoot, 'INIT_LEASE_FAILED');
  const channels = [];
  try {
    channels.push(await startHeartbeatChannel(location, lease, intervalMs, 'heartbeat', rootIdentity));
    channels.push(await startHeartbeatChannel(location, lease, intervalMs, 'owner', rootIdentity));
    assertDirectoryIdentity(location.adminHeartbeatRoot, rootIdentity, 'INIT_LEASE_FAILED');
    await syncDirectory(location.adminHeartbeatRoot);
    return { channels, lease };
  } catch (error) {
    await Promise.all(channels.map(stopHeartbeatChannel));
    throw error;
  }
}

async function stopHeartbeatChannel(controller) {
  const { channel, lease, worker } = controller;
  if (worker.threadId !== -1) {
    await new Promise((resolveStopped) => {
      let settled = false;
      const finish = () => {
        if (settled) return;
        settled = true;
        clearTimeout(timeout);
        worker.off('exit', finish);
        worker.off('message', onMessage);
        resolveStopped();
      };
      const onMessage = (message) => {
        if (message?.type === 'stopped') finish();
      };
      const timeout = setTimeout(async () => {
        await worker.terminate().catch(() => {});
        finish();
      }, 2_000);
      worker.on('exit', finish);
      worker.on('message', onMessage);
      worker.postMessage({ type: 'stop' });
    });
    await worker.terminate().catch(() => {});
  }
  const snapshot = readHeartbeatSnapshot({ adminHeartbeatRoot: dirname(controller.file) }, lease, channel);
  if (
    adminHeartbeatMatchesLease(snapshot?.heartbeat, lease)
    && snapshot.identity.dev === controller.ownedIdentity.dev
    && snapshot.identity.ino === controller.ownedIdentity.ino
  ) await removeLeaseSnapshot(controller.file, snapshot);
}

async function stopAdminHeartbeat(controller) {
  if (!controller) return;
  await Promise.all(controller.channels.map(stopHeartbeatChannel));
}

async function activateOwnedLease(location, owned, lease, options) {
  const dependencyState = strictPathState(location.dependencyLease);
  const dependencyReclaimState = strictPathState(location.dependencyLeaseReclaim);
  if (dependencyState.kind !== 'missing' || dependencyReclaimState.kind !== 'missing') {
    await removeLeaseSnapshot(location.adminLease, owned);
    throw new Error('INIT_BUSY');
  }
  let heartbeat;
  try {
    heartbeat = await startAdminHeartbeat(location, lease, options);
  } catch (error) {
    await stopAdminHeartbeat(heartbeat);
    await removeLeaseSnapshot(location.adminLease, owned);
    throw error;
  }
  let released = false;
  const release = async () => {
    if (released) return false;
    released = true;
    await stopAdminHeartbeat(heartbeat);
    return removeLeaseSnapshot(location.adminLease, owned);
  };
  Object.defineProperty(release, 'lease', {
    configurable: false,
    enumerable: true,
    value: Object.freeze({ ...lease }),
    writable: false,
  });
  return release;
}

async function recoverInterruptedLeaseReclaim(location, now, graceMs, heartbeatStaleMs, startTokenResolver, options = {}) {
  const tombstone = readLeaseSnapshot(location.adminLeaseReclaim, options);
  if (!tombstone) return false;
  if (tombstone.error || !tombstone.regular || now - tombstone.identity.ctimeMs <= graceMs) throw new Error('INIT_BUSY');

  const canonical = readLeaseSnapshot(location.adminLease, options);
  if (canonical) {
    if (!reclaimableLease(canonical, now, location, heartbeatStaleMs, startTokenResolver, options)) throw new Error('INIT_BUSY');
    if (!leaseSnapshotMatches(location.adminLeaseReclaim, tombstone)) throw new Error('INIT_BUSY');
    await rm(location.adminLease);
    await syncDirectory(location.stateRoot);
  }

  try {
    await link(location.adminLeaseReclaim, location.adminLease);
    await syncDirectory(location.stateRoot);
  } catch (error) {
    if (error?.code === 'EEXIST' || error?.code === 'ENOENT') throw new Error('INIT_BUSY');
    throw new Error('INIT_LEASE_FAILED');
  }
  const restored = readLeaseSnapshot(location.adminLease, options);
  const currentTombstone = readLeaseSnapshot(location.adminLeaseReclaim, options);
  if (!sameLeaseInodeAndContents(restored, currentTombstone)) throw new Error('INIT_LEASE_FAILED');
  if (!await removeLeaseSnapshot(location.adminLeaseReclaim, currentTombstone)) throw new Error('INIT_LEASE_FAILED');
  return true;
}

async function reclaimAndPublishLease(location, stale, lease, encoded, options, now, heartbeatStaleMs, startTokenResolver) {
  try {
    await link(location.adminLease, location.adminLeaseReclaim);
    await syncDirectory(location.stateRoot);
  } catch (error) {
    if (error?.code === 'EEXIST' || error?.code === 'ENOENT') throw new Error('INIT_BUSY');
    throw new Error('INIT_LEASE_FAILED');
  }

  const tombstone = readLeaseSnapshot(location.adminLeaseReclaim, options);
  const canonical = readLeaseSnapshot(location.adminLease, options);
  if (!sameLeaseInodeAndContents(tombstone, stale) || !sameLeaseInodeAndContents(canonical, tombstone)) {
    throw new Error('INIT_BUSY');
  }
  if (!reclaimableLease(canonical, now, location, heartbeatStaleMs, startTokenResolver, options)) {
    await removeLeaseSnapshot(location.adminLeaseReclaim, tombstone);
    throw new Error('INIT_BUSY');
  }

  await rm(location.adminLease);
  await syncDirectory(location.stateRoot);
  try {
    await publishExclusive(location.adminLease, encoded, 'candidate');
  } catch (error) {
    try {
      await link(location.adminLeaseReclaim, location.adminLease);
      await syncDirectory(location.stateRoot);
      const restoredTombstone = readLeaseSnapshot(location.adminLeaseReclaim, options);
      await removeLeaseSnapshot(location.adminLeaseReclaim, restoredTombstone);
    } catch {
      // Preserve the tombstone when restoration loses a race; a later run can recover it.
    }
    throw error;
  }

  const owned = readLeaseSnapshot(location.adminLease, options);
  if (!owned || owned.contents !== encoded) throw new Error('INIT_LEASE_FAILED');
  const currentTombstone = readLeaseSnapshot(location.adminLeaseReclaim, options);
  if (!sameLeaseInodeAndContents(currentTombstone, tombstone)) throw new Error('INIT_LEASE_FAILED');
  if (!await removeLeaseSnapshot(location.adminLeaseReclaim, currentTombstone)) throw new Error('INIT_LEASE_FAILED');
  if (stale.lease) {
    for (const channel of ['heartbeat', 'owner']) {
      const staleHeartbeat = readHeartbeatSnapshot(location, stale.lease, channel, options);
      if (staleHeartbeat?.error) throw new Error('INIT_BUSY');
      if (adminHeartbeatMatchesLease(staleHeartbeat?.heartbeat, stale.lease)) {
        await removeLeaseSnapshot(staleHeartbeat.file, staleHeartbeat);
      }
    }
  }
  return activateOwnedLease(location, owned, lease, options);
}

export async function acquireAdminInitLease(root, options = {}) {
  const location = statePaths(root);
  const runtimeRoot = dirname(location.stateRoot);
  if (existsSync(runtimeRoot) && !plainDirectory(runtimeRoot)) throw new Error('PREFLIGHT_FAILED');
  mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
  if (!plainDirectory(location.stateRoot)) throw new Error('PREFLIGHT_FAILED');
  await ensureDependencyInstallIdle(root, options);
  await options.afterDependencyAdmission?.();

  const startTokenResolver = options.processStartToken ?? processStartToken;
  const pidStartToken = resolveProcessStartToken(startTokenResolver, process.pid);
  if (!pidStartToken) throw new Error('INIT_LEASE_FAILED');
  const lease = {
    schema: 2,
    owner: 'admin-init',
    id: randomUUID(),
    pid: process.pid,
    pidStartToken,
    createdAt: new Date().toISOString(),
  };
  const encoded = `${JSON.stringify(lease)}\n`;
  const now = options.now?.() ?? Date.now();
  const reclaimGraceMs = options.reclaimGraceMs ?? ADMIN_INIT_LEASE_MAX_UNKNOWN_AGE_MS;
  const heartbeatIntervalMs = options.heartbeatIntervalMs ?? ADMIN_INIT_HEARTBEAT_INTERVAL_MS;
  const heartbeatStaleMs = options.heartbeatStaleMs ?? ADMIN_INIT_HEARTBEAT_STALE_MS;
  if (
    !Number.isInteger(heartbeatIntervalMs)
    || heartbeatIntervalMs < 10
    || !Number.isInteger(heartbeatStaleMs)
    || heartbeatStaleMs < heartbeatIntervalMs * 2
  ) throw new Error('INIT_LEASE_FAILED');
  for (let attempt = 0; attempt < 5; attempt += 1) {
    if (existsSync(location.adminLeaseReclaim)) {
      await recoverInterruptedLeaseReclaim(location, now, reclaimGraceMs, heartbeatStaleMs, startTokenResolver, options);
      continue;
    }
    try {
      await publishExclusive(location.adminLease, encoded, 'candidate');
      const owned = readLeaseSnapshot(location.adminLease, options);
      if (!owned || owned.contents !== encoded) throw new Error('INIT_LEASE_FAILED');
      return activateOwnedLease(location, owned, lease, options);
    } catch (error) {
      if (error?.code !== 'EEXIST') {
        if (error instanceof Error && error.message === 'INIT_LEASE_FAILED') throw error;
        throw new Error('INIT_LEASE_FAILED');
      }
    }

    const existing = readLeaseSnapshot(location.adminLease, options);
    if (!existing) continue;
    if (!reclaimableLease(existing, now, location, heartbeatStaleMs, startTokenResolver, options)) throw new Error('INIT_BUSY');
    return reclaimAndPublishLease(location, existing, lease, encoded, options, now, heartbeatStaleMs, startTokenResolver);
  }
  throw new Error('INIT_BUSY');
}

function assertTemplateLayout(root) {
  assertAppsRoot(root, 'TEMPLATE_LAYOUT_INVALID');
  for (const entry of Object.values(UI_PROFILES)) {
    if (!plainDirectory(join(root, entry.appDirectory))) throw new Error('TEMPLATE_LAYOUT_INVALID');
  }
  if (existsSync(join(root, 'apps', 'web'))) throw new Error('TEMPLATE_LAYOUT_INVALID');
}

function assertWritableDirectory(target) {
  if (!plainDirectory(target)) throw new Error('PREFLIGHT_FAILED');
  accessSync(target, fsConstants.W_OK | fsConstants.X_OK);
}

function assertBackupRootAvailable(backupRoot) {
  let existing = backupRoot;
  let state = strictPathState(existing);
  while (state.kind === 'missing') {
    const parent = dirname(existing);
    if (parent === existing) throw new Error('PREFLIGHT_FAILED');
    existing = parent;
    state = strictPathState(existing);
  }
  if (state.kind !== 'present') throw new Error('PREFLIGHT_FAILED');
  assertWritableDirectory(existing);
  return existing;
}

class InitializationPreflightError extends Error {
  constructor(scope, operation) {
    super('PREFLIGHT_FAILED');
    this.name = 'InitializationPreflightError';
    this.scope = scope;
    this.operation = operation;
  }
}

function preflightCheck(scope, operation, check) {
  try {
    return check();
  } catch (error) {
    if (error instanceof InitializationPreflightError) throw error;
    throw new InitializationPreflightError(scope, operation);
  }
}

async function probeDirectoryCapabilities(directory, scope, operations, options = {}) {
  await recoverDirectoryPreflightArtifacts(directory, scope, operations);
  const id = `${process.pid}-${randomUUID()}`;
  const created = join(directory, `.gin-vben-preflight-${id}`);
  const linked = `${created}.linked`;
  const renamed = `${created}.renamed`;
  const artifacts = [created, linked, renamed];
  artifacts.forEach((artifact) => ACTIVE_PREFLIGHT_ARTIFACTS.add(artifact));
  let handle;
  let operation = 'create';
  let failure;
  try {
    handle = await operations.open(created, 'wx', 0o600);
    operation = 'write';
    await handle.writeFile(PREFLIGHT_FILE_CONTENTS);
    await handle.sync();
    await handle.close();
    handle = null;
    if (options.requireLink !== false) {
      operation = 'link';
      await operations.link(created, linked);
      operation = 'sync';
      await operations.syncDirectory(directory);
      operation = 'delete';
      await operations.remove(linked);
    }
    operation = 'rename';
    await operations.rename(created, renamed);
    operation = 'sync';
    await operations.syncDirectory(directory);
    operation = 'delete';
    await operations.remove(renamed);
    operation = 'sync';
    await operations.syncDirectory(directory);
  } catch {
    failure = new InitializationPreflightError(scope, operation);
  } finally {
    let cleanupFailed = false;
    try {
      await handle?.close();
    } catch {
      cleanupFailed = true;
    }
    for (const artifact of [created, linked, renamed]) {
      try {
        await operations.cleanup(artifact, { force: true });
      } catch (error) {
        if (error?.code !== 'ENOENT') cleanupFailed = true;
      }
    }
    artifacts.forEach((artifact) => ACTIVE_PREFLIGHT_ARTIFACTS.delete(artifact));
    if (!failure && cleanupFailed) failure = new InitializationPreflightError(scope, 'delete');
  }
  if (failure) throw failure;
}

async function probeUIBackupTransfer(appsRoot, backupParent, operations, options = {}) {
  const sourceScope = options.sourceScope ?? 'admin_apps';
  const targetScope = options.targetScope ?? 'ui_backup';
  const failureScope = options.failureScope ?? 'ui_backup';
  await recoverDirectoryPreflightArtifacts(appsRoot, sourceScope, operations);
  if (backupParent !== appsRoot) {
    await recoverDirectoryPreflightArtifacts(backupParent, targetScope, operations);
  }
  const id = `${process.pid}-${randomUUID()}`;
  const source = join(appsRoot, `.gin-vben-preflight-transfer-${id}`);
  const target = join(backupParent, `.gin-vben-preflight-target-${id}`);
  const artifacts = [source, target];
  artifacts.forEach((artifact) => ACTIVE_PREFLIGHT_ARTIFACTS.add(artifact));
  let operation = 'create';
  let failure;
  try {
    await operations.mkdir(source, { mode: 0o700 });
    operation = 'cross_directory_rename';
    await operations.rename(source, target);
    await operations.rename(target, source);
    operation = 'sync';
    await operations.syncDirectory(appsRoot);
    if (backupParent !== appsRoot) await operations.syncDirectory(backupParent);
    operation = 'delete';
    await operations.remove(source, { force: true, recursive: true });
    operation = 'sync';
    await operations.syncDirectory(appsRoot);
    if (backupParent !== appsRoot) await operations.syncDirectory(backupParent);
  } catch {
    failure = new InitializationPreflightError(failureScope, operation);
  } finally {
    let cleanupFailed = false;
    for (const artifact of [source, target]) {
      try {
        await operations.cleanup(artifact, { force: true, recursive: true });
      } catch (error) {
        if (error?.code !== 'ENOENT') cleanupFailed = true;
      }
    }
    artifacts.forEach((artifact) => ACTIVE_PREFLIGHT_ARTIFACTS.delete(artifact));
    if (!failure && cleanupFailed) failure = new InitializationPreflightError(failureScope, 'delete');
  }
  if (failure) throw failure;
}

function preflightArtifactOwnerPid(name) {
  const match = String(name).match(/^\.gin-vben-preflight-(?:(?:transfer|target)-)?([1-9][0-9]*)-/i);
  if (!match) return null;
  const pid = Number(match[1]);
  return Number.isSafeInteger(pid) && pid > 0 ? pid : null;
}

async function recoverDirectoryPreflightArtifacts(directory, scope, operations) {
  const state = strictPathState(directory);
  if (state.kind === 'missing') return;
  if (state.kind === 'error') throw new InitializationPreflightError(scope, 'read');
  if (!state.stat.isDirectory() || state.stat.isSymbolicLink()) return;
  let entries;
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    throw new InitializationPreflightError(scope, 'read');
  }
  let removed = false;
  for (const entry of entries) {
    const preflightFile = PREFLIGHT_FILE_PATTERN.test(entry.name);
    const preflightDirectory = PREFLIGHT_DIRECTORY_PATTERN.test(entry.name);
    if (!preflightFile && !preflightDirectory) continue;
    const target = join(directory, entry.name);
    const ownerPid = preflightArtifactOwnerPid(entry.name);
    if (ACTIVE_PREFLIGHT_ARTIFACTS.has(target) || (ownerPid && ownerPid !== process.pid && processAlive(ownerPid))) continue;
    let safe = false;
    try {
      const stat = lstatSync(target);
      if (preflightFile && stat.isFile() && !stat.isSymbolicLink()) {
        const contents = readFileSync(target, 'utf8');
        // Only remove a probe when its complete sentinel is present. A
        // prefix/empty match could otherwise delete a user-created file that
        // merely happens to use the reserved-looking name after a crash.
        safe = contents === PREFLIGHT_FILE_CONTENTS;
      } else if (preflightDirectory && stat.isDirectory() && !stat.isSymbolicLink()) {
        safe = readdirSync(target).length === 0;
      }
    } catch (error) {
      if (error?.code === 'ENOENT') continue;
      throw new InitializationPreflightError(scope, 'read');
    }
    if (!safe) throw new InitializationPreflightError(scope, 'read');
    try {
      await operations.cleanup(target, { force: false, recursive: preflightDirectory });
      removed = true;
    } catch {
      throw new InitializationPreflightError(scope, 'delete');
    }
  }
  if (removed) {
    try {
      await operations.syncDirectory(directory);
    } catch {
      throw new InitializationPreflightError(scope, 'sync');
    }
  }
}

async function recoverBackupPreflightArtifacts(root, operations) {
  const backupRoot = statePaths(root).backupRoot;
  const state = strictPathState(backupRoot);
  if (state.kind === 'missing') return;
  if (state.kind === 'error') throw new InitializationPreflightError('ui_backup', 'read');
  if (!state.stat.isDirectory() || state.stat.isSymbolicLink()) return;
  await recoverDirectoryPreflightArtifacts(backupRoot, 'ui_backup', operations);
  let entries;
  try {
    entries = readdirSync(backupRoot, { withFileTypes: true });
  } catch {
    throw new InitializationPreflightError('ui_backup', 'read');
  }
  for (const entry of entries) {
    if (!TRANSACTION_ID_PATTERN.test(entry.name) || !entry.isDirectory() || entry.isSymbolicLink()) continue;
    const transactionDirectory = join(backupRoot, entry.name);
    await recoverDirectoryPreflightArtifacts(transactionDirectory, 'ui_backup', operations);
    const appsDirectory = join(transactionDirectory, 'apps');
    const appsState = strictPathState(appsDirectory);
    if (appsState.kind === 'error') throw new InitializationPreflightError('ui_backup', 'read');
    if (appsState.kind === 'present' && appsState.stat.isDirectory() && !appsState.stat.isSymbolicLink()) {
      await recoverDirectoryPreflightArtifacts(appsDirectory, 'ui_backup', operations);
    }
  }
}

async function recoverUUIDPreflightArtifacts(directory, scope, operations, includeApps = false) {
  const state = strictPathState(directory);
  if (state.kind === 'missing') return;
  if (state.kind === 'error') throw new InitializationPreflightError(scope, 'read');
  if (!state.stat.isDirectory() || state.stat.isSymbolicLink()) return;
  await recoverDirectoryPreflightArtifacts(directory, scope, operations);
  let entries;
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    throw new InitializationPreflightError(scope, 'read');
  }
  for (const entry of entries) {
    if (!TRANSACTION_ID_PATTERN.test(entry.name) || !entry.isDirectory() || entry.isSymbolicLink()) continue;
    const child = join(directory, entry.name);
    await recoverDirectoryPreflightArtifacts(child, scope, operations);
    if (!includeApps) continue;
    const apps = join(child, 'apps');
    const appsState = strictPathState(apps);
    if (appsState.kind === 'error') throw new InitializationPreflightError(scope, 'read');
    if (appsState.kind === 'present' && appsState.stat.isDirectory() && !appsState.stat.isSymbolicLink()) {
      await recoverDirectoryPreflightArtifacts(apps, scope, operations);
    }
  }
}

export async function recoverInitializationPreflightArtifacts(root, options = {}) {
  const operations = initializationPreflightOperations(options);
  const location = statePaths(root);
  // Only strict transaction namespaces need eager cleanup before inspectState.
  // Other directories recover their probes lazily when that operation is
  // actually selected, so merely opening the installer stays side-effect free.
  await recoverBackupPreflightArtifacts(root, operations);
  await recoverUUIDPreflightArtifacts(location.legacyBackupRoot, 'ui_backup', operations, true);
  await recoverUUIDPreflightArtifacts(location.legacyReceiptIsolationRoot, 'state_root', operations);
}

export async function preflightSafeLocalRecovery(root, options = {}) {
  const operations = initializationPreflightOperations(options);
  await recoverInitializationPreflightArtifacts(root, options);
  const current = inspectState(root);
  if (![STATE_REASONS.RECEIPT_WITHOUT_PROFILE, STATE_REASONS.RUNTIME_WITHOUT_PROFILE].includes(current.reason)) {
    return { required: false };
  }
  const location = statePaths(root);
  if (
    existsSync(location.profile)
    || existsSync(location.transaction)
    || existsSync(location.marker)
    || !pristineTemplateLayout(root)
  ) throw new Error('RECOVERY_VALIDATION_FAILED');
  const localState = [location.receipt, location.runtime].filter(pathPresent);
  if (localState.length === 0 || localState.some((source) => !plainFile(source))) {
    throw new Error('RECOVERY_VALIDATION_FAILED');
  }
  const recoveryParent = preflightCheck('state_root', 'create', () => assertBackupRootAvailable(location.recoveryRoot));
  preflightCheck('admin_root', 'execute', () => assertWritableDirectory(root));
  for (const source of localState) preflightExistingFile(source, 'admin_root', 'rename', operations);
  await probeDirectoryCapabilities(root, 'admin_root', operations, { requireLink: false });
  if (recoveryParent !== root) {
    await probeDirectoryCapabilities(recoveryParent, 'state_root', operations, { requireLink: false });
  }
  await probeUIBackupTransfer(root, recoveryParent, operations, {
    sourceScope: 'admin_root',
    targetScope: 'state_root',
    failureScope: 'state_root',
  });
  for (const source of localState) {
    preflightCheck('state_root', 'cross_directory_rename', () => {
      if (operations.deviceOf(source) !== operations.deviceOf(recoveryParent)) {
        throw new Error('PREFLIGHT_FAILED');
      }
    });
  }
  return { required: true, reason: current.reason };
}

export async function preflightLegacyPreparedMigration(root, options = {}) {
  const operations = initializationPreflightOperations(options);
  await recoverInitializationPreflightArtifacts(root, options);
  const location = statePaths(root);
  const hasMigration = pathPresent(location.legacyMigration);
  let migration = hasMigration ? parseLegacyMigration(location.legacyMigration) : null;
  if (hasMigration && (!migration || !validLegacyMigrationCheckpoint(root, migration))) {
    throw new Error('LEGACY_MIGRATION_INVALID');
  }
  if (!migration) {
    const candidate = legacyPreparedCandidate(root);
    if (!candidate) return { required: false };
    migration = legacyMigrationFor(candidate.receipt);
  }
  const profile = parseProfile(location.profile);
  if (!profile || profile.selectedUi !== migration.selectedUi) throw new Error('LEGACY_MIGRATION_INVALID');
  const selectedDirectory = join(root, profile.appDirectory);
  const resetAfterMigration = options.resetAfterMigration === true;
  const stateParent = preflightCheck('state_root', 'create', () => assertBackupRootAvailable(location.stateRoot));
  preflightCheck('admin_root', 'execute', () => assertWritableDirectory(root));
  if (resetAfterMigration) {
    // A legacy reset only reads the selected workspace while restoring the
    // staged templates. It does not publish runtime env files or write into
    // the selected app, so forward-only env templates and its hard-link
    // capability must not block a valid reset.
    preflightCheck('selected_ui', 'read', () => {
      if (!plainDirectory(selectedDirectory)) throw new Error('PREFLIGHT_FAILED');
      accessSync(selectedDirectory, fsConstants.R_OK | fsConstants.X_OK);
    });
  } else {
    preflightCheck('selected_ui', 'execute', () => assertWritableDirectory(selectedDirectory));
    preflightCheck('selected_ui', 'read', () => assertSelectedUIRuntimeInputs(root, profile));
  }
  preflightExistingFile(location.profile, 'admin_root', resetAfterMigration ? 'delete' : 'write', operations);
  preflightExistingFile(location.transaction, 'state_root', resetAfterMigration ? 'delete' : 'write', operations);
  await probeDirectoryCapabilities(root, 'admin_root', operations, { requireLink: false });
  if (!resetAfterMigration) await probeDirectoryCapabilities(selectedDirectory, 'selected_ui', operations);
  preflightExistingFile(location.legacyMigration, 'state_root', 'delete', operations);
  await probeDirectoryCapabilities(stateParent, 'state_root', operations);

  const oldBackup = join(location.legacyBackupRoot, migration.transactionId);
  const newBackup = join(location.backupRoot, migration.transactionId);
  const oldBackupPresent = pathPresent(oldBackup);
  if (oldBackupPresent) {
    const newBackupParent = preflightCheck('ui_backup', 'create', () => assertBackupRootAvailable(location.backupRoot));
    preflightCheck('ui_backup', 'execute', () => assertWritableDirectory(location.legacyBackupRoot));
    await probeDirectoryCapabilities(location.legacyBackupRoot, 'ui_backup', operations, { requireLink: false });
    if (newBackupParent !== stateParent && newBackupParent !== location.legacyBackupRoot) {
      await probeDirectoryCapabilities(newBackupParent, 'ui_backup', operations, { requireLink: false });
    }
    await probeUIBackupTransfer(location.legacyBackupRoot, newBackupParent, operations, {
      sourceScope: 'ui_backup',
      targetScope: 'ui_backup',
    });
    preflightCheck('ui_backup', 'cross_directory_rename', () => {
      if (operations.deviceOf(oldBackup) !== operations.deviceOf(newBackupParent)) {
        throw new Error('PREFLIGHT_FAILED');
      }
    });
  } else if (!pathPresent(newBackup)) {
    throw new Error('LEGACY_MIGRATION_INVALID');
  }
  const activeBackup = oldBackupPresent ? oldBackup : newBackup;
  await probeDirectoryCapabilities(activeBackup, 'ui_backup', operations, { requireLink: false });
  preflightExistingFile(join(activeBackup, 'receipt.json'), 'ui_backup', resetAfterMigration ? 'delete' : 'write', operations);

  if (options.resetAfterMigration) {
    const appsRoot = join(root, 'apps');
    const backupApps = join(activeBackup, 'apps');
    preflightCheck('admin_apps', 'execute', () => assertWritableDirectory(appsRoot));
    await probeDirectoryCapabilities(appsRoot, 'admin_apps', operations, { requireLink: false });
    await probeUIBackupTransfer(backupApps, appsRoot, operations, {
      sourceScope: 'ui_backup',
      targetScope: 'admin_apps',
    });
    for (const move of migration.moves) {
      const source = join(activeBackup, move.backup);
      preflightCheck('ui_backup', 'cross_directory_rename', () => {
        if (operations.deviceOf(source) !== operations.deviceOf(appsRoot)) {
          throw new Error('PREFLIGHT_FAILED');
        }
      });
    }
  }

  if (pathPresent(location.receipt)) {
    const isolationParent = preflightCheck('state_root', 'create', () => assertBackupRootAvailable(location.legacyReceiptIsolationRoot));
    preflightExistingFile(location.receipt, 'admin_root', 'rename', operations);
    if (isolationParent !== root && isolationParent !== stateParent) {
      await probeDirectoryCapabilities(isolationParent, 'state_root', operations, { requireLink: false });
    }
    await probeUIBackupTransfer(root, isolationParent, operations, {
      sourceScope: 'admin_root',
      targetScope: 'state_root',
      failureScope: 'state_root',
    });
    preflightCheck('state_root', 'cross_directory_rename', () => {
      if (operations.deviceOf(location.receipt) !== operations.deviceOf(isolationParent)) {
        throw new Error('PREFLIGHT_FAILED');
      }
    });
  }
  return { required: true, transactionId: migration.transactionId };
}

function initializationPreflightOperations(options = {}) {
  return {
    access: options.access ?? accessSync,
    deviceOf: options.deviceOf ?? ((target) => lstatSync(target).dev),
    link: options.link ?? link,
    mkdir: options.mkdir ?? mkdir,
    open: options.open ?? open,
    remove: options.remove ?? rm,
    rename: options.rename ?? rename,
    syncDirectory: options.syncDirectory ?? syncDirectory,
    cleanup: options.cleanup ?? rm,
    platform: options.platform ?? process.platform,
  };
}

function preflightExistingFile(target, scope, operation, operations) {
  const state = strictPathState(target);
  if (state.kind === 'missing') return false;
  if (state.kind === 'error') throw new InitializationPreflightError(scope, 'read');
  if (!state.stat.isFile() || state.stat.isSymbolicLink()) {
    throw new InitializationPreflightError(scope, operation);
  }
  // On POSIX, replacing, deleting, or renaming a file is governed by the
  // containing directory; a read-only file can still be edited atomically or
  // removed when that directory is writable. Keep a read check for atomic
  // writes because the state validator has to parse the existing record. On
  // Windows the target's read-only attribute also blocks replacement,
  // deletion, and rename, so add W_OK for those operations.
  preflightCheck(scope, operation, () => {
    operations.access(dirname(target), fsConstants.W_OK | fsConstants.X_OK);
    let targetMode = operation === 'write' ? fsConstants.R_OK : 0;
    if (operations.platform === 'win32' && operation !== 'read') targetMode |= fsConstants.W_OK;
    if (targetMode !== 0) operations.access(target, targetMode);
  });
  return true;
}

function assertSelectedUIRuntimeInputs(root, profile) {
  const selectedDirectory = join(root, profile.appDirectory);
  for (const mode of RUNTIME_ENV_MODES) {
    const template = join(selectedDirectory, `.env.${mode}.example`);
    if (!plainFile(template)) throw new Error('PREFLIGHT_FAILED');
    accessSync(template, fsConstants.R_OK);
    const target = join(selectedDirectory, `.env.${mode}`);
    const targetState = strictPathState(target);
    if (targetState.kind === 'error') throw new Error('PREFLIGHT_FAILED');
    if (targetState.kind === 'present') {
      if (!targetState.stat.isFile() || targetState.stat.isSymbolicLink()) throw new Error('PREFLIGHT_FAILED');
      readFileSync(target);
    }
  }
}

// preflightInitialization performs real, reversible capability probes before
// any initialization lease, transaction, profile or UI source move is created.
// Native Node filesystem calls keep the same behavior on macOS, Windows and
// Linux and catch ACL, filesystem-feature and cross-volume failures that
// access(2) alone cannot prove. Actual template moves remain journaled because
// another process can still acquire a file lock after this reversible probe.
export async function preflightInitialization(root, selectedUi, options = {}) {
  const profile = profileFor(selectedUi);
  if (!profile) throw new Error('UI_INVALID');
  const allowPartialLayout = options.allowPartialLayout === true;
  if (!allowPartialLayout) assertTemplateLayout(root);
  const location = statePaths(root);
  const appsRoot = join(root, 'apps');
  const selectedDirectory = join(root, profile.appDirectory);
  const operations = initializationPreflightOperations(options);

  preflightCheck('admin_root', 'execute', () => assertWritableDirectory(root));
  preflightCheck('admin_apps', 'execute', () => assertWritableDirectory(appsRoot));
  for (const [ui, entry] of Object.entries(UI_PROFILES)) {
    const directory = join(root, entry.appDirectory);
    if (allowPartialLayout && ui !== selectedUi && !unselectedWorkspaceSurfacePresent(root, entry)) continue;
    preflightCheck(ui === selectedUi ? 'selected_ui' : 'admin_apps', 'read', () => {
      accessSync(directory, fsConstants.R_OK | fsConstants.X_OK);
      accessSync(join(directory, 'package.json'), fsConstants.R_OK);
    });
  }
  preflightCheck('selected_ui', 'execute', () => assertWritableDirectory(selectedDirectory));
  preflightCheck('selected_ui', 'read', () => assertSelectedUIRuntimeInputs(root, profile));
  const backupParent = preflightCheck('state_root', 'create', () => assertBackupRootAvailable(location.backupRoot));
  for (const [ui, entry] of Object.entries(UI_PROFILES)) {
    if (ui === selectedUi) continue;
    const source = join(root, entry.appDirectory);
    if (allowPartialLayout && !unselectedWorkspaceSurfacePresent(root, entry)) continue;
    preflightCheck('ui_backup', 'cross_directory_rename', () => {
      if (operations.deviceOf(source) !== operations.deviceOf(backupParent)) {
        throw new Error('PREFLIGHT_FAILED');
      }
    });
  }
  await probeDirectoryCapabilities(root, 'admin_root', operations, { requireLink: false });
  await probeDirectoryCapabilities(appsRoot, 'admin_apps', operations, { requireLink: false });
  await probeDirectoryCapabilities(selectedDirectory, 'selected_ui', operations);
  await probeDirectoryCapabilities(backupParent, 'state_root', operations);
  await probeUIBackupTransfer(appsRoot, backupParent, operations);

  return {
    profile,
    retain: profile.appDirectory,
    stage: Object.entries(UI_PROFILES).filter(([ui]) => ui !== selectedUi).map(([, entry]) => entry.appDirectory),
    backup: '.runtime/install/ui-backup/<transaction>',
  };
}

export async function preflightInitializationResume(root, options = {}) {
  const location = statePaths(root);
  const transaction = validTransaction(location.transaction);
  if (!transaction || transaction.owner !== 'admin-init') {
    throw new Error('INITIALIZATION_RESUME_INVALID');
  }
  const operations = initializationPreflightOperations(options);
  await recoverBackupPreflightArtifacts(root, operations);
  assertInitializationResume(root, transaction);
  const profile = profileFor(transaction.selectedUi);
  if (!profile) throw new Error('INITIALIZATION_RESUME_INVALID');

  const appsRoot = join(root, 'apps');
  const selectedDirectory = join(root, profile.appDirectory);
  const transactionDirectory = join(location.backupRoot, transaction.id);
  const backupApps = join(location.backupRoot, transaction.id, 'apps');
  const transactionParent = preflightCheck('ui_backup', 'create', () => assertBackupRootAvailable(transactionDirectory));
  const pendingMoves = transaction.phase === 'moving_ui'
    ? transaction.moves.filter((move) => existsSync(join(root, move.source)))
    : [];
  const backupParent = pendingMoves.length > 0
    ? preflightCheck('ui_backup', 'create', () => assertBackupRootAvailable(backupApps))
    : null;

  preflightCheck('admin_root', 'execute', () => assertWritableDirectory(root));
  if (pendingMoves.length > 0) {
    preflightCheck('admin_apps', 'execute', () => assertWritableDirectory(appsRoot));
  }
  preflightCheck('selected_ui', 'execute', () => assertWritableDirectory(selectedDirectory));
  preflightCheck('selected_ui', 'read', () => assertSelectedUIRuntimeInputs(root, profile));
  preflightCheck('state_root', 'execute', () => assertWritableDirectory(location.stateRoot));
  preflightExistingFile(location.profile, 'admin_root', 'write', operations);
  preflightExistingFile(location.transaction, 'state_root', 'write', operations);
  if (transaction.phase === 'dependencies_pending') {
    preflightExistingFile(join(transactionDirectory, 'receipt.json'), 'ui_backup', 'write', operations);
  }

  await probeDirectoryCapabilities(root, 'admin_root', operations, { requireLink: false });
  if (pendingMoves.length > 0) {
    await probeDirectoryCapabilities(appsRoot, 'admin_apps', operations, { requireLink: false });
  }
  await probeDirectoryCapabilities(selectedDirectory, 'selected_ui', operations);
  await probeDirectoryCapabilities(location.stateRoot, 'state_root', operations);
  if (transactionParent !== location.stateRoot) {
    await probeDirectoryCapabilities(transactionParent, 'ui_backup', operations, { requireLink: false });
  }
  if (backupParent && backupParent !== location.stateRoot && backupParent !== transactionParent) {
    await probeDirectoryCapabilities(backupParent, 'ui_backup', operations, { requireLink: false });
  }
  if (backupParent) await probeUIBackupTransfer(appsRoot, backupParent, operations);

  for (const move of pendingMoves) {
    const source = join(root, move.source);
    preflightCheck('ui_backup', 'cross_directory_rename', () => {
      if (operations.deviceOf(source) !== operations.deviceOf(backupParent)) {
        throw new Error('PREFLIGHT_FAILED');
      }
    });
  }
  return { profile, transactionId: transaction.id };
}

export async function preflightReset(root, options = {}) {
  const location = statePaths(root);
  const operations = initializationPreflightOperations(options);
  await recoverBackupPreflightArtifacts(root, operations);
  assertAppsRoot(root, 'RESET_LAYOUT_INVALID');
  const transactionPresent = strictPathPresent(location.transaction, 'INIT_BUSY');
  const pending = transactionPresent ? validTransaction(location.transaction) : null;
  if (transactionPresent && !pending) throw new Error('INIT_BUSY');
  if (pending?.owner === 'server-installer') throw new Error('INIT_BUSY');

  let transaction = pending;
  if (transaction?.owner === 'admin-init' && transaction.phase !== 'resetting_ui') {
    assertInitializationResume(root, transaction);
    transaction = { ...transaction, phase: 'resetting_ui' };
  }
  if (!transaction) {
    const current = inspectState(root);
    if (current.state !== STATES.UI_PREPARED || !current.profile) throw new Error('RESET_UNAVAILABLE');
    const receipt = backupReceipt(root, current.profile);
    if (!receipt) throw new Error('RESET_RECEIPT_UNAVAILABLE');
    transaction = {
      schema: 1,
      owner: 'admin-init',
      id: receipt.transactionId,
      selectedUi: receipt.selectedUi,
      phase: 'resetting_ui',
      moves: receipt.moves,
    };
  }
  const backup = resetBackupState(root, transaction);
  if (!backup) throw new Error('RESET_LAYOUT_INVALID');

  const appsRoot = join(root, 'apps');
  const backupDirectoryPresent = plainDirectory(backup.directory);
  preflightCheck('admin_root', 'execute', () => assertWritableDirectory(root));
  preflightCheck('state_root', 'execute', () => assertWritableDirectory(location.stateRoot));
  if (!backup.allRestored) {
    preflightCheck('admin_apps', 'execute', () => assertWritableDirectory(appsRoot));
  }
  if (backupDirectoryPresent) {
    preflightCheck('ui_backup', 'execute', () => assertWritableDirectory(location.backupRoot));
    preflightCheck('ui_backup', 'execute', () => assertWritableDirectory(backup.directory));
  }
  preflightExistingFile(location.profile, 'admin_root', 'delete', operations);
  preflightExistingFile(location.transaction, 'state_root', 'delete', operations);
  if (backupDirectoryPresent) {
    preflightExistingFile(backup.receiptFile, 'ui_backup', 'delete', operations);
    let backupEntries;
    try {
      backupEntries = readdirSync(backup.directory, { withFileTypes: true });
    } catch {
      throw new InitializationPreflightError('ui_backup', 'read');
    }
    for (const entry of backupEntries) {
      if (RECEIPT_TEMP_PATTERN.test(entry.name)) {
        preflightExistingFile(join(backup.directory, entry.name), 'ui_backup', 'delete', operations);
      }
    }
  }

  await probeDirectoryCapabilities(root, 'admin_root', operations, { requireLink: false });
  await probeDirectoryCapabilities(location.stateRoot, 'state_root', operations);
  if (!backup.allRestored) {
    await probeDirectoryCapabilities(appsRoot, 'admin_apps', operations, { requireLink: false });
  }
  if (backupDirectoryPresent) {
    await probeDirectoryCapabilities(location.backupRoot, 'ui_backup', operations, { requireLink: false });
    await probeDirectoryCapabilities(backup.directory, 'ui_backup', operations, { requireLink: false });
  }
  if (!backup.allRestored && plainDirectory(backup.appsDirectory)) {
    await probeUIBackupTransfer(backup.appsDirectory, appsRoot, operations);
    for (const move of transaction.moves) {
      const source = join(backup.directory, move.backup);
      if (!existsSync(source)) continue;
      preflightCheck('ui_backup', 'cross_directory_rename', () => {
        if (operations.deviceOf(source) !== operations.deviceOf(appsRoot)) {
          throw new Error('PREFLIGHT_FAILED');
        }
      });
    }
  }
  return { transactionId: transaction.id };
}

function assertInitializationResume(root, transaction) {
  const location = statePaths(root);
  assertAppsRoot(root, 'INITIALIZATION_RESUME_INVALID');
  if (strictDirectoryState(location.backupRoot) !== 'directory') {
    throw new Error('INITIALIZATION_RESUME_INVALID');
  }
  const expectedProfile = profileFor(transaction.selectedUi);
  if (!expectedProfile || !plainDirectory(join(root, expectedProfile.appDirectory))) {
    throw new Error('INITIALIZATION_RESUME_INVALID');
  }
  if (strictPathPresent(location.profile, 'INITIALIZATION_RESUME_INVALID')) {
    const currentProfile = parseProfile(location.profile);
    if (!currentProfile || currentProfile.selectedUi !== transaction.selectedUi) {
      throw new Error('INITIALIZATION_RESUME_INVALID');
    }
  }

  const transactionDirectory = join(location.backupRoot, transaction.id);
  const appsDirectory = join(transactionDirectory, 'apps');
  if (strictPathPresent(transactionDirectory, 'INITIALIZATION_RESUME_INVALID')) {
    if (!plainDirectory(transactionDirectory)) throw new Error('INITIALIZATION_RESUME_INVALID');
    const expectedNames = new Set(transaction.moves.map((move) => move.backup.slice('apps/'.length)));
    const entries = readdirSync(transactionDirectory, { withFileTypes: true });
    for (const entry of entries) {
      const validApps = entry.name === 'apps' && entry.isDirectory() && !entry.isSymbolicLink();
      const validReceipt = entry.name === 'receipt.json' && entry.isFile() && !entry.isSymbolicLink();
      const validReceiptTemp = RECEIPT_TEMP_PATTERN.test(entry.name) && entry.isFile() && !entry.isSymbolicLink();
      if (!validApps && !validReceipt && !validReceiptTemp) throw new Error('INITIALIZATION_RESUME_INVALID');
      if ((validReceipt || validReceiptTemp) && transaction.phase !== 'dependencies_pending') {
        throw new Error('INITIALIZATION_RESUME_INVALID');
      }
    }
    if (strictPathPresent(appsDirectory, 'INITIALIZATION_RESUME_INVALID')) {
      if (!plainDirectory(appsDirectory)) throw new Error('INITIALIZATION_RESUME_INVALID');
      const appEntries = readdirSync(appsDirectory, { withFileTypes: true });
      if (appEntries.some((entry) => !expectedNames.has(entry.name) || !entry.isDirectory() || entry.isSymbolicLink())) {
        throw new Error('INITIALIZATION_RESUME_INVALID');
      }
    }
    const receiptFile = join(transactionDirectory, 'receipt.json');
    if (strictPathPresent(receiptFile, 'INITIALIZATION_RESUME_INVALID')) {
      const receipt = parseJSON(receiptFile);
      if (
        !receipt
        || Object.keys(receipt).sort().join(',') !== 'dependenciesReady,moves,owner,schema,selectedUi,transactionId'
        || receipt.schema !== 1
        || receipt.owner !== 'admin-init'
        || receipt.transactionId !== transaction.id
        || receipt.selectedUi !== transaction.selectedUi
        || receipt.dependenciesReady !== true
        || JSON.stringify(receipt.moves) !== JSON.stringify(transaction.moves)
      ) throw new Error('INITIALIZATION_RESUME_INVALID');
    }
  }
  for (const move of transaction.moves) {
    const source = join(root, move.source);
    const backup = join(location.backupRoot, transaction.id, move.backup);
    const sourceExists = strictPathPresent(source, 'INITIALIZATION_RESUME_INVALID');
    const backupExists = strictPathPresent(backup, 'INITIALIZATION_RESUME_INVALID');
    if (sourceExists === backupExists || (sourceExists ? !plainDirectory(source) : !plainDirectory(backup))) {
      throw new Error('INITIALIZATION_RESUME_INVALID');
    }
    if (transaction.phase === 'dependencies_pending' && sourceExists) {
      throw new Error('INITIALIZATION_RESUME_INVALID');
    }
  }
}

export async function initialize(root, selectedUi) {
  const location = statePaths(root);
  const pending = existsSync(location.transaction) ? validTransaction(location.transaction) : null;
  if (pending?.owner === 'admin-init') selectedUi = pending.selectedUi;
  const profile = profileFor(selectedUi);
  if (!profile) throw new Error('UI_INVALID');
  const current = inspectState(root);
  if (!pending && current.state !== STATES.PRISTINE) return { ...current, repeated: true };
  if (pending) {
    assertInitializationResume(root, pending);
    await preflightInitializationResume(root);
  } else {
    await preflightInitialization(root, selectedUi);
  }
  assertAppsRoot(root, pending ? 'INITIALIZATION_RESUME_INVALID' : 'SOURCE_MOVE_STATE_INVALID');

  let transaction = pending;
  if (!transaction) {
    const id = randomUUID();
    const moves = Object.entries(UI_PROFILES)
      .filter(([ui]) => ui !== selectedUi)
      .map(([, entry]) => ({ source: entry.appDirectory, backup: entry.appDirectory }));
    transaction = { schema: 1, owner: 'admin-init', id, selectedUi, phase: 'moving_ui', moves };
    mkdirSync(location.stateRoot, { recursive: true, mode: 0o700 });
    try {
      await acquireTransaction(location.transaction, `${JSON.stringify(transaction, null, 2)}\n`);
    } catch (error) {
      if (error && typeof error === 'object' && error.code === 'EEXIST') return { ...inspectState(root), repeated: true };
      throw error;
    }
  }

  ensurePlainDirectory(location.backupRoot, 'SOURCE_MOVE_STATE_INVALID');
  const transactionDirectory = join(location.backupRoot, transaction.id);
  ensurePlainDirectory(transactionDirectory, 'SOURCE_MOVE_STATE_INVALID');
  ensurePlainDirectory(join(transactionDirectory, 'apps'), 'SOURCE_MOVE_STATE_INVALID');

  for (const move of transaction.moves) {
    assertAppsRoot(root, 'SOURCE_MOVE_STATE_INVALID');
    const target = join(location.backupRoot, transaction.id, move.backup);
    const source = join(root, move.source);
    const sourceExists = strictPathPresent(source, 'SOURCE_MOVE_STATE_INVALID');
    const targetExists = strictPathPresent(target, 'SOURCE_MOVE_STATE_INVALID');
    if (sourceExists && !targetExists && plainDirectory(source)) {
      await rename(source, target);
      await syncDirectory(dirname(source));
      await syncDirectory(dirname(target));
    } else if (sourceExists || !plainDirectory(target)) {
      throw new Error('SOURCE_MOVE_STATE_INVALID');
    }
  }
  await ensureSelectedUIRuntimeEnv(root, profile);
  await atomicWrite(location.profile, `${JSON.stringify(profile, null, 2)}\n`);
  transaction = { ...transaction, phase: 'dependencies_pending' };
  await atomicWrite(location.transaction, `${JSON.stringify(transaction, null, 2)}\n`);
  return { state: STATES.UI_PREPARED, profile, repeated: Boolean(pending), transactionId: transaction.id };
}

async function cleanupTransactionReceiptTemps(directory) {
  let entries;
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    throw new Error('DEPENDENCY_TRANSACTION_INVALID');
  }
  const temporaryEntries = entries.filter((entry) => RECEIPT_TEMP_PATTERN.test(entry.name));
  if (temporaryEntries.some((entry) => !entry.isFile() || entry.isSymbolicLink())) {
    throw new Error('DEPENDENCY_TRANSACTION_INVALID');
  }
  for (const entry of temporaryEntries) await rm(join(directory, entry.name));
  if (temporaryEntries.length > 0) await syncDirectory(directory);
}

export async function completeDependencyPreparation(root) {
  const location = statePaths(root);
  assertAppsRoot(root, 'DEPENDENCY_TRANSACTION_INVALID');
  if (strictDirectoryState(location.backupRoot) !== 'directory') {
    throw new Error('DEPENDENCY_TRANSACTION_INVALID');
  }
  const transaction = validTransaction(location.transaction);
  if (!transaction || transaction.owner !== 'admin-init' || transaction.phase !== 'dependencies_pending') {
    throw new Error('DEPENDENCY_TRANSACTION_INVALID');
  }
  const receipt = {
    schema: 1,
    owner: 'admin-init',
    transactionId: transaction.id,
    selectedUi: transaction.selectedUi,
    dependenciesReady: true,
    moves: transaction.moves,
  };
  const transactionDirectory = join(location.backupRoot, transaction.id);
  if (!plainDirectory(transactionDirectory)) throw new Error('DEPENDENCY_TRANSACTION_INVALID');
  await cleanupTransactionReceiptTemps(transactionDirectory);
  await atomicWrite(join(transactionDirectory, 'receipt.json'), `${JSON.stringify(receipt, null, 2)}\n`);
  await cleanupTransactionReceiptTemps(transactionDirectory);
  const current = validTransaction(location.transaction);
  if (!current || current.owner !== 'admin-init' || current.id !== transaction.id) throw new Error('DEPENDENCY_TRANSACTION_INVALID');
  await rm(location.transaction);
  await syncDirectory(dirname(location.transaction));
  return inspectState(root);
}

function resetBackupState(root, transaction) {
  const location = statePaths(root);
  if (strictDirectoryState(join(root, 'apps')) !== 'directory') return null;
  if (strictDirectoryState(location.backupRoot) !== 'directory') return null;
  const directory = join(location.backupRoot, transaction.id);
  const appsDirectory = join(directory, 'apps');
  const receiptFile = join(directory, 'receipt.json');
  const allRestored = transaction.moves.every((move) => plainDirectory(join(root, move.source)));

  const directoryState = strictPathState(directory);
  if (directoryState.kind === 'error') return null;
  if (directoryState.kind === 'missing') {
    return allRestored ? { allRestored, appsDirectory, directory, receiptFile } : null;
  }
  if (!plainDirectory(directory)) return null;

  let topLevel;
  try {
    topLevel = readdirSync(directory, { withFileTypes: true });
  } catch {
    return null;
  }
  for (const entry of topLevel) {
    const validApps = entry.name === 'apps' && entry.isDirectory() && !entry.isSymbolicLink();
    const validReceipt = entry.name === 'receipt.json' && entry.isFile() && !entry.isSymbolicLink();
    const validReceiptTemp = transaction.phase === 'resetting_ui'
      && RECEIPT_TEMP_PATTERN.test(entry.name)
      && entry.isFile()
      && !entry.isSymbolicLink();
    if (!validApps && !validReceipt && !validReceiptTemp) return null;
  }

  const receiptState = strictPathState(receiptFile);
  if (receiptState.kind === 'error') return null;
  const hasReceipt = receiptState.kind === 'present';
  if (hasReceipt) {
    const receipt = parseJSON(receiptFile);
    if (
      !receipt
      || Object.keys(receipt).sort().join(',') !== 'dependenciesReady,moves,owner,schema,selectedUi,transactionId'
      || receipt.schema !== 1
      || receipt.owner !== 'admin-init'
      || receipt.transactionId !== transaction.id
      || receipt.selectedUi !== transaction.selectedUi
      || receipt.dependenciesReady !== true
      || !movesEqual(receipt.moves, transaction.moves)
    ) return null;
  } else if (!allRestored && transaction.phase !== 'resetting_ui') {
    return null;
  }

  const appsState = strictPathState(appsDirectory);
  if (appsState.kind === 'error') return null;
  if (appsState.kind === 'present') {
    if (!plainDirectory(appsDirectory)) return null;
    let entries;
    try {
      entries = readdirSync(appsDirectory, { withFileTypes: true });
    } catch {
      return null;
    }
    const expectedNames = new Set(transaction.moves.map((move) => move.backup.slice('apps/'.length)));
    if (entries.some((entry) => !expectedNames.has(entry.name) || !entry.isDirectory() || entry.isSymbolicLink())) return null;
  } else if (!allRestored) {
    return null;
  }

  for (const move of transaction.moves) {
    const staged = join(directory, move.backup);
    const restored = join(root, move.source);
    const stagedState = strictPathState(staged);
    const restoredState = strictPathState(restored);
    if (stagedState.kind === 'error' || restoredState.kind === 'error') return null;
    const stagedExists = stagedState.kind === 'present';
    const restoredExists = restoredState.kind === 'present';
    if (stagedExists === restoredExists) return null;
    if (stagedExists ? !plainDirectory(staged) : !plainDirectory(restored)) return null;
  }
  return { allRestored, appsDirectory, directory, receiptFile };
}

async function cleanupResetBackup(backup) {
  if (!existsSync(backup.directory)) return;
  if (existsSync(backup.receiptFile)) {
    await rm(backup.receiptFile);
    await syncDirectory(backup.directory);
  }
  const entries = readdirSync(backup.directory, { withFileTypes: true });
  const receiptTemps = entries.filter((entry) => RECEIPT_TEMP_PATTERN.test(entry.name));
  if (receiptTemps.some((entry) => !entry.isFile() || entry.isSymbolicLink())) {
    throw new Error('RESET_LAYOUT_INVALID');
  }
  for (const entry of receiptTemps) await rm(join(backup.directory, entry.name));
  if (receiptTemps.length > 0) await syncDirectory(backup.directory);
  if (existsSync(backup.appsDirectory)) {
    if (!plainDirectory(backup.appsDirectory) || readdirSync(backup.appsDirectory).length !== 0) {
      throw new Error('RESET_LAYOUT_INVALID');
    }
    await rmdir(backup.appsDirectory);
    await syncDirectory(backup.directory);
  }
  if (!plainDirectory(backup.directory) || readdirSync(backup.directory).length !== 0) {
    throw new Error('RESET_LAYOUT_INVALID');
  }
  await rmdir(backup.directory);
  await syncDirectory(dirname(backup.directory));
}

export async function reset(root, options = {}) {
  const location = statePaths(root);
  assertAppsRoot(root, 'RESET_LAYOUT_INVALID');
  const transactionPresent = strictPathPresent(location.transaction, 'INIT_BUSY');
  const pending = transactionPresent ? validTransaction(location.transaction) : null;
  if (transactionPresent && !pending) throw new Error('INIT_BUSY');
  if (pending?.owner === 'server-installer') throw new Error('INIT_BUSY');

  let transaction = pending;
  let createdClaim;
  if (transaction?.owner === 'admin-init' && transaction.phase !== 'resetting_ui') {
    if (strictPathPresent(location.marker, 'INIT_BUSY')) throw new Error('RESET_UNAVAILABLE_INSTALLED');
    if (strictPathPresent(location.lock, 'INIT_BUSY')) throw new Error('INIT_BUSY');
    assertInitializationResume(root, transaction);
    const resetting = { ...transaction, phase: 'resetting_ui' };
    if (!resetBackupState(root, resetting)) throw new Error('RESET_LAYOUT_INVALID');
    await options.beforeTransition?.();
    const current = validTransaction(location.transaction);
    if (
      !current
      || current.owner !== 'admin-init'
      || current.id !== transaction.id
      || current.phase !== transaction.phase
      || current.selectedUi !== transaction.selectedUi
      || !movesEqual(current.moves, transaction.moves)
    ) throw new Error('INIT_BUSY');
    await atomicWrite(location.transaction, `${JSON.stringify(resetting, null, 2)}\n`);
    transaction = resetting;
    await options.afterTransition?.();
  }
  if (!transaction) {
    const current = inspectState(root);
    if (current.state === STATES.INSTALLED) throw new Error('RESET_UNAVAILABLE_INSTALLED');
    if (current.state !== STATES.UI_PREPARED || !current.profile) throw new Error('RESET_UNAVAILABLE');
    const receipt = backupReceipt(root, current.profile);
    if (!receipt) throw new Error('RESET_RECEIPT_UNAVAILABLE');
    transaction = {
      schema: 1,
      owner: 'admin-init',
      id: receipt.transactionId,
      selectedUi: receipt.selectedUi,
      phase: 'resetting_ui',
      moves: receipt.moves,
    };
    const encoded = `${JSON.stringify(transaction, null, 2)}\n`;
    await options.beforeClaim?.();
    try {
      await acquireTransaction(location.transaction, encoded);
    } catch (error) {
      if (error?.code === 'EEXIST') throw new Error('INIT_BUSY');
      throw error;
    }
    createdClaim = readLeaseSnapshot(location.transaction);
    if (!createdClaim || createdClaim.contents !== encoded) throw new Error('RESET_TRANSACTION_INVALID');
    await options.afterClaim?.();
  }

  const rejectBeforeMove = async (code) => {
    if (createdClaim) await removeLeaseSnapshot(location.transaction, createdClaim);
    throw new Error(code);
  };
  if (strictPathPresent(location.marker, 'INIT_BUSY')) await rejectBeforeMove('RESET_UNAVAILABLE_INSTALLED');
  if (strictPathPresent(location.lock, 'INIT_BUSY')) await rejectBeforeMove('INIT_BUSY');
  const profilePresent = strictPathPresent(location.profile, 'RESET_LAYOUT_INVALID');
  const profile = profilePresent ? parseProfile(location.profile) : null;
  if (profilePresent && profile?.selectedUi !== transaction.selectedUi) await rejectBeforeMove('RESET_LAYOUT_INVALID');
  if (!plainDirectory(join(root, UI_PROFILES[transaction.selectedUi].appDirectory))) await rejectBeforeMove('RESET_LAYOUT_INVALID');
  let backup = resetBackupState(root, transaction);
  if (!backup) await rejectBeforeMove('RESET_LAYOUT_INVALID');

  for (const move of transaction.moves) {
    assertAppsRoot(root, 'RESET_LAYOUT_INVALID');
    const from = join(backup.directory, move.backup);
    const to = join(root, move.source);
    const fromPresent = strictPathPresent(from, 'RESET_LAYOUT_INVALID');
    const toPresent = strictPathPresent(to, 'RESET_LAYOUT_INVALID');
    if (fromPresent && plainDirectory(from) && !toPresent) {
      await rename(from, to);
      await syncDirectory(dirname(from));
      await syncDirectory(dirname(to));
    } else if (fromPresent || !plainDirectory(to)) {
      throw new Error('RESET_LAYOUT_INVALID');
    }
  }

  backup = resetBackupState(root, transaction);
  if (!backup?.allRestored) throw new Error('RESET_LAYOUT_INVALID');
  await cleanupResetBackup(backup);
  await rm(location.profile, { force: true });
  await syncDirectory(dirname(location.profile));

  const owned = validTransaction(location.transaction);
  if (!owned || owned.owner !== 'admin-init' || owned.phase !== 'resetting_ui' || owned.id !== transaction.id) {
    throw new Error('RESET_TRANSACTION_INVALID');
  }
  await rm(location.transaction);
  await syncDirectory(dirname(location.transaction));
  return { state: STATES.PRISTINE, profile: null };
}

export function actionForState({ state, reason = STATE_REASONS.NONE }) {
  if ([STATE_REASONS.RECEIPT_WITHOUT_PROFILE, STATE_REASONS.RUNTIME_WITHOUT_PROFILE].includes(reason)) {
    return 'RUN_INIT_AUTO_RECOVERY';
  }
  if (state === STATES.PRISTINE) return 'START_INITIALIZATION';
  if (state === STATES.UI_PREPARED) return 'OPEN_INSTALLER';
  if (state === STATES.INSTALLING) return 'WAIT_FOR_INITIALIZATION';
  if (state === STATES.INSTALLED) return 'RUN_SELECTED_APP';
  if (reason === STATE_REASONS.EXTRA_TEMPLATE_PRESENT) return 'REMOVE_UNSELECTED_UI_WORKSPACE';
  return 'KEEP_STATE_AND_REPORT_CODE';
}

export function formatStatus({ state, profile, reason = STATE_REASONS.NONE, action, next, error = 'NONE', port = 8080 }) {
  return [
    `INIT_STATE=${state}`,
    `INIT_SELECTED_UI=${profile?.selectedUi ?? 'none'}`,
    `INIT_REASON=${reason}`,
    `INIT_ACTION=${action ?? actionForState({ state, reason })}`,
    `INIT_URL=${installURL(port)}`,
    `INIT_NEXT=${next}`,
    `INIT_ERROR=${error}`,
  ].join('\n');
}

export function rootFromScript(scriptURL) {
  return resolve(dirname(fileURLToPath(scriptURL)), '..');
}
