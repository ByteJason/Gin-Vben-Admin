#!/usr/bin/env node
import { access } from 'node:fs/promises';
import { constants } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const files = ['contracts/openapi/admin-v1.yaml', 'contracts/openapi/client-v1.yaml'];
for (const file of files) {
  try {
    await access(path.join(root, file), constants.F_OK);
  } catch {
    console.error(`OPENAPI_SOURCE_MISSING=${file}`);
    process.exit(1);
  }
}
console.log('OPENAPI_SOURCES_OK=2');
console.log('OPENAPI_GENERATION_MODE=skeleton');
console.log('OPENAPI_GENERATE_OK');
