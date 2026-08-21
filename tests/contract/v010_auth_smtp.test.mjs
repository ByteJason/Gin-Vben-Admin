import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('0.10 SMTP recovery seam is explicit, redacted, and Mailpit-isolated', () => {
  const config = read('server/internal/config/config.go');
  const example = read('server/configs/server.example.yaml');
  const compose = read('deploy/compose.mailpit.yaml');
  const adapter = read('server/internal/platform/notification/smtp.go');

  assert.match(config, /type MailConfig struct/);
  for (const key of ['MAIL_ENABLED', 'MAIL_HOST', 'MAIL_PORT', 'MAIL_FROM', 'MAIL_START_TLS']) {
    assert.match(config, new RegExp(key));
  }
  assert.match(config, /Password.*json:\"-\"|MailSummary/);
  assert.match(example, /mail:\s*\n[\s\S]*enabled:\s*false/);
  assert.match(compose, /mailpit:/);
  assert.match(compose, /1025/);
  assert.match(compose, /8025/);
  assert.match(compose, /profiles:/);
  assert.match(adapter, /func NewSMTPMailer/);
  assert.match(adapter, /StartTLS/);
  assert.match(adapter, /context\.Context/);
  assert.doesNotMatch(adapter, /log\.Printf|fmt\.Print/);
});
