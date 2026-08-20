#!/usr/bin/env node
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
console.log(`DEV_ROOT=${root}`);
console.log('DEV_COMMANDS=go -C server run ./cmd/api | pnpm --dir admin run dev:antd');
console.log('DEV_NOTE=Use node ./scripts/verify.mjs before starting watchers.');
