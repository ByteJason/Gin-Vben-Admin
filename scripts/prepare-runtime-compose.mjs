#!/usr/bin/env node

/**
 * Generate disposable development/acceptance compose files outside deploy/.
 * Production deploy intentionally stays single-node; this command creates
 * opt-in fixtures for the dual-database, HA and observability checks.
 */
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const out = join(root, '.runtime', 'compose');

const fixtures = {
  'README.md': `# Runtime compose fixtures\n\nThese files are disposable local acceptance assets. They are deliberately outside deploy/ and never start automatically.\n\n- dev.yaml: single MySQL/Redis plus Mailpit seam\n- postgres.yaml: PostgreSQL single-node\n- read-write.yaml: MySQL primary/replica seam\n- ha.yaml: Redis Sentinel/cluster and dual-database seam\n- observability.yaml: Prometheus/Grafana/OTel profile and local alert webhook\n\nUse only synthetic local values from the surrounding environment. Do not commit runtime credentials.\n`,
  'dev.yaml': `name: gin-vben-runtime-dev\nservices:\n  mysql:\n    image: mysql:8.4\n    environment:\n      MYSQL_DATABASE: gin_vben_admin\n      MYSQL_USER: app\n      MYSQL_PASSWORD: local_fixture_password\n      MYSQL_ROOT_PASSWORD: local_fixture_root_password\n    healthcheck:\n      test: ["CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -u root -p$$MYSQL_ROOT_PASSWORD --silent"]\n  redis:\n    image: redis:7-alpine\n    command: ["redis-server", "--appendonly", "yes"]\n    healthcheck:\n      test: ["CMD", "redis-cli", "ping"]\n  mailpit:\n    image: axllent/mailpit:latest\n`,
  'postgres.yaml': `name: gin-vben-runtime-postgres\nservices:\n  postgres:\n    image: postgres:16-alpine\n    environment:\n      POSTGRES_DB: gin_vben_admin\n      POSTGRES_USER: app\n      POSTGRES_PASSWORD: local_fixture_password\n    healthcheck:\n      test: ["CMD-SHELL", "pg_isready -U app -d gin_vben_admin"]\n`,
  'read-write.yaml': `name: gin-vben-runtime-read-write\nservices:\n  mysql-primary:\n    image: mysql:8.4\n    environment: &mysql-env\n      MYSQL_DATABASE: gin_vben_admin\n      MYSQL_USER: app\n      MYSQL_PASSWORD: local_fixture_password\n      MYSQL_ROOT_PASSWORD: local_fixture_root_password\n  mysql-replica:\n    image: mysql:8.4\n    environment: *mysql-env\n    depends_on: [mysql-primary]\n`,
  'ha.yaml': `name: gin-vben-runtime-ha\nservices:\n  redis-primary:\n    image: redis:7-alpine\n  redis-sentinel:\n    image: redis:7-alpine\n    command: ["redis-server", "--sentinel"]\n    depends_on: [redis-primary]\n  postgres-primary:\n    image: postgres:16-alpine\n    environment:\n      POSTGRES_DB: gin_vben_admin\n      POSTGRES_USER: app\n      POSTGRES_PASSWORD: local_fixture_password\n`,
  'observability.yaml': `name: gin-vben-runtime-observability\nservices:\n  prometheus:\n    image: prom/prometheus:v2.53.1\n    profiles: ["observability"]\n    command: ["--config.file=/etc/prometheus/prometheus.yml", "--web.enable-lifecycle"]\n    volumes:\n      - ./observability/prometheus.yml:/etc/prometheus/prometheus.yml:ro\n      - ./observability/rules.yml:/etc/prometheus/rules.yml:ro\n  otel-collector:\n    image: otel/opentelemetry-collector-contrib:0.108.0\n    profiles: ["observability"]\n    command: ["--config=/etc/otelcol-contrib/config.yaml"]\n    volumes:\n      - ./observability/otel-collector-config.yaml:/etc/otelcol-contrib/config.yaml:ro\n  alertmanager:\n    image: prom/alertmanager:v0.27.0\n    profiles: ["observability"]\n    command: ["--config.file=/etc/alertmanager/alertmanager.yml"]\n    volumes:\n      - ./observability/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro\n  webhook:\n    image: python:3.12-alpine\n    profiles: ["observability"]\n    command: ["python", "-m", "http.server", "8080"]\n  grafana:\n    image: grafana/grafana:11.1.0\n    profiles: ["observability"]\n    environment:\n      GF_AUTH_ANONYMOUS_ENABLED: "true"\n      GF_AUTH_ANONYMOUS_ORG_ROLE: Viewer\n    volumes:\n      - ./observability/dashboard.json:/var/lib/grafana/dashboards/gin-vben-admin.json:ro\n`,
  'observability/prometheus.yml': `global:\n  scrape_interval: 15s\nrule_files:\n  - /etc/prometheus/rules.yml\nscrape_configs:\n  - job_name: gin-vben-admin\n    static_configs:\n      - targets: ["host.docker.internal:8080"]\n`,
  'observability/rules.yml': `groups:\n  - name: gin-vben-admin\n    rules:\n      - alert: readiness\n        expr: up == 0\n      - alert: http_5xx\n        expr: rate(http_requests_total{status=~"5.."}[5m]) > 0\n      - alert: request_p95\n        expr: histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 1\n      - alert: otlp_drops\n        expr: rate(otelcol_exporter_send_failed_spans[5m]) > 0\n      - alert: dependency\n        expr: probe_success == 0\n`,
  'observability/otel-collector-config.yaml': `receivers:\n  otlp:\n    protocols:\n      grpc:\n      http:\nexporters:\n  debug:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [debug]\n`,
  'observability/alertmanager.yml': `route:\n  receiver: local-webhook\nreceivers:\n  - name: local-webhook\n    webhook_configs:\n      - url: http://webhook:8080/alerts\n`,
  'observability/dashboard.json': `{ "title": "Gin-Vben-Admin Observability", "schemaVersion": 39, "panels": [] }\n`,
};

await mkdir(out, { recursive: true });
for (const [name, contents] of Object.entries(fixtures)) {
  const path = join(out, name);
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, contents, { mode: 0o600 });
  console.log(`RUNTIME_COMPOSE_GENERATED=${path}`);
}
