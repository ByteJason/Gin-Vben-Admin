import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const generator = join(root, 'scripts', 'prepare-runtime-compose.mjs');
const compose = join(root, '.runtime', 'compose', 'observability.yaml');
const prometheus = join(root, '.runtime', 'compose', 'observability', 'prometheus.yml');
const rules = join(root, '.runtime', 'compose', 'observability', 'rules.yml');
const otel = join(root, '.runtime', 'compose', 'observability', 'otel-collector-config.yaml');
const dashboard = join(root, '.runtime', 'compose', 'observability', 'dashboard.json');
const alertmanager = join(root, '.runtime', 'compose', 'observability', 'alertmanager.yml');

function generate() {
  const result = spawnSync(process.execPath, [generator], { cwd: root, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stdout + result.stderr);
}

test('optional observability profile declares external scrape, OTLP, dashboard and webhook paths', () => {
  generate();
  for (const path of [compose, prometheus, rules, otel, dashboard, alertmanager]) assert.equal(existsSync(path), true, path);
  const composeText = readFileSync(compose, 'utf8');
  assert.match(composeText, /profiles:\s*\["observability"\]/g);
  assert.match(composeText, /prometheus/);
  assert.match(composeText, /otel-collector/);
  assert.match(composeText, /grafana/);
  assert.match(composeText, /webhook/);
  for (const path of [prometheus, rules, otel]) assert.doesNotMatch(readFileSync(path, 'utf8'), /password\s*:/i);
  const ruleText = readFileSync(rules, 'utf8');
  for (const id of ['readiness', '5xx', 'p95', 'otlp', 'dependency']) assert.match(ruleText, new RegExp(id, 'i'));
});

test('compose profile parses without becoming part of the default topology', () => {
  generate();
  const result = spawnSync('docker', ['compose', '-f', '.runtime/compose/observability.yaml', 'config', '--quiet'], {
    cwd: root,
    encoding: 'utf8',
  });
  if (result.error?.code === 'ENOENT') return;
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const defaultText = readFileSync(join(root, 'deploy', 'docker-compose.yml'), 'utf8');
  assert.doesNotMatch(defaultText, /prometheus|otel-collector|grafana|alertmanager/);
});

test('dashboard and alert delivery are local fixtures with no remote destination', () => {
  generate();
  const dashboardJSON = JSON.parse(readFileSync(dashboard, 'utf8'));
  assert.equal(dashboardJSON.title, 'Gin-Vben-Admin Observability');
  const alertText = readFileSync(alertmanager, 'utf8');
  assert.match(alertText, /webhook:8080/);
  assert.doesNotMatch(alertText, /https?:\/\/(?!webhook)/i);
});
