import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const read = (relative) => readFileSync(join(root, relative), 'utf8');

test('admin exposes one profile-driven command for each runtime action', () => {
  const pkg = JSON.parse(read('admin/package.json'));
  for (const command of [
    'build:analyze',
    'build:antd',
    'build:ele',
    'build:naive',
    'dev:antd',
    'dev:ele',
    'dev:naive',
  ]) {
    assert.equal(Object.hasOwn(pkg.scripts, command), false, command);
  }

  for (const command of ['build', 'dev', 'preview']) {
    assert.match(pkg.scripts[command], /profile-gate\.mjs/);
    assert.match(pkg.scripts[command], /selected-dispatch\.mjs/);
  }
  assert.doesNotMatch(read('admin/turbo.json'), /"build:analyze"/);
  assert.doesNotMatch(read('admin/scripts/profile-gate.mjs'), /build:analyze/);
  assert.doesNotMatch(read('admin/scripts/selected-dispatch.mjs'), /build:analyze/);
});

test('internal automation no longer depends on removed public UI scripts', () => {
  for (const relative of [
    '.github/workflows/ci.yml',
    'admin/Dockerfile',
    'scripts/build.mjs',
    'scripts/dev.mjs',
  ]) {
    const source = read(relative);
    assert.doesNotMatch(source, /(?:build|dev):(antd|ele|naive)/, relative);
  }
  assert.doesNotMatch(read('scripts/dev.mjs'), /--filter/);
  assert.match(read('scripts/dev.mjs'), /buildPnpmCommand/);
  assert.doesNotMatch(read('scripts/dev.mjs'), /pnpm\.cmd/);
});

test('ordinary API replaces the temporary init command', () => {
  assert.equal(existsSync(join(root, 'server/cmd/init')), false);
  assert.equal(existsSync(join(root, 'admin/scripts/init-runtime.mjs')), false);
});

test('README documents the dependency-minimal quick start and protected state', () => {
  const readmes = `${read('README.md')}\n${read('admin/README.md')}`;
  assert.doesNotMatch(read('README.md'), /^pnpm --dir admin install\b/m);
  for (const command of [
    'go run ./cmd/api/main.go',
    'pnpm run init',
    'pnpm run build',
    'pnpm run dev',
  ]) {
    assert.match(readmes, new RegExp(command.replaceAll('.', '\\.')));
  }
  for (const statePath of [
    'admin/.ui-profile.json',
    '.runtime/install/',
    '.runtime/install/.installed',
    '.runtime/install/admin-init.lock',
    '.runtime/install/admin-init-heartbeat/',
    '.runtime/install/admin-init.lock.reclaim',
    '.runtime/install/apply.lock',
    '.runtime/install/dependency-install.lock',
    '.runtime/install/dependency-install.lock.reclaim',
    '.runtime/install/dependency-install-heartbeat/',
    '.runtime/install/dependency-install.log',
    '.runtime/install/dependency-job-gate-<UUID>.json',
    '.runtime/install/process.guard',
    '.runtime/install/.installed.lock',
    '.runtime/install/transaction.json',
    '.runtime/install/ui-backup/',
    '.runtime/install/legacy-prepared-migration.json',
    '.runtime/install/legacy-recovery/<transaction>/.ui-init-receipt.json',
    '.runtime/init-backup/',
    '.runtime/init-recovery/',
  ]) {
    assert.match(readmes, new RegExp(statePath.replaceAll('.', '\\.')));
  }
  assert.match(readmes, /不要手动删除/);
});

test('fresh source quick start selects UI in the browser without an init terminal', () => {
  const rootReadme = read('README.md');
  const adminReadme = read('admin/README.md');
  const acceptance = read('docs/manual-acceptance/1.0.0-dev-end-to-end.md');
  const rootQuickStart = rootReadme.slice(
    rootReadme.indexOf('### 初始化与源码运行'),
    rootReadme.indexOf('在网页中完成数据库'),
  );
  const adminQuickStart = adminReadme.slice(
    adminReadme.indexOf('## 初始化与验证'),
    adminReadme.indexOf('状态和恢复命令'),
  );
  const acceptanceQuickStart = acceptance.slice(
    acceptance.indexOf('### UAT-INSTALL-001'),
    acceptance.indexOf('### UAT-INSTALL-002'),
  );

  for (const [name, section] of [
    ['README', rootQuickStart],
    ['admin README', adminQuickStart],
    ['acceptance', acceptanceQuickStart],
  ]) {
    assert.match(section, /go run \.\/cmd\/api\/main\.go/iu, name);
    assert.match(section, /http:\/\/127\.0\.0\.1:8080\/install/iu, name);
    assert.match(section, /Ant Design Vue|Element Plus|Naive UI/iu, name);
    assert.doesNotMatch(section, /^\s*pnpm run init(?:\s|$)/mu, name);
  }
});

test('post-install quick start uses two terminals and installs before dev without a build prerequisite', () => {
  const rootReadme = read('README.md');
  const adminReadme = read('admin/README.md');
  const acceptance = read('docs/manual-acceptance/1.0.0-dev-end-to-end.md');
  const rootQuickStart = rootReadme.slice(
    rootReadme.indexOf('在网页中完成数据库'),
    rootReadme.indexOf('编辑本地 `server/configs/server.yaml`'),
  );
  const adminQuickStart = adminReadme.slice(
    adminReadme.indexOf('安装成功后'),
    adminReadme.indexOf('以下状态由程序维护'),
  );
  const acceptanceQuickStart = acceptance.slice(
    acceptance.indexOf('### UAT-INSTALL-002'),
    acceptance.indexOf('### UAT-INSTALL-003'),
  );

  for (const [name, section] of [
    ['README', rootQuickStart],
    ['admin README', adminQuickStart],
    ['acceptance', acceptanceQuickStart],
  ]) {
    assert.match(section, /终端 1/iu, name);
    assert.match(section, /终端 2/iu, name);
    assert.match(section, /终端 1[\s\S]*cd server[\s\S]*go run \.\/cmd\/api\/main\.go[\s\S]*终端 2[\s\S]*cd admin[\s\S]*pnpm install[\s\S]*pnpm run dev/iu, name);
    assert.match(section, /go run \.\/cmd\/api\/main\.go/iu, name);
    assert.match(section, /pnpm install[\s\S]*pnpm run dev/iu, name);
    assert.doesNotMatch(section, /pnpm run build/iu, name);
  }
});

test('installer contract and ignore policy use only the unified runtime state', () => {
  const installContract = read('contracts/openapi/install-v1.yaml');
  const applyStep = installContract.slice(
    installContract.indexOf('    ApplyStep:'),
    installContract.indexOf('    ApplyErrorEnvelope:'),
  );
  assert.match(applyStep, /enum: \[plan, database, redis, schema, identity, environment, lock\]/);
  assert.doesNotMatch(applyStep, /\bassets\b/);

  const gitignore = read('.gitignore');
  const dockerignore = read('.dockerignore');
  assert.match(gitignore, /^\/\.runtime\/$/m);
  assert.match(dockerignore, /^\.runtime$/m);
  for (const source of [gitignore, dockerignore]) {
    assert.doesNotMatch(
      source,
      /admin\/apps\/install\/\.(?:installed|installed\.lock|install-state|install-backup|env-backup)/,
    );
    assert.doesNotMatch(source, /admin\/\.ui-init-transaction\.json/);
  }
  assert.doesNotMatch(gitignore, /^\/\.(?:init|init-backup)\/$/m);
  for (const legacyState of ['.ui-init-receipt.json', '.ui-init-runtime.json']) {
    assert.match(gitignore, new RegExp(`^/admin/${legacyState.replaceAll('.', '\\.')}$`, 'm'));
    assert.match(dockerignore, new RegExp(`^admin/${legacyState.replaceAll('.', '\\.')}$`, 'm'));
  }
});

test('public initialization docs contain no removed entry points or active use of legacy state paths', () => {
  const publicDocFiles = [
    'README.md',
    'admin/README.md',
    'docs/manual-acceptance/1.0.0-dev-end-to-end.md',
  ];
  const publicDocs = publicDocFiles.map(read).join('\n');
  assert.doesNotMatch(publicDocs, /server\/cmd\/init|admin\/scripts\/init-runtime/);
  assert.doesNotMatch(publicDocs, /pnpm run (?:build|dev):(antd|ele|naive)/);
  assert.doesNotMatch(
    publicDocs,
    /admin\/apps\/install\/\.(?:installed|installed\.lock|install-state|install-backup|env-backup)/,
  );
  for (const relative of publicDocFiles) {
    for (const line of read(relative).split('\n').filter((entry) => /\.runtime\/init-(?:backup|recovery)/.test(entry))) {
      assert.match(line, /旧版|历史|迁移|接管|冲突/, `${relative}: ${line}`);
    }
  }
});
