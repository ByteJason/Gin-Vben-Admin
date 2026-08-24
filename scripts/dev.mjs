#!/usr/bin/env node
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { spawn } from 'node:child_process';

import { buildPnpmCommand } from '../admin/scripts/pnpm-command.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const checkOnly = process.argv.includes('--check');
const help = process.argv.includes('--help') || process.argv.includes('-h');

if (help) {
  console.log('Usage: node ./scripts/dev.mjs [--check]');
  process.exit(0);
}

const invalid = process.argv.slice(2).find((argument) => argument !== '--check');
if (invalid) {
  console.error(`unsupported argument: ${invalid}`);
  process.exit(2);
}

const adminCommand = buildPnpmCommand(['--dir', 'admin', 'run', 'dev']);
const commands = [
  { label: 'server', command: 'go', args: ['-C', 'server', 'run', './cmd/api/main.go'] },
  { label: 'admin', ...adminCommand },
];

console.log(`DEV_ROOT=${root}`);
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
