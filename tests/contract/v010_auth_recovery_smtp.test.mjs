import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const read = (path) => readFileSync(new URL(`../../${path}`, import.meta.url), 'utf8');

test('0.10 password recovery uses profile email for optional SMTP delivery', () => {
  const provider = read('server/internal/application/auth/password_reset_provider.go');
  const smtpProvider = read('server/internal/application/auth/smtp_password_reset_provider.go');
  const recovery = read('server/internal/application/auth/account_recovery.go');
  const bootstrap = read('server/internal/bootstrap/app.go');

  assert.match(provider, /type PasswordResetRecipientProvider interface/);
  assert.match(provider, /RequestTo\(context\.Context, string, string\) error/);
  assert.match(provider, /func NewDevelopmentPasswordResetProvider/);
  assert.match(smtpProvider, /func NewSMTPPasswordResetProvider/);
  assert.match(smtpProvider, /notification\.SendInput/);
  assert.match(recovery, /PasswordResetRecipientProvider/);
  assert.match(recovery, /user\.Email/);
  assert.match(recovery, /RequestTo\(/);
  assert.match(bootstrap, /cfg\.Mail\.Enabled/);
  assert.match(bootstrap, /NewSMTPMailer/);
  assert.match(bootstrap, /NewSMTPPasswordResetProvider/);
  assert.doesNotMatch(smtpProvider, /log\.(Print|Printf|Println)/);
  assert.doesNotMatch(smtpProvider, /fmt\.Print/);
});
