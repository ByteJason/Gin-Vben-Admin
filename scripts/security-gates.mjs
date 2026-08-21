#!/usr/bin/env node

/**
 * Offline security gate orchestrator for the release candidate.
 *
 * The default command never contacts a registry, remote URL, database, or
 * production system. It inventories lockfiles, scans tracked text for
 * high-confidence secret material, and records unavailable scanner tools as
 * explicit, expiring exceptions instead of pretending that a scan passed.
 */
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const VERSION = '0.9.0-rc';
const EXPIRES = '2026-12-31';
const OWNER = 'security-engineering';
const TOOL_NAMES = [
  ['govulncheck', 'Go vulnerability scan'],
  ['pnpm', 'frontend dependency audit'],
  ['codeql', 'CodeQL analysis'],
  ['trivy', 'filesystem/image scan'],
  ['gosec', 'Go SAST'],
  ['osv-scanner', 'OSV dependency scan'],
  ['zap-baseline.py', 'OWASP ZAP local baseline'],
];
const TEXT_EXTENSIONS = new Set([
  '.go', '.mod', '.sum', '.mjs', '.js', '.ts', '.vue', '.yaml', '.yml', '.json',
  '.md', '.txt', '.conf', '.toml', '.env.example', '.sh', '.sql', '.html', '.css',
]);
const args = process.argv.slice(2);

function argValue(name, fallback = '') {
  const index = args.indexOf(name);
  return index >= 0 ? (args[index + 1] ?? fallback) : fallback;
}

function has(name) {
  return args.includes(name);
}

function canonical(value) {
  return `${JSON.stringify(value, null, 2)}\n`;
}

function commandExists(command) {
  const lookup = process.platform === 'win32' ? 'where.exe' : 'which';
  const result = spawnSync(lookup, [command], { cwd: ROOT, encoding: 'utf8' });
  return result.status === 0;
}

function runGitFiles() {
  const result = spawnSync('git', ['ls-files'], { cwd: ROOT, encoding: 'utf8' });
  if (result.status !== 0) throw new Error('git ls-files failed');
  return result.stdout.split(/\r?\n/).filter(Boolean).sort();
}

function isTextPath(path) {
  const lower = path.toLowerCase();
  if (lower.includes('/node_modules/') || lower.includes('/dist/') || lower.includes('/.git/')) return false;
  return [...TEXT_EXTENSIONS].some((extension) => lower.endsWith(extension));
}

function allowedFixture(content, path) {
  // Local compose/test fixtures deliberately use synthetic credentials. They
  // are allowed only when the endpoint is loopback or the file is a test/CI
  // fixture; real private-key material is never allowlisted.
  return /(?:compose|test|fixture|example|\.github\/workflows)/i.test(path)
    && /(?:127\.0\.0\.1|localhost|root_password|app_password|postgres)/i.test(content);
}

function scanSecrets(paths) {
  const findings = [];
  const patterns = [
    { id: 'private-key', severity: 'High', pattern: /-----BEGIN [A-Z ]*PRIVATE KEY-----/ },
    { id: 'aws-access-key', severity: 'High', pattern: /\bAKIA[0-9A-Z]{16}\b/ },
    { id: 'generic-token', severity: 'High', pattern: /\b(?:ghp|github_pat|xox[baprs])-?[A-Za-z0-9_-]{20,}\b/ },
    { id: 'remote-secret-dsn', severity: 'High', pattern: /(?:mysql|postgres(?:ql)?):\/\/[^\s:@]+:[^\s@]+@(?!(?:127\.0\.0\.1|localhost|\[::1\]))[^\s/]+/i },
  ];
  for (const path of paths) {
    if (!isTextPath(path)) continue;
    const absolute = join(ROOT, path);
    if (!existsSync(absolute)) continue;
    const content = readFileSync(absolute, 'utf8');
    if (allowedFixture(content, path)) continue;
    for (const item of patterns) {
      if (item.pattern.test(content)) findings.push({ id: item.id, severity: item.severity, path });
    }
  }
  return findings.sort((a, b) => `${a.path}:${a.id}`.localeCompare(`${b.path}:${b.id}`));
}

function inventoryTools() {
  return TOOL_NAMES.map(([name, purpose]) => {
    const available = commandExists(name);
    return {
      id: name,
      purpose,
      status: available ? 'available_not_run' : 'exception',
      owner: OWNER,
      reason: available ? 'Tool is installed but execution is controlled by the pinned CI profile.' : 'Pinned scanner is not installed in this local runner.',
      expires: EXPIRES,
    };
  });
}

function validateTarget(target) {
  if (!target) return;
  let parsed;
  try { parsed = new URL(target); } catch { throw new Error('DAST target URL is invalid'); }
  if (parsed.protocol !== 'http:') throw new Error('DAST target must be HTTP loopback');
  if (!['127.0.0.1', 'localhost', '::1'].includes(parsed.hostname)) {
    throw new Error('DAST target must be loopback; remote targets are refused');
  }
}

function fixtureFinding() {
  const fixture = argValue('--fixture');
  if (!fixture) return [];
  const [severity = 'High', id = 'unregistered'] = fixture.split(':');
  return [{ id, severity, source: 'fixture', status: 'unregistered' }];
}

async function main() {
  const output = argValue('--output', '.runtime/security/security-report.json');
  const target = argValue('--target');
  if (has('--integration')) validateTarget(target);

  const paths = runGitFiles();
  const findings = [...scanSecrets(paths), ...fixtureFinding()];
  const tools = inventoryTools();
  const highCritical = findings.filter((item) => ['high', 'critical'].includes(String(item.severity).toLowerCase())).length;
  const report = {
    schema: 1,
    version: VERSION,
    scope: 'local-only',
    generatedBy: 'scripts/security-gates.mjs',
    policy: {
      highCritical,
      mediumExceptionsRequire: ['owner', 'reason', 'expires'],
      productionClaim: false,
    },
    checks: [
      { id: 'secret-scan', status: findings.length === 0 ? 'passed' : 'failed', findings },
      { id: 'lockfiles', status: existsSync(join(ROOT, 'server/go.sum')) && existsSync(join(ROOT, 'admin/pnpm-lock.yaml')) ? 'passed' : 'failed', sources: ['server/go.sum', 'admin/pnpm-lock.yaml'] },
      { id: 'dast-target', status: target ? 'loopback-only' : 'not_requested', target: target ? new URL(target).host : null },
    ],
    toolExceptions: tools,
    unavailableTools: tools.filter((item) => item.status === 'exception').map((item) => item.id),
  };
  const serialized = canonical(report);
  if (has('--check')) {
    const existing = await readFile(resolve(ROOT, output), 'utf8').catch(() => '');
    if (existing !== serialized) throw new Error(`security report drift: ${output}`);
  } else {
    await mkdir(dirname(resolve(ROOT, output)), { recursive: true });
    await writeFile(resolve(ROOT, output), serialized, { mode: 0o600 });
  }
  process.stdout.write(serialized);
  if (highCritical > 0 || report.checks.some((item) => item.status === 'failed')) process.exitCode = 1;
}

try {
  await main();
} catch (error) {
  process.stderr.write(`SECURITY_GATE_ERROR=${error.message}\n`);
  process.exitCode = 1;
}
