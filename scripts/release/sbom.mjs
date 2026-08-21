#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const args = process.argv.slice(2);
const value = (flag) => {
  const i = args.indexOf(flag);
  return i >= 0 ? args[i + 1] : undefined;
};
const output = value('--output');
if (!output) {
  console.error('usage: sbom.mjs --output <path> [--check]');
  process.exit(2);
}

const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');
const read = (relative) => readFileSync(join(ROOT, relative));
const license = (id = 'MIT') => [{ license: { id } }];

function goComponents() {
  const seen = new Set();
  return read('server/go.sum').toString('utf8').split(/\r?\n/).flatMap((line) => {
    const match = line.match(/^(\S+)\s+(\S+)\s+h1:/);
    if (!match || match[2].endsWith('/go.mod')) return [];
    const key = `${match[1]}@${match[2]}`;
    if (seen.has(key)) return [];
    seen.add(key);
    return [{ type: 'library', name: match[1], version: match[2], purl: `pkg:golang/${match[1]}@${match[2]}`, licenses: license('NOASSERTION'), properties: [{ name: 'source', value: 'server/go.sum' }, { name: 'licenseEvidence', value: 'lockfile-does-not-declare-spdx' }] }];
  });
}

function pnpmComponents() {
  const text = read('admin/pnpm-lock.yaml').toString('utf8');
  const start = text.indexOf('\npackages:\n');
  const body = start >= 0 ? text.slice(start + '\npackages:\n'.length) : '';
  return body.split(/\r?\n/).flatMap((line) => {
    const match = line.match(/^  (?:'([^']+)'|([^\s:]+)):\s*$/);
    const key = match?.[1] ?? match?.[2];
    if (!key || key.startsWith('.')) return [];
    const at = key.lastIndexOf('@');
    if (at <= 0) return [];
    const name = key.slice(0, at);
    const version = key.slice(at + 1).split('(')[0];
    if (!version || version.startsWith('link:')) return [];
    return [{ type: 'library', name, version, purl: `pkg:npm/${name}@${version}`, licenses: license('NOASSERTION'), properties: [{ name: 'source', value: 'admin/pnpm-lock.yaml' }, { name: 'licenseEvidence', value: 'lockfile-does-not-declare-spdx' }] }];
  });
}

function sourceComponent(name, relative, id = 'MIT') {
  const bytes = read(relative);
  return { type: 'file', name, version: '1.0.0', hashes: [{ alg: 'SHA-256', content: sha256(bytes) }], licenses: license(id), properties: [{ name: 'source', value: relative }] };
}

function build() {
  const components = [
    sourceComponent('Gin-Vben-Admin', 'LICENSE'),
    sourceComponent('NOTICE', 'NOTICE'),
    sourceComponent('Vue-Vben-Admin', 'LICENSES/Vue-Vben-Admin-MIT.txt'),
    sourceComponent('server/go.sum', 'server/go.sum'),
    sourceComponent('admin/pnpm-lock.yaml', 'admin/pnpm-lock.yaml'),
    ...goComponents(),
    ...pnpmComponents(),
  ].sort((a, b) => {
    const left = `${a.type}:${a.name}:${a.version}`;
    const right = `${b.type}:${b.name}:${b.version}`;
    return left < right ? -1 : left > right ? 1 : 0;
  });
  return {
    bomFormat: 'CycloneDX',
    specVersion: '1.5',
    serialNumber: 'urn:uuid:00000000-0000-4000-8000-000000000009',
    version: 1,
    metadata: { timestamp: '1970-01-01T00:00:00.000Z', tools: [{ vendor: 'Gin-Vben-Admin', name: 'offline-sbom', version: '0.9.0-rc' }], component: { type: 'application', name: 'Gin-Vben-Admin', version: '0.9.0-rc', licenses: license() } },
    components,
  };
}

const document = `${JSON.stringify(build(), null, 2)}\n`;
if (args.includes('--check')) {
  const existing = readFileSync(resolve(output), 'utf8');
  if (existing !== document) {
    console.error(`SBOM_CHECK_FAILED: ${output}`);
    process.exit(1);
  }
  console.log(`SBOM_CHECK_OK: ${output}`);
} else {
  const path = resolve(output);
  writeFileSync(path, document);
  console.log(`SBOM_GENERATED: ${path}`);
}
