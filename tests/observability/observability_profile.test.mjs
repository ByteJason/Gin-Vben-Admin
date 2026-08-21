import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const compose = join(root, 'deploy', 'compose.observability.yaml');
const prometheus = join(root, 'deploy', 'observability', 'prometheus', 'prometheus.yml');
const rules = join(root, 'deploy', 'observability', 'prometheus', 'rules.yml');
const otel = join(root, 'deploy', 'observability', 'otel-collector-config.yaml');
const dashboard = join(root, 'deploy', 'observability', 'grafana', 'dashboards', 'gin-vben-admin.json');
const alertmanager = join(root, 'deploy', 'observability', 'alertmanager.yml');

test('optional observability profile declares external scrape, OTLP, dashboard and webhook paths', () => {
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
  const result = spawnSync('docker', ['compose', '-f', 'deploy/compose.dev.yaml', '-f', 'deploy/compose.observability.yaml', 'config', '--quiet'], {
    cwd: root,
    encoding: 'utf8',
  });
  if (result.error?.code === 'ENOENT') return;
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const defaultText = readFileSync(join(root, 'deploy', 'compose.dev.yaml'), 'utf8');
  assert.doesNotMatch(defaultText, /prometheus|otel-collector|grafana|alertmanager/);
});

test('dashboard and alert delivery are local fixtures with no remote destination', () => {
  const dashboardJSON = JSON.parse(readFileSync(dashboard, 'utf8'));
  assert.equal(dashboardJSON.title, 'Gin-Vben-Admin Observability');
  const alertText = readFileSync(alertmanager, 'utf8');
  assert.match(alertText, /webhook:8080/);
  assert.doesNotMatch(alertText, /https?:\/\/(?!webhook)/i);
});
