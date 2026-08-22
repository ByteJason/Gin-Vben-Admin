import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

test('1.0 SMTP accounts and delivery records expose portable schema', () => {
  const mysql = read('server/migrations/mysql/000014_mail.up.sql');
  const postgres = read('server/migrations/postgres/000014_mail.up.sql');
  for (const source of [mysql, postgres]) {
    for (const token of [
      'smtp_accounts',
      'email_messages',
      'email_recipients',
      'deleted_at',
      'implicit_tls',
      'weight',
      'body_ciphertext',
      'provider_message_id',
    ]) {
      assert.match(source, new RegExp(token, 'i'), token);
    }
  }
  assert.match(mysql, /uq_smtp_accounts_tenant_name/i);
  assert.match(postgres, /uq_smtp_accounts_tenant_name/i);
});

test('1.0 mail and monitor HTTP contracts are declared and redacted', () => {
  const openapi = read('contracts/openapi/admin-v1.yaml');
  const client = read('admin/packages/api-client/src/generated/admin-v1.ts');
  for (const token of [
    '/mail/accounts',
    '/mail/accounts/{id}/test',
    '/mail/messages',
    '/mail/messages/{id}',
    '/ops/monitor',
    'SMTPAccount',
    'EmailMessage',
    'MonitorOverview',
  ]) {
    assert.match(`${openapi}\n${client}`, new RegExp(token.replace(/[{}]/g, '\\$&'), 'i'), token);
  }
  assert.doesNotMatch(openapi, /password_ciphertext.*type: string.*writeOnly: false/i);
});

for (const app of apps) {
  test(`1.0 ${app} exposes mail and monitor management pages`, () => {
    const mailApi = `admin/apps/${app}/src/api/core/mail.ts`;
    const mailView = `admin/apps/${app}/src/views/system/mail/index.vue`;
    const monitorApi = `admin/apps/${app}/src/api/core/monitor.ts`;
    const monitorView = `admin/apps/${app}/src/views/system/monitor/index.vue`;
    const routes = `admin/apps/${app}/src/router/routes/modules/system.ts`;
    for (const path of [mailApi, mailView, monitorApi, monitorView, routes]) {
      assert.equal(existsSync(new URL(path, root)), true, path);
    }
    for (const token of ['listSMTPAccountsApi', 'saveSMTPAccountApi', 'testSMTPAccountApi', 'deleteSMTPAccountApi']) {
      assert.match(read(mailApi), new RegExp(token), `${app}/${token}`);
    }
    for (const token of ['load', 'save', 'test', 'weight', 'implicit', 'TLS', 'password']) {
      assert.match(read(mailView), new RegExp(token, 'i'), `${app}/${token}`);
    }
    for (const token of ['getMonitorOverviewApi', 'refresh']) {
      assert.match(read(monitorApi), new RegExp(token), `${app}/${token}`);
    }
    for (const token of ['CPU', 'memory', 'disk', 'MySQL', 'PostgreSQL', 'Redis', 'degraded', '15']) {
      assert.match(read(monitorView), new RegExp(token, 'i'), `${app}/${token}`);
    }
    assert.match(read(routes), /views\/system\/mail\/index\.vue/);
    assert.match(read(routes), /views\/system\/monitor\/index\.vue/);
  });
}

test('1.0 deploy keeps single-machine compose and local Alpine Dockerfiles', () => {
  const compose = read('deploy/docker-compose.yml');
  const serverDockerfile = read('deploy/server.Dockerfile');
  const adminDockerfile = read('deploy/admin.Dockerfile');
  for (const token of ['mysql', 'redis', 'server', 'admin', 'healthcheck']) {
    assert.match(compose, new RegExp(token, 'i'), token);
  }
  assert.match(serverDockerfile, /alpine/i);
  assert.match(adminDockerfile, /alpine/i);
  assert.doesNotMatch(compose, /sentinel|cluster|replica/i);
});
