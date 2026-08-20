#!/usr/bin/env node
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { spawn } from 'node:child_process';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const allowedUi = new Set(['antd', 'ele', 'naive']);
const value = (name, fallback) => {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
};
const ui = value('--ui', 'antd');
const checkOnly = process.argv.includes('--check');
const help = process.argv.includes('--help') || process.argv.includes('-h');

if (help) {
  console.log('Usage: node ./scripts/dev.mjs [--ui antd|ele|naive] [--check]');
  process.exit(0);
}

if (!allowedUi.has(ui)) {
  console.error(`unsupported --ui: ${ui}`);
  process.exit(2);
}

const pnpm = process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm';
const commands = [
  { label: 'server', command: 'go', args: ['-C', 'server', 'run', './cmd/api'] },
  { label: 'admin', command: pnpm, args: ['--dir', 'admin', 'run', `dev:${ui}`] },
];

console.log(`DEV_ROOT=${root}`);
console.log(`DEV_UI=${ui}`);
console.log(`DEV_COMMANDS=${commands.map(({ command, args }) => [command, ...args].join(' ')).join(' | ')}`);

if (checkOnly) {
  console.log('DEV_CHECK_OK');
  process.exit(0);
}

const children = [];
let stopping = false;

function stopAll(exitCode = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of children) {
    if (!child.killed) child.kill();
  }
  setTimeout(() => process.exit(exitCode), 250);
}

for (const spec of commands) {
  const child = spawn(spec.command, spec.args, {
    cwd: root,
    env: process.env,
    shell: false,
    stdio: 'inherit',
  });
  children.push(child);
  child.on('error', (error) => {
    console.error(`DEV_${spec.label.toUpperCase()}_ERROR=${error.message}`);
    stopAll(1);
  });
  child.on('exit', (code, signal) => {
    if (stopping) return;
    const status = code ?? `signal:${signal}`;
    console.error(`DEV_${spec.label.toUpperCase()}_EXIT=${status}`);
    stopAll(code ?? 1);
  });
}

process.on('SIGINT', () => stopAll(130));
process.on('SIGTERM', () => stopAll(143));
