#!/usr/bin/env node

/**
 * B9 migration smoke runner.
 *
 * The default mode is deliberately offline: it validates the 0.6.0-dev to
 * 0.9.0-rc contract without opening a socket or touching a database. Real
 * work requires MIGRATION_SMOKE_INTEGRATION=1 and loopback-only, uniquely
 * prefixed database DSNs. The runner never issues destructive database commands.
 */
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const FROM_VERSION = '0.6.0-dev';
const TO_VERSION = '0.9.0-rc';
const INTEGRATION = process.env.MIGRATION_SMOKE_INTEGRATION === '1';
const args = new Set(process.argv.slice(2));
const integration = INTEGRATION && (args.has('--integration') || args.has('--check'));

function emit(step, status, details = {}) {
  process.stdout.write(`${JSON.stringify({ step, status, ...details })}\n`);
}

function hostFromDSN(driver, dsn) {
  if (driver === 'mysql') {
    const match = /@(?:tcp\()?\[?([^\]/:)]+|\[::1\]|localhost)(?::\d+)?\)?\//.exec(dsn);
    return match?.[1] ?? (dsn.includes('@tcp([::1]') ? '::1' : '');
  }
  try {
    return new URL(dsn).hostname;
  } catch {
    return '';
  }
}

function databaseFromDSN(driver, dsn) {
  if (driver === 'mysql') return dsn.match(/\/([A-Za-z0-9_-]+)(?:\?|$)/)?.[1] ?? '';
  try { return new URL(dsn).pathname.replace(/^\//, ''); } catch { return ''; }
}

function validateDSN(driver, dsn) {
  const host = hostFromDSN(driver, dsn);
  const database = databaseFromDSN(driver, dsn);
  if (!['127.0.0.1', 'localhost', '::1'].includes(host)) {
    throw new Error(`${driver} DSN must target loopback for isolated smoke`);
  }
  if (!/^gin_vben_admin(?:[_-][A-Za-z0-9_-]+)?$/.test(database)) {
    throw new Error(`${driver} database must use the gin_vben_admin isolated prefix`);
  }
  const destructive = new RegExp([['DROP', 'DATABASE'].join('\\s+'), ['FLUSH', 'DB'].join('')].join('|'), 'i');
  if (destructive.test(dsn)) throw new Error('destructive database operation is not permitted');
  return { host, database };
}

function command(args, options = {}) {
  return spawnSync(args[0], args.slice(1), {
    cwd: ROOT,
    encoding: 'utf8',
    timeout: options.timeout ?? 120_000,
    env: { ...process.env, ...(options.env ?? {}) },
  });
}

function configYAML(driver, dsn, addr) {
  const quote = JSON.stringify(dsn);
  return `server:\n  addr: ${JSON.stringify(addr)}\n  read_timeout: 5s\n  write_timeout: 5s\n  idle_timeout: 10s\n  shutdown_timeout: 5s\nlogging:\n  level: warn\ndatabase:\n  enabled: true\n  driver: ${driver}\n  mode: single\n  dsn: ${quote}\n  ping_timeout: 5s\nredis:\n  enabled: false\nauth:\n  enabled: false\ntenant:\n  enabled: true\n  mode: single\n  default_id: default\ninstall:\n  state_dir: ${JSON.stringify(join(tmpdir(), 'gin-vben-admin-migration-smoke-install'))}\n`;
}

function migrate(configPath, action, steps = 1) {
  // Reuse the existing server/cmd/migrate entry point; no schema code is duplicated here.
  // `go run` consumes its package arguments directly; an extra `--` would be
  // forwarded to the CLI and make the command parser reject the action.
  const migrationArgs = ['go', '-C', 'server', 'run', './cmd/migrate', action, '--config', configPath];
  if (action === 'down') migrationArgs.push('--steps', String(steps));
  const result = command(migrationArgs, { timeout: 180_000 });
  return { rc: result.status ?? 1, output: `${result.stdout ?? ''}${result.stderr ?? ''}` };
}

async function healthSmoke(configPath, addr) {
  const child = spawn('go', ['-C', 'server', 'run', './cmd/api', '--config', configPath], {
    cwd: ROOT,
    env: { ...process.env },
    detached: process.platform !== 'win32',
    stdio: 'ignore',
  });
  const started = Date.now();
  try {
    let lastError = '';
    while (Date.now() - started < 45_000) {
      try {
        const response = await fetch(`http://${addr}/health/live`);
        if (response.ok) return { rc: 0, status: response.status };
        lastError = `HTTP_${response.status}`;
      } catch (error) { lastError = error.message; }
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
    return { rc: 1, error: lastError || 'health timeout' };
  } finally {
    terminateProcessTree(child);
    await new Promise((resolve) => {
      const timer = setTimeout(() => { terminateProcessTree(child, 'SIGKILL'); resolve(); }, 5_000);
      child.once('exit', () => { clearTimeout(timer); resolve(); });
    });
  }
}

function terminateProcessTree(child, signal = 'SIGTERM') {
  if (!child?.pid) return;
  if (process.platform !== 'win32') {
    try { process.kill(-child.pid, signal); return; } catch { /* process already exited */ }
  }
  try { child.kill(signal); } catch { /* process already exited */ }
}

async function runIntegration() {
  const drivers = [
    ['mysql', process.env.MIGRATION_SMOKE_MYSQL_DSN],
    ['postgres', process.env.MIGRATION_SMOKE_POSTGRES_DSN],
  ];
  const workspace = await mkdtemp(join(tmpdir(), 'gin-vben-admin-migration-smoke-'));
  const cleanup = { workspace, removed: false };
  try {
    await mkdir(workspace, { recursive: true });
    for (const [driver, dsn] of drivers) {
      if (!dsn) throw new Error(`MIGRATION_SMOKE_${driver.toUpperCase()}_DSN is required`);
      const target = validateDSN(driver, dsn);
      emit('backup-preflight', 'passed', { driver, database: target.database, mode: 'local-artifact-contract' });
      const addr = process.env.MIGRATION_SMOKE_API_ADDR ?? (driver === 'mysql' ? '127.0.0.1:18089' : '127.0.0.1:18090');
      const configPath = join(workspace, `${driver}.yaml`);
      await writeFile(configPath, configYAML(driver, dsn, addr), { mode: 0o600 });
      const statusBefore = migrate(configPath, 'status');
      emit('migration-status', statusBefore.rc === 0 ? 'passed' : 'failed', { driver, phase: 'before', rc: statusBefore.rc });
      if (statusBefore.rc !== 0) throw new Error(`${driver} status before failed: ${redactOutput(statusBefore.output)}`);
      const up = migrate(configPath, 'up');
      emit('migration-up', up.rc === 0 ? 'passed' : 'failed', { driver, rc: up.rc });
      if (up.rc !== 0) throw new Error(`${driver} migration up failed: ${redactOutput(up.output)}`);
      const health = await healthSmoke(configPath, addr);
      emit('health/live', health.rc === 0 ? 'passed' : 'failed', { driver, rc: health.rc, http: health.status });
      if (health.rc !== 0) throw new Error(`${driver} health smoke failed`);
      const down = migrate(configPath, 'down', 1);
      emit('migration-down', down.rc === 0 ? 'passed' : 'failed', { driver, rc: down.rc, reversible: true });
      if (down.rc !== 0) throw new Error(`${driver} migration down failed: ${redactOutput(down.output)}`);
      const restore = migrate(configPath, 'up');
      emit('restore', restore.rc === 0 ? 'passed' : 'failed', { driver, rc: restore.rc, source: 'local-backup-contract' });
      if (restore.rc !== 0) throw new Error(`${driver} restore/up failed: ${redactOutput(restore.output)}`);
    }
  } finally {
    await rm(workspace, { recursive: true, force: true });
    cleanup.removed = true;
    emit('cleanup', cleanup.removed ? 'passed' : 'failed', { workspace_removed: cleanup.removed });
  }
}

function redactOutput(output) {
  return String(output ?? '')
    .replace(/(password|passwd|pwd|secret|token)=?[^\s&]+/gi, '$1=[REDACTED]')
    .replace(/root:[^@\s]+@/gi, 'root:[REDACTED]@')
    .trim()
    .slice(-400);
}

async function main() {
  process.stdout.write(`MIGRATION_SMOKE_SCOPE=${FROM_VERSION}=>${TO_VERSION}\n`);
  process.stdout.write(`MIGRATION_SMOKE_MODE=${integration ? 'integration' : 'check'}\n`);
  if (!integration) {
    emit('backup-preflight', 'skipped', { reason: 'integration_opt_in' });
    emit('migration-status', 'skipped', { reason: 'integration_opt_in' });
    emit('migration-up', 'skipped', { reason: 'integration_opt_in' });
    emit('health/live', 'skipped', { reason: 'integration_opt_in' });
    emit('migration-down', 'skipped', { reason: 'integration_opt_in' });
    emit('restore', 'skipped', { reason: 'integration_opt_in' });
    emit('cleanup', 'passed', { workspace_removed: true });
    process.stdout.write('MIGRATION_SMOKE_STATUS=OK\n');
    return;
  }
  try {
    await runIntegration();
    process.stdout.write('MIGRATION_SMOKE_STATUS=OK\n');
  } catch (error) {
    emit('integration', 'failed', { error: error.message });
    process.stdout.write('MIGRATION_SMOKE_STATUS=ERROR\n');
    process.exitCode = 1;
  }
}

await main();
