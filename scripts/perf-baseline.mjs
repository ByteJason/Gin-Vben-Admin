#!/usr/bin/env node

/**
 * B9.4 performance/capacity baseline runner.
 *
 * The default mode is a deterministic, offline contract report.  It records
 * the DEC-025 experiment shape without pretending that a local fixture is a
 * production capacity result.  A real HTTP observation is deliberately
 * opt-in: PERF_INTEGRATION=1 and --integration are both required, and the
 * target must be loopback-only.
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const DEFAULT_OUTPUT = join(ROOT, '.runtime', 'perf', 'performance-baseline-v0.9.0-rc.json');
const VERSION = '0.9.0-rc';
const FIXED_TIMESTAMP = '1970-01-01T00:00:00.000Z';

const DEC025 = Object.freeze({
  environment: Object.freeze({ cpu: 4, memory_gib: 8, isolation: 'dedicated' }),
  fixtures: Object.freeze({ users: 100_000, roles: 1_000, audit_events: 1_000_000 }),
  workloads: Object.freeze({
    read_api: Object.freeze({ concurrency: 100 }),
    login: Object.freeze({ concurrency: 20 }),
  }),
  duration_minutes: Object.freeze({ warmup: 10, steady: 30 }),
});

function fail(message, status = 2) {
  process.stderr.write(`PERF_BASELINE_ERROR=${message}\n`);
  process.exit(status);
}

function parseArgs(argv) {
  const options = { check: false, integration: false, output: DEFAULT_OUTPUT, baseUrl: '' };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === '--check') options.check = true;
    else if (arg === '--integration') options.integration = true;
    else if (arg === '--output') {
      options.output = argv[++index];
      if (!options.output) fail('missing value for --output');
    } else if (arg === '--base-url') {
      options.baseUrl = argv[++index];
      if (!options.baseUrl) fail('missing value for --base-url');
    } else if (arg === '--help' || arg === '-h') {
      process.stdout.write('usage: perf-baseline.mjs [--output <path>] [--check] [--integration --base-url <loopback-url>]\n');
      process.exit(0);
    } else {
      fail(`unknown argument: ${arg}`);
    }
  }
  return options;
}

function offlineReport() {
  return {
    schema: 1,
    report: 'performance-baseline',
    version: VERSION,
    scope: 'local-only',
    generated_at: FIXED_TIMESTAMP,
    environment: { ...DEC025.environment },
    fixtures: { ...DEC025.fixtures },
    workloads: {
      read_api: { ...DEC025.workloads.read_api },
      login: { ...DEC025.workloads.login },
    },
    duration_minutes: { ...DEC025.duration_minutes },
    acceptance: {
      status: 'not_evaluated',
      production_capacity_claim: false,
      reason: 'offline contract only; run an isolated integration experiment before interpreting measurements',
    },
    integration: {
      enabled: false,
      transport: 'loopback-http',
      status: 'not_run',
      observations: [],
    },
  };
}

function document(report) {
  return `${JSON.stringify(report, null, 2)}\n`;
}

function ensureOutputPath(path) {
  const absolute = resolve(ROOT, path);
  mkdirSync(dirname(absolute), { recursive: true });
  return absolute;
}

function assertLoopback(raw) {
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error('base URL must be a valid http loopback URL');
  }
  if (parsed.protocol !== 'http:') throw new Error('base URL must use http for isolated smoke');
  if (!['127.0.0.1', 'localhost', '::1', '[::1]'].includes(parsed.hostname)) {
    throw new Error('base URL must target loopback; remote targets are not permitted');
  }
  if (parsed.username || parsed.password) throw new Error('base URL must not contain credentials');
  return parsed;
}

async function probe(url, path) {
  const target = new URL(path, url);
  process.stdout.write(`PERF_REQUEST_SENT=${target.pathname}\n`);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5_000);
  const started = performance.now();
  try {
    const response = await fetch(target, { signal: controller.signal });
    return {
      path: target.pathname,
      status: response.status,
      ok: response.ok,
      elapsed_ms: Math.round((performance.now() - started) * 100) / 100,
    };
  } finally {
    clearTimeout(timer);
  }
}

async function runIntegration(report, options) {
  if (process.env.PERF_INTEGRATION !== '1') {
    throw new Error('integration requires PERF_INTEGRATION=1');
  }
  if (!options.baseUrl) throw new Error('--base-url is required in integration mode');
  const baseUrl = assertLoopback(options.baseUrl);
  report.integration = {
    enabled: true,
    transport: 'loopback-http',
    status: 'observing',
    base_url: `${baseUrl.protocol}//${baseUrl.host}`,
    observations: [],
  };
  for (const path of ['/health/live', '/health/ready']) {
    try {
      const observation = await probe(baseUrl, path);
      report.integration.observations.push(observation);
      if (!observation.ok) throw new Error(`${path} returned HTTP ${observation.status}`);
    } catch (error) {
      report.integration.status = 'error';
      report.integration.error = error.name === 'AbortError' ? 'request timeout' : error.message;
      return false;
    }
  }
  report.integration.status = 'observed';
  return true;
}

function check(options) {
  const path = resolve(ROOT, options.output);
  if (!existsSync(path)) fail(`PERF_BASELINE_CHECK_FAILED: missing ${path}`, 1);
  const actual = readFileSync(path, 'utf8');
  const expected = document(offlineReport());
  if (actual !== expected) fail(`PERF_BASELINE_CHECK_FAILED: drift detected in ${path}`, 1);
  process.stdout.write(`PERF_BASELINE_CHECK_OK=${path}\n`);
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  process.stdout.write(`PERF_BASELINE_VERSION=${VERSION}\n`);
  process.stdout.write(`PERF_BASELINE_SCOPE=local-only\n`);
  if (options.check) {
    check(options);
    return;
  }

  const report = offlineReport();
  if (options.integration) {
    process.stdout.write('PERF_BASELINE_MODE=integration\n');
    let observed = false;
    try {
      observed = await runIntegration(report, options);
    } catch (error) {
      report.integration = {
        enabled: true,
        transport: 'loopback-http',
        status: 'error',
        observations: [],
        error: error.message,
      };
      process.stderr.write(`PERF_INTEGRATION_ERROR=${error.message}\n`);
    }
    const path = ensureOutputPath(options.output);
    writeFileSync(path, document(report), { mode: 0o600 });
    process.stdout.write(`PERF_PRODUCTION_CAPACITY_CLAIM=${report.acceptance.production_capacity_claim}\n`);
    process.stdout.write(`PERF_INTEGRATION_STATUS=${report.integration.status.toUpperCase()}\n`);
    if (!observed) process.exitCode = 1;
    return;
  }

  process.stdout.write('PERF_BASELINE_MODE=offline-contract\n');
  const path = ensureOutputPath(options.output);
  writeFileSync(path, document(report), { mode: 0o600 });
  process.stdout.write(`PERF_BASELINE_STATUS=NOT_EVALUATED\n`);
  process.stdout.write('PERF_PRODUCTION_CAPACITY_CLAIM=false\n');
  process.stdout.write(`PERF_BASELINE_WRITTEN=${path}\n`);
}

await main();
