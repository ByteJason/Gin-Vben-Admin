import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const profilePath = join(root, 'admin/.ui-profile.json');
const managementApps = existsSync(profilePath)
  ? [`web-${JSON.parse(readFileSync(profilePath, 'utf8')).selectedUi}`]
  : ['web-antd', 'web-ele', 'web-naive'];

function runNode(script, ...args) {
  return spawnSync(process.execPath, [join(root, script), ...args], {
    cwd: root,
    encoding: 'utf8',
  });
}

test('repository exposes the required code boundaries', () => {
  for (const path of [
    ...managementApps.map((app) => `admin/apps/${app}`),
    'server/cmd/api',
    'server/internal/bootstrap',
    'deploy/server.Dockerfile',
    'deploy/admin.Dockerfile',
    'deploy/docker-compose.yml',
    'scripts/prepare-runtime-compose.mjs',
    'docs/README.md',
    'contracts/openapi/admin-v1.yaml',
    'contracts/openapi/client-v1.yaml',
    'contracts/openapi/install-v1.yaml',
    'admin/apps/install/src/index.html',
    'admin/apps/install/src/app.js',
    'admin/apps/install/src/styles.css',
    'admin/apps/install/package.json',
    'admin/pnpm-lock.yaml',
    '.env.example',
    'server/configs/server.example.yaml',
    'LICENSE',
    'LICENSES/Vue-Vben-Admin-MIT.txt',
    'NOTICE',
  ]) {
    assert.equal(existsSync(join(root, path)), true, path);
  }
  const allowed = new Set([
    '.dev-docs', '.git', '.github', '.idea', '.pnpm-store', '.runtime', 'LICENSES', 'admin', 'contracts', 'deploy', 'docs',
    'scripts', 'server', 'storage', 'tests',
  ]);
  const unexpected = readdirSync(root, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !allowed.has(entry.name))
    .map((entry) => entry.name);
  assert.deepEqual(unexpected, []);
});

test('runtime configuration examples are compact and credential-free', () => {
  const envExample = readFileSync(join(root, '.env.example'), 'utf8');
  const serverExample = readFileSync(
    join(root, 'server/configs/server.example.yaml'),
    'utf8',
  );

  for (const key of [
    'APP_UI_ACTIVE',
    'SERVER_ADDR',
    'LOGGING_LEVEL',
    'DATABASE_ENABLED',
    'REDIS_ENABLED',
    'AUTH_ENABLED',
    'I18N_DEFAULT_LOCALE',
  ]) {
    assert.match(envExample, new RegExp(`^${key}=`, 'm'), key);
  }
  assert.doesNotMatch(envExample, /^(?:DATABASE_DSN|REDIS_PASSWORD|AUTH_JWT_SECRET)=\S+/m);
  assert.match(serverExample, /^version:\s*0\.2\.0-dev$/m);
  assert.match(serverExample, /^database:\s*$/m);
  assert.match(serverExample, /^redis:\s*$/m);
  assert.match(serverExample, /^auth:\s*$/m);
  assert.match(serverExample, /^  namespace:\s*app:v1$/m);
  assert.doesNotMatch(serverExample, /(?:password|dsn|jwt_secret):\s*["'][^"']+["']/i);
});

test('OpenAPI scopes stay separate and expose the HTTP seams', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const client = readFileSync(join(root, 'contracts/openapi/client-v1.yaml'), 'utf8');
  assert.match(admin, /\/health\/live/);
  assert.match(admin, /\/health\/ready/);
  assert.match(admin, /\/api\/admin\/v1\/ping/);
  assert.match(admin, /X-Request-ID/);
  assert.doesNotMatch(client, /\/api\/admin\/v1/);
});

test('management UIs consume the generated admin API client contract', () => {
  const packagePath = join(root, 'admin/packages/api-client/package.json');
  const generatedPath = join(root, 'admin/packages/api-client/src/generated/admin-v1.ts');
  assert.equal(existsSync(packagePath), true, packagePath);
  assert.equal(existsSync(generatedPath), true, generatedPath);
  const generated = readFileSync(generatedPath, 'utf8');
  assert.match(generated, /Generated from contracts\/openapi\/admin-v1\.yaml/);
  assert.match(generated, /adminAuthLogin:\s*'\/admin\/v1\/auth\/login'/);
  assert.match(generated, /listVisibleMenus:\s*'\/admin\/v1\/menu\/all'/);
  for (const app of managementApps) {
    const packageJSON = readFileSync(join(root, `admin/apps/${app}/package.json`), 'utf8');
    const auth = readFileSync(join(root, `admin/apps/${app}/src/api/core/auth.ts`), 'utf8');
    const menu = readFileSync(join(root, `admin/apps/${app}/src/api/core/menu.ts`), 'utf8');
    assert.match(packageJSON, /"@vben\/api-client":\s*"workspace:\*"/, app);
    assert.match(auth, /from '@vben\/api-client'/, `${app} auth client`);
    assert.match(menu, /from '@vben\/api-client'/, `${app} menu client`);
    assert.doesNotMatch(auth, /export const AUTH_ENDPOINTS/, `${app} duplicated auth endpoints`);
  }
  const generation = runNode('scripts/generate-openapi.mjs', '--check');
  assert.equal(generation.status, 0, generation.stderr || generation.stdout);
  assert.match(generation.stdout, /OPENAPI_CLIENT_CHECK_OK/);
});

test('installation contract exposes credential-free status on its own scope', () => {
  const install = readFileSync(join(root, 'contracts/openapi/install-v1.yaml'), 'utf8');
  assert.match(install, /\/api\/system\/install\/v1\/status:/);
  assert.match(install, /get:/);
  assert.match(install, /InstallationStatus/);
  assert.match(install, /schemaVersion/);
  assert.match(install, /installerVersion/);
  assert.match(install, /selectedUi/);
  assert.match(install, /enum: \[pristine, ui_prepared, installing, installed, inconsistent\]/);
  assert.match(install, /enum: \[antd, ele, naive\]/);
  assert.match(install, /enum: \[embedded, standalone, api_only, dev\]/);
  const statusSchema = install.slice(install.indexOf('    InstallationStatus:'), install.indexOf('    ErrorEnvelope:'));
  assert.doesNotMatch(statusSchema, /password|dsn|jwtSecret|redisPassword/i);
  assert.doesNotMatch(install, /\/api\/admin\/v1|\/api\/client\/v1/);
});

test('installation contract exposes asynchronous UI preparation without widening apply', () => {
  const install = readFileSync(join(root, 'contracts/openapi/install-v1.yaml'), 'utf8');
  for (const path of [
    '/api/system/install/v1/ui/prepare:',
    '/api/system/install/v1/ui/progress/{id}:',
    '/api/system/install/v1/ui/reset:',
  ]) {
    assert.match(install, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
    const sectionStart = install.indexOf(`  ${path}`);
    const nextSection = install.indexOf('\n  /', sectionStart + 4);
    const section = install.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
    assert.match(section, /'503':[\s\S]*?InstallServiceUnavailableEnvelope/, `${path} 503 schema`);
  }
  assert.match(install, /UIPrepareRequest/);
  assert.match(install, /UIResetRequest/);
  assert.match(install, /UIPreparationJob/);
  assert.match(install, /confirmCleanup/);
  assert.match(install, /confirmReset/);
  assert.match(install, /enum: \[queued, running, succeeded, failed\]/);
  assert.match(install, /enum: \[prepare, reset\]/);
  assert.match(install, /currentStep/);
  assert.match(install, /errorKey/);
  assert.match(install, /logPath/);
  assert.match(install, /phase:[\s\S]*?enum: \[ui_prepare, apply\]/);
  assert.match(install, /uiAction:[\s\S]*?enum: \[prepare, reset\]/);
  assert.match(
    install,
    /InstallServiceUnavailableEnvelope:[\s\S]*?const: installation service unavailable/,
  );

  const jobSchema = install.slice(
    install.indexOf('    UIPreparationJob:'),
    install.indexOf('    UIPreparationErrorEnvelope:'),
  );
  for (const stableValue of [
    'ui_switch_failed',
    'ui_workspace_layout_invalid',
    'ui_workspace_transaction_invalid',
    'workspace_layout_invalid',
    'workspace_transaction_invalid',
  ]) {
    assert.match(jobSchema, new RegExp(`- ${stableValue}`), stableValue);
  }
  assert.doesNotMatch(jobSchema, /command|stdout|stderr|absolutePath|password|dsn|secret|token/i);
  const applySchema = install.slice(install.indexOf('    ApplyRequest:'), install.indexOf('    AdminAccount:'));
  assert.doesNotMatch(applySchema, /selectedUi|confirmCleanup|confirmReset/);
});

test('installation contract exposes a permission plan without filesystem details', () => {
  const install = readFileSync(join(root, 'contracts/openapi/install-v1.yaml'), 'utf8');
  assert.match(install, /\/api\/system\/install\/v1\/plan:/);
  assert.match(install, /post:/);
  assert.match(install, /PlanRequest/);
  assert.match(install, /InstallationPlan/);
  assert.match(install, /canCleanup/);
  assert.match(install, /canBuild/);
  assert.match(install, /canWriteEnv/);
  assert.match(install, /requiresRestart/);
  assert.match(install, /path:/);
  assert.match(install, /action:/);
  const requestSchema = install.slice(install.indexOf('    PlanRequest:'), install.indexOf('    InstallationPlanEnvelope:'));
  assert.match(requestSchema, /required: \[mode\]/);
  assert.doesNotMatch(requestSchema, /selectedUi/);
  const planSchema = install.slice(install.indexOf('    InstallationPlan:'), install.indexOf('    PlanErrorEnvelope:'));
  assert.doesNotMatch(planSchema, /absolutePath|rootPath|password|dsn|jwtSecret|redisPassword/i);
});

test('installation contract exposes database and Redis connection checks', () => {
  const install = readFileSync(join(root, 'contracts/openapi/install-v1.yaml'), 'utf8');
  for (const path of ['/api/system/install/v1/check/database:', '/api/system/install/v1/check/redis:']) {
    assert.match(install, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
    const sectionStart = install.indexOf(`  ${path}`);
    const nextSection = install.indexOf('\n  /', sectionStart + 4);
    const section = install.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
    assert.match(section, /post:/, `${path} method`);
    assert.match(section, /'200':/, `${path} success`);
    assert.match(section, /'400':/, `${path} validation`);
    assert.match(section, /'503':/, `${path} unavailable`);
  }
  assert.match(install, /DatabaseConnection/);
  assert.match(install, /RedisConnection/);
  assert.match(install, /DependencyCheck/);
  assert.match(install, /latencyMs/);
  assert.match(install, /password:[\s\S]*?writeOnly: true/);
  assert.match(install, /dsn:[\s\S]*?writeOnly: true/);
  const checkSchema = install.slice(install.indexOf('    DependencyCheck:'), install.indexOf('    ConnectionErrorEnvelope:'));
  assert.doesNotMatch(checkSchema, /password|dsn|absolutePath|rootPath|jwtSecret|redisPassword/i);
});

test('installation contract exposes one credential-write-only apply operation', () => {
  const install = readFileSync(join(root, 'contracts/openapi/install-v1.yaml'), 'utf8');
  const errors = readFileSync(join(root, 'contracts/errors/error-codes.yaml'), 'utf8');
  const path = '/api/system/install/v1/apply:';
  assert.match(install, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`));
  const sectionStart = install.indexOf(`  ${path}`);
  const nextSection = install.indexOf('\n  /', sectionStart + 4);
  const section = install.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
  assert.match(section, /post:/);
  assert.match(section, /ApplyRequest/);
  for (const status of ['200', '202', '400', '409', '422', '500', '503']) {
    assert.match(section, new RegExp(`'${status}':`), `apply ${status}`);
  }
  assert.match(install, /\/api\/system\/install\/v1\/progress\/{id}:/);
  assert.match(install, /\/api\/system\/install\/v1\/retry\/{id}:/);
  assert.match(install, /ApplyJob/);
  const jobSchema = install.slice(install.indexOf('    ApplyJob:'), install.indexOf('    RollbackRequest:'));
  assert.match(jobSchema, /failureStep:[\s\S]*?enum: \[request, coordination, plan, database, redis, recovery, journal, schema, identity, environment, marker, lock, complete\]/);
  assert.match(jobSchema, /failureReason:[\s\S]*?enum: \[tls_mode_mismatch, tls_configuration_failed, authentication_failed, permission_denied, database_unavailable, database_busy, schema_unavailable, schema_conflict, migration_dirty, migration_statement_failed, migration_status_failed, migration_close_failed, invalid_configuration, navigation_seed_conflict, unknown\]/);
  assert.match(jobSchema, /failureOperation:[\s\S]*?enum: \[connect, apply, status, close\]/);
  assert.match(jobSchema, /databaseCode:[\s\S]*?pattern: '\^\[0-9A-Z\]\{5\}\$'/);
  assert.match(jobSchema, /failureResourceKind:[\s\S]*?enum: \[menu, permission\]/);
  assert.ok(jobSchema.includes("failureResourceId:\n          type: string\n          pattern: '^[A-Za-z0-9:._-]{1,128}$'"));
  assert.doesNotMatch(jobSchema, /password|dsn|secret|rawError|errorDetail|query/i);
  assert.match(install, /AdminAccount/);
  const applySchema = install.slice(install.indexOf('    ApplyRequest:'), install.indexOf('    AdminAccount:'));
  assert.match(applySchema, /required: \[mode, database, redis, admin\]/);
  assert.doesNotMatch(applySchema, /selectedUi|confirmCleanup/);
  assert.match(applySchema, /localeMode:[\s\S]*enum: \[single, multi\]/);
  assert.match(applySchema, /locale:[\s\S]*enum: \[zh-CN, en-US\]/);
  const adminSchema = install.slice(
    install.indexOf('    AdminAccount:'),
    install.indexOf('    ApplyResultEnvelope:'),
  );
  assert.match(adminSchema, /password:[\s\S]*?minLength: 6/);
  assert.match(adminSchema, /password:[\s\S]*?maxLength: 72/);
  assert.ok(
    adminSchema.includes(
      "pattern: '^(?=.*[A-Za-z])(?=.*[0-9])[A-Za-z0-9]{6,72}$'",
    ),
  );
  assert.match(adminSchema, /password:[\s\S]*?writeOnly: true/);
  const result = install.slice(install.indexOf('    ApplyResult:'), install.indexOf('    ApplyErrorEnvelope:'));
  assert.doesNotMatch(result, /password|dsn|secret/i);
  assert.match(errors, /code: 10006[\s\S]*?key: installation_completed[\s\S]*?http_status: 409/);
  assert.match(errors, /code: 10007[\s\S]*?key: installation_running[\s\S]*?http_status: 409/);
  assert.match(errors, /code: 10008[\s\S]*?key: installation_required[\s\S]*?http_status: 423/);
  assert.match(install, /10008/);
});

test('installation contract exposes an explicit confirmed rollback operation', () => {
  const install = readFileSync(join(root, 'contracts/openapi/install-v1.yaml'), 'utf8');
  const errors = readFileSync(join(root, 'contracts/errors/error-codes.yaml'), 'utf8');
  const path = '/api/system/install/v1/rollback/{id}:';
  assert.match(install, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`));
  const sectionStart = install.indexOf(`  ${path}`);
  const nextSection = install.indexOf('\n  /', sectionStart + 4);
  const section = install.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
  assert.match(section, /post:/);
  assert.match(section, /RollbackRequest/);
  for (const status of ['200', '400', '404', '409', '503']) {
    assert.match(section, new RegExp(`'${status}':`), `rollback ${status}`);
  }
  assert.match(install, /confirmRollback/);
  assert.match(install, /RollbackResult/);
  assert.match(errors, /code: 10009[\s\S]*?key: installation_rollback_unavailable[\s\S]*?http_status: 409/);
});

test('installation workspace smoke and runtime artifacts stay cross-platform', () => {
  const smoke = runNode('admin/apps/install/tests/smoke.test.mjs');
  assert.equal(smoke.status, 0, smoke.stdout + smoke.stderr);
  const ignore = readFileSync(join(root, '.gitignore'), 'utf8');
  assert.match(ignore, /^\/\.runtime\/$/m);
  const config = readFileSync(join(root, 'server/configs/server.example.yaml'), 'utf8');
  assert.match(config, /state_dir:\s+\.\.\/\.runtime\/install/);
  assert.match(ignore, /\*\*\/dist\//);
});

test('authentication contract declares login, refresh, and logout endpoints', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const errors = readFileSync(join(root, 'contracts/errors/error-codes.yaml'), 'utf8');

  for (const path of [
    '/api/admin/v1/auth/login:',
    '/api/admin/v1/auth/refresh:',
    '/api/admin/v1/auth/logout:',
  ]) {
    assert.match(admin, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
    const sectionStart = admin.indexOf(`  ${path}`);
    const nextSection = admin.indexOf('\n  /', sectionStart + 4);
    const section = admin.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
    assert.match(section, /post:/, path);
    assert.match(section, /'200':/, `${path} success`);
    assert.match(section, /'400':/, `${path} bad request`);
    assert.match(section, /'401':/, `${path} unauthorized`);
    assert.match(section, /'503':/, `${path} dependency failure`);
    assert.match(section, /Set-Cookie:/, `${path} cookie response`);
    if (path.endsWith('/login:')) {
      assert.match(section, /'429':/, `${path} rate limit`);
      assert.match(section, /requestBody:/, `${path} request body`);
      assert.match(section, /LoginRequest/, `${path} request schema`);
    } else {
      assert.match(section, /security:/, `${path} cookie security`);
      assert.match(section, /RefreshCookie/, `${path} refresh security`);
      assert.match(section, /RefreshTokenCookie/, `${path} cookie parameter`);
    }
  }

  assert.match(admin, /securitySchemes:/);
  assert.match(admin, /BearerAuth:/);
  assert.match(admin, /type: http/);
  assert.match(admin, /scheme: bearer/);
  assert.match(admin, /RefreshCookie:/);
  assert.match(admin, /in: cookie/);
  assert.match(admin, /HttpOnly/);
  assert.match(admin, /accessToken/);
  assert.match(admin, /tokenType/);
  assert.match(admin, /expiresIn/);
  assert.match(errors, /key: invalid_credentials/);
  assert.match(errors, /key: invalid_token/);
  assert.match(errors, /key: auth_rate_limited/);
  assert.match(errors, /code: 10000[\s\S]*?http_status: 400/);
  assert.match(errors, /http_status: 401/);
  assert.match(admin, /RateLimited:/);
  assert.match(admin, /AuthServiceUnavailable:/);
  assert.match(admin, /\/api\/admin\/v1\/auth\/captcha:/);
  assert.match(admin, /CaptchaChallenge/);
  assert.match(errors, /key: invalid_captcha/);
  assert.match(errors, /code: 10005[\s\S]*?http_status: 400/);
});

test('authentication recovery contract declares registration and reset endpoints', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  for (const path of [
    '/api/admin/v1/auth/register:',
    '/api/admin/v1/auth/password/reset/request:',
    '/api/admin/v1/auth/password/reset:',
  ]) {
    assert.match(admin, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
    const sectionStart = admin.indexOf(`  ${path}`);
    const nextSection = admin.indexOf('\n  /', sectionStart + 4);
    const section = admin.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
    assert.match(section, /post:/, `${path} method`);
    assert.match(section, /requestBody:/, `${path} request body`);
    assert.match(section, /'200':/, `${path} success`);
    assert.match(section, /'400':/, `${path} validation`);
    assert.match(section, /'503':/, `${path} dependency failure`);
  }
  assert.match(admin, /RegisterRequest/);
  assert.match(admin, /PasswordResetRequest/);
  assert.match(admin, /PasswordResetTokenRequest/);
});

test('authentication session contract is bearer-protected and hides token material', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  for (const path of [
    '/api/admin/v1/auth/sessions:',
    '/api/admin/v1/auth/sessions/{id}:',
  ]) {
    assert.match(admin, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
    const sectionStart = admin.indexOf(`  ${path}`);
    const nextSection = admin.indexOf('\n  /', sectionStart + 4);
    const section = admin.slice(sectionStart, nextSection === -1 ? undefined : nextSection);
    assert.match(section, /BearerAuth/, `${path} bearer security`);
    assert.match(section, /'401':/, `${path} unauthorized`);
  }
  assert.match(admin, /SessionData/);
  assert.match(admin, /deviceId/);
  assert.match(admin, /deviceName/);
  assert.match(admin, /ipAddress/);
  assert.doesNotMatch(admin, /refreshTokenHash/);
});

test('RBAC management contract exposes guarded collections and denial code', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  for (const path of [
    '/api/admin/v1/iam/me:',
    '/api/admin/v1/iam/users:',
    '/api/admin/v1/iam/roles:',
    '/api/admin/v1/iam/menus:',
    '/api/admin/v1/iam/permissions:',
    '/api/admin/v1/iam/policies:',
    '/api/admin/v1/iam/data-scopes:',
    '/api/admin/v1/menu/all:',
  ]) {
    assert.match(admin, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
  }
  assert.match(admin, /BearerAuth/);
  assert.match(admin, /code: 30000|const: 30000/);
  for (const schema of ['IAMUser', 'IAMRole', 'IAMMenu', 'IAMPermission', 'IAMPolicy', 'IAMDataScope']) {
    assert.match(admin, new RegExp(`^    ${schema}:`, 'm'), schema);
  }
});

test('settings and audit contracts expose versioned guarded management seams', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const errors = readFileSync(join(root, 'contracts/errors/error-codes.yaml'), 'utf8');
  for (const path of [
    '/api/admin/v1/settings:',
    '/api/admin/v1/settings/{key}:',
    '/api/admin/v1/settings/{key}/history:',
    '/api/admin/v1/settings/{key}/rollback:',
    '/api/admin/v1/audit/events:',
  ]) {
    assert.match(admin, new RegExp(`\\n  ${path.replaceAll('/', '\\/')}`), path);
    const start = admin.indexOf(`  ${path}`);
    const next = admin.indexOf('\n  /', start + 4);
    const section = admin.slice(start, next === -1 ? undefined : next);
    assert.match(section, /BearerAuth/, `${path} bearer security`);
  }
  assert.match(admin, /SettingData/);
  assert.match(admin, /AuditPage/);
  assert.match(admin, /expectedVersion/);
  assert.match(errors, /code: 10010[\s\S]*?key: setting_version_conflict[\s\S]*?http_status: 409/);
});

test('web-antd auth seam uses the versioned API and sends the refresh cookie', () => {
  const auth = readFileSync(join(root, 'admin/apps/web-antd/src/api/core/auth.ts'), 'utf8');
  const generated = readFileSync(join(root, 'admin/packages/api-client/src/generated/admin-v1.ts'), 'utf8');
  const login = readFileSync(
    join(root, 'admin/apps/web-antd/src/views/_core/authentication/login.vue'),
    'utf8',
  );

  assert.match(auth, /from '@vben\/api-client'/);
  assert.match(auth, /AUTH_ENDPOINTS\.login/);
  assert.match(auth, /AUTH_ENDPOINTS\.refresh/);
  assert.match(auth, /AUTH_ENDPOINTS\.logout/);
  for (const endpoint of ['/admin/v1/auth/login', '/admin/v1/auth/refresh', '/admin/v1/auth/logout']) {
    assert.match(generated, new RegExp(endpoint.replaceAll('/', '\\/')));
  }
  assert.match(auth, /withCredentials:\s*true/);
  assert.match(auth, /undefined\s*,\s*\{\s*withCredentials:/s);
  assert.match(login, /authStore\.loginLoading/);
  assert.match(login, /login-error|login-success|role=["']alert["']/);
});

test('all management UIs expose equivalent login loading and result states', () => {
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const login = readFileSync(
      join(root, `admin/apps/${ui}/src/views/_core/authentication/login.vue`),
      'utf8',
    );
    assert.match(login, /loginLoading/, `${ui} loading state`);
    assert.match(login, /data-testid="login-error"/, `${ui} error target`);
    assert.match(login, /role="alert"/, `${ui} error announcement`);
    assert.match(login, /data-testid="login-success"/, `${ui} success target`);
    assert.match(login, /role="status"/, `${ui} success announcement`);
  }
});

test('all management UIs expose versioned settings and audit clients', () => {
  const generated = readFileSync(join(root, 'admin/packages/api-client/src/generated/admin-v1.ts'), 'utf8');
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const settings = readFileSync(join(root, `admin/apps/${ui}/src/api/core/settings.ts`), 'utf8');
    const audit = readFileSync(join(root, `admin/apps/${ui}/src/api/core/audit.ts`), 'utf8');
    for (const fn of ['listSettingDefinitionsApi', 'getSettingApi', 'updateSettingApi', 'listSettingModulesApi', 'getSettingModuleApi', 'updateSettingModuleApi']) {
      assert.match(settings, new RegExp(`export async function ${fn}`), `${ui} ${fn}`);
    }
    assert.match(audit, /export async function queryAuditEventsApi/);
    assert.match(settings, /ADMIN_ENDPOINTS\.getSetting/);
    assert.match(audit, /ADMIN_ENDPOINTS\.queryAuditEvents/);
  }
  for (const endpoint of ['/admin/v1/settings', '/admin/v1/settings/{key}', '/admin/v1/audit/events']) {
    assert.match(generated, new RegExp(endpoint.replaceAll('/', '\\/')));
  }
});

test('management login forms do not expose mock accounts or preset passwords', () => {
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const login = readFileSync(
      join(root, `admin/apps/${ui}/src/views/_core/authentication/login.vue`),
      'utf8',
    );
    assert.doesNotMatch(login, /MOCK_USER_OPTIONS|selectAccount|123456/, `${ui} mock credentials`);
    for (const field of ['identifier', 'password', 'captcha']) {
      assert.match(login, new RegExp(`fieldName:\\s*['"]${field}['"]`), `${ui} ${field}`);
    }
  }
});

test('management UIs and installer expose a Playwright axe gate', () => {
  const pkg = JSON.parse(readFileSync(join(root, 'admin/package.json'), 'utf8'));
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
  assert.match(pkg.scripts?.['test:e2e:a11y'] ?? '', /playwright test/);
  assert.ok(pkg.devDependencies?.['@axe-core/playwright']);
  assert.ok(pkg.devDependencies?.['@playwright/test']);
  for (const path of [
    'admin/playwright.config.ts',
    'admin/tests/e2e/accessibility.spec.ts',
    'admin/tests/e2e/static-server.mjs',
  ]) {
    assert.equal(existsSync(join(root, path)), true, path);
  }
  assert.match(workflow, /playwright install --with-deps chromium/);
  assert.match(workflow, /pnpm --dir admin run test:e2e:a11y/);
});

test('management UI clients use versioned authentication and menu endpoints', () => {
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const auth = readFileSync(join(root, `admin/apps/${ui}/src/api/core/auth.ts`), 'utf8');
    const menu = readFileSync(join(root, `admin/apps/${ui}/src/api/core/menu.ts`), 'utf8');
    assert.match(auth, /from '@vben\/api-client'/, `${ui} shared client`);
    for (const endpoint of ['login', 'refresh', 'logout']) {
      assert.match(auth, new RegExp(`AUTH_ENDPOINTS\\.${endpoint}`), `${ui} ${endpoint}`);
    }
    assert.match(auth, /withCredentials:\s*true/, `${ui} refresh credentials`);
    assert.match(auth, /getCaptchaApi/, `${ui} captcha client`);
    assert.match(auth, /captchaId/, `${ui} captcha challenge id`);
    assert.match(menu, /MENU_ENDPOINT/, `${ui} menu endpoint`);
  }
});

test('management UI clients use the versioned current-user endpoint', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const generated = readFileSync(join(root, 'admin/packages/api-client/src/generated/admin-v1.ts'), 'utf8');
  assert.match(admin, /\/api\/admin\/v1\/iam\/me:/);
  assert.match(generated, /CURRENT_USER_ENDPOINT/);
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const user = readFileSync(join(root, `admin/apps/${ui}/src/api/core/user.ts`), 'utf8');
    assert.match(user, /CURRENT_USER_ENDPOINT/, `${ui} current-user client`);
    assert.doesNotMatch(user, /['"]\/user\/info['"]/, `${ui} stale user endpoint`);
  }
});

test('management UI clients expose account recovery and device-session endpoints', () => {
  const generated = readFileSync(join(root, 'admin/packages/api-client/src/generated/admin-v1.ts'), 'utf8');
  for (const ui of ['web-antd', 'web-ele', 'web-naive']) {
    const auth = readFileSync(join(root, `admin/apps/${ui}/src/api/core/auth.ts`), 'utf8');
    for (const endpoint of ['register', 'passwordResetRequest', 'passwordReset', 'sessions']) {
      assert.match(auth, new RegExp(`AUTH_ENDPOINTS\\.${endpoint}`), `${ui} ${endpoint}`);
    }
    for (const fn of ['registerApi', 'requestPasswordResetApi', 'resetPasswordApi', 'listSessionsApi', 'revokeSessionApi']) {
      assert.match(auth, new RegExp(`export async function ${fn}`), `${ui} ${fn}`);
    }
    assert.match(auth, /withCredentials:\s*true/);
  }
  for (const endpoint of ['/admin/v1/auth/register', '/admin/v1/auth/password/reset/request', '/admin/v1/auth/password/reset', '/admin/v1/auth/sessions']) {
    assert.match(generated, new RegExp(endpoint.replaceAll('/', '\\/')));
  }
});

test('bootstrap check is cross-platform and verification succeeds', () => {
  const bootstrap = runNode('scripts/bootstrap.mjs', '--check');
  assert.equal(bootstrap.status, 0, bootstrap.stdout + bootstrap.stderr);
  const verify = runNode('scripts/verify.mjs', '--scope', 'basic');
  assert.equal(verify.status, 0, verify.stdout + verify.stderr);
  assert.match(verify.stdout, /VERIFY_OK/);
});

test('container build prepares the workspace with Alpine deploy images', () => {
  const dockerfile = readFileSync(join(root, 'deploy/admin.Dockerfile'), 'utf8');
  assert.match(dockerfile, /FROM node:.*-alpine/);
  assert.match(dockerfile, /pnpm install --frozen-lockfile/);
  assert.match(dockerfile, /nginx:.*-alpine/);
});

test('install flow advertises the explicit migration command', () => {
  const readme = readFileSync(join(root, 'README.md'), 'utf8');
  assert.match(readme, /docker compose -f deploy\/docker-compose\.yml run --rm migrate status/);
  assert.match(readme, /go -C server run \.\/cmd\/migrate status/);
});

test('CI covers the three host platforms and core gates', () => {
  const workflowPath = join(root, '.github/workflows/ci.yml');
  assert.equal(existsSync(workflowPath), true, workflowPath);
  const workflow = readFileSync(workflowPath, 'utf8');
  for (const platform of ['ubuntu-latest', 'macos-latest', 'windows-latest']) {
    assert.match(workflow, new RegExp(platform));
  }
  assert.match(workflow, /pnpm\/action-setup/);
  assert.match(workflow, /node --test tests\/contract\/contract\.test\.mjs/);
  assert.match(workflow, /pnpm --dir admin run test:smoke/);
  assert.match(workflow, /go -C server test \.\/\.\.\./);
});

test('dev orchestrator exposes a cross-platform check mode', () => {
  const source = readFileSync(join(root, 'scripts/dev.mjs'), 'utf8');
  assert.match(source, /shell:\s*false/);
  const check = runNode('scripts/dev.mjs', '--check');
  assert.equal(check.status, 0, check.stdout + check.stderr);
  assert.match(check.stdout, /DEV_CHECK_OK/);
  assert.match(check.stdout, /go -C server run \.\/cmd\/api/);
  assert.match(check.stdout, /pnpm --dir admin run dev/);
  assert.doesNotMatch(check.stdout, /--filter|dev:(?:antd|ele|naive)/);
});

test('public surface documents config topologies and explicit migrations', () => {
  const readme = readFileSync(join(root, 'README.md'), 'utf8');
  const example = readFileSync(join(root, 'server/configs/server.example.yaml'), 'utf8');
  assert.match(readme, /0\.9\.0-rc/);
  assert.match(readme, /cmd\/migrate/);
  assert.match(readme, /migrate status/);
  for (const mode of ['single', 'read_write', 'cluster_endpoint']) assert.match(example, new RegExp(mode));
  for (const mode of ['single', 'sentinel', 'cluster']) assert.match(example, new RegExp(mode));
  assert.match(example, /namespace: app:v1/);
  assert.equal(existsSync(join(root, 'server/cmd/migrate')), true);
});

test('readiness error contract uses the dependency-unavailable code', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const errors = readFileSync(join(root, 'contracts/errors/error-codes.yaml'), 'utf8');
  assert.match(admin, /40001/);
  assert.match(admin, /const: 40001/);
  assert.match(errors, /code: 40001/);
  assert.match(errors, /dependency_unavailable/);
});

test('verification awaits asynchronous repository checks', () => {
  const source = readFileSync(join(root, 'scripts/verify.mjs'), 'utf8');
  assert.match(source, /Promise\.all/);
  assert.doesNotMatch(source, /required\.filter\(\(item\) => !exists\(item\)\)/);
});

test('single-node integration CI runs the explicit gated suite', () => {
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
  for (const variable of [
    'DATA_PLATFORM_INTEGRATION',
    'TEST_MYSQL_DSN',
    'TEST_POSTGRES_DSN',
    'TEST_REDIS_ADDR',
  ]) {
    assert.match(workflow, new RegExp(variable));
  }
  const postgresDSN = workflow.match(/TEST_POSTGRES_DSN:\s*([^\n]+)/)?.[1] || '';
  assert.doesNotMatch(postgresDSN, /sslmode=/, 'Postgres integration must exercise the omitted sslmode installer path');
  assert.match(workflow, /go -C server test \.\/tests\/integration/);
});

test('development compose wires the API to the default single-node dependencies', () => {
  const compose = readFileSync(join(root, 'deploy/docker-compose.yml'), 'utf8');
  const server = compose.slice(compose.indexOf('  server:'), compose.indexOf('\n  admin:'));
  for (const variable of ['DATABASE_ENABLED', 'DATABASE_DRIVER', 'DATABASE_MODE', 'DATABASE_DSN', 'REDIS_ENABLED', 'REDIS_MODE', 'REDIS_ADDR']) {
    assert.match(server, new RegExp(variable));
  }
  assert.doesNotMatch(server, /postgres:\s*\n\s+condition:/);
  assert.match(server, /REDIS_NAMESPACE: app:v1/);
  assert.doesNotMatch(compose, /postgres|sentinel|cluster|prometheus|grafana/i);
  const runtimeGenerator = readFileSync(join(root, 'scripts/prepare-runtime-compose.mjs'), 'utf8');
  assert.match(runtimeGenerator, /postgres\.yaml/);
  assert.match(runtimeGenerator, /observability\.yaml/);
});

test('readiness contract describes dependency check states', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  assert.match(admin, /checks:/);
  assert.match(admin, /enum: \[up, down\]/);
  assert.match(admin, /enum: \[ok, ready, unavailable\]/);
});

test('bootstrap renders the selected database driver into a new local config', async () => {
  const { renderServerConfig } = await import('../../scripts/bootstrap-config.mjs');
  const template = 'database:\n  enabled: false\n  driver: mysql\n';
  const postgres = renderServerConfig(template, 'postgres');
  assert.match(postgres, /driver: postgres/);
  assert.doesNotMatch(postgres, /driver: mysql/);
  assert.throws(() => renderServerConfig(template, 'sqlite'));
});

test('server image packages the explicit migration command', () => {
  const dockerfile = readFileSync(join(root, 'deploy/server.Dockerfile'), 'utf8');
  const readme = readFileSync(join(root, 'README.md'), 'utf8');
  assert.match(dockerfile, /-o \/out\/migrate \.\/cmd\/migrate/);
  assert.match(dockerfile, /COPY --from=build \/out\/migrate \/migrate/);
  assert.match(readme, /run --rm migrate status/);
});
