import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

test('0.10 captcha risk configuration and HTTP seam stay explicit and disabled by default', () => {
  const config = readFileSync(join(root, 'server/internal/config/config.go'), 'utf8');
  const example = readFileSync(join(root, 'server/configs/server.example.yaml'), 'utf8');
  const handler = readFileSync(join(root, 'server/internal/transport/http/auth/handler.go'), 'utf8');
  const risk = readFileSync(join(root, 'server/internal/application/auth/captcha_risk.go'), 'utf8');

  assert.match(config, /CaptchaEnabled\s+bool/);
  assert.match(config, /CaptchaRiskThreshold\s+int/);
  assert.match(config, /CaptchaRiskWindow\s+time\.Duration/);
  assert.match(config, /CaptchaKeyPrefix\s+string/);
  assert.match(config, /CaptchaEnabled:\s+false/);
  assert.match(example, /captcha_enabled:\s+false/);
  assert.match(example, /captcha_risk_threshold:\s+3/);
  assert.match(example, /captcha_risk_window:\s+15m/);
  assert.match(example, /captcha_key_prefix:\s+auth-captcha/);
  assert.match(handler, /SetCaptchaRiskStore/);
  assert.match(handler, /captchaRequired/);
  assert.match(risk, /type CaptchaRiskStore interface/);
  assert.match(risk, /RecordFailure/);
  assert.match(risk, /Reset/);
});
