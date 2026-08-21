import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const ROOT = fileURLToPath(new URL('../..', import.meta.url));
const SBOM = join(ROOT, 'scripts/release/sbom.mjs');
const POLICY = join(ROOT, 'scripts/release/license-policy.mjs');

test('sbom generation is deterministic and includes offline source manifests', () => {
  const dir = mkdtempSync(join(tmpdir(), 'gva-sbom-'));
  const one = join(dir, 'path with spaces', 'one.json');
  const two = join(dir, 'two.json');
  mkdirSync(join(dir, 'path with spaces'), { recursive: true });
  execFileSync(process.execPath, [SBOM, '--output', one], { cwd: ROOT, stdio: 'pipe' });
  execFileSync(process.execPath, [SBOM, '--output', two], { cwd: ROOT, stdio: 'pipe' });
  assert.equal(readFileSync(one, 'utf8'), readFileSync(two, 'utf8'));
  const doc = JSON.parse(readFileSync(one, 'utf8'));
  assert.equal(doc.bomFormat, 'CycloneDX');
  assert.equal(doc.specVersion, '1.5');
  assert.ok(doc.components.some((item) => item.name === 'Gin-Vben-Admin'));
  assert.ok(doc.components.some((item) => item.name === 'server/go.sum'));
  assert.ok(doc.components.some((item) => item.name === 'admin/pnpm-lock.yaml'));
  execFileSync(process.execPath, [SBOM, '--output', one, '--check'], { cwd: ROOT, stdio: 'pipe' });
});

test('license policy accepts the project SBOM and rejects unknown licenses', () => {
  const dir = mkdtempSync(join(tmpdir(), 'gva-license-'));
  const sbom = join(dir, 'sbom.json');
  const notice = join(dir, 'NOTICE');
  writeFileSync(notice, readFileSync(join(ROOT, 'NOTICE')));
  execFileSync(process.execPath, [SBOM, '--output', sbom], { cwd: ROOT, stdio: 'pipe' });
  execFileSync(process.execPath, [POLICY, '--sbom', sbom, '--notice', join(ROOT, 'NOTICE')], {
    cwd: ROOT,
    stdio: 'pipe',
  });
  const summary = execFileSync(process.execPath, [POLICY, '--sbom', sbom, '--notice', join(ROOT, 'NOTICE')], { cwd: ROOT, encoding: 'utf8' });
  assert.match(summary, /registeredExceptions/);
  const bad = JSON.parse(readFileSync(sbom, 'utf8'));
  bad.components.push({ name: 'fixture-unknown', version: '1.0.0', licenses: [{ license: { id: 'UNKNOWN' } }] });
  writeFileSync(join(dir, 'bad.json'), `${JSON.stringify(bad, null, 2)}\n`);
  const result = spawnSync(process.execPath, [POLICY, '--sbom', join(dir, 'bad.json'), '--notice', notice], {
    cwd: ROOT,
    encoding: 'utf8',
  });
  assert.notEqual(result.status, 0);
  assert.match(`${result.stdout}${result.stderr}`, /unknown|UNKNOWN/i);
});
