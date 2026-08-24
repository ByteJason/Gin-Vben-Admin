import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test } from 'node:test';

const root = resolve(import.meta.dirname, '..');
const profilePath = resolve(root, '.ui-profile.json');
const requiredApps = existsSync(profilePath)
  ? [`web-${JSON.parse(readFileSync(profilePath, 'utf8')).selectedUi}`]
  : ['web-antd', 'web-ele', 'web-naive'];
const brandLogoFilename = 'gin-vben-admin-logo.png';
const brandLogoSha256 =
  'a76a68003fdc33d7a112e9683cda3a74603362d372195421b2983e902d44ca07';
const brandPwaLogoSha256 = new Map([
  [192, '5358825f8ad8dcfc4d85b50819ec641c9eaf1c9f2e3fcbbdf5506831836bf668'],
  [512, '0e888e36c58685fcc09d365e462b6f4f554cdd02575f65bc92bd25c70775cb1b'],
]);

test('workspace exposes the supported UI templates', () => {
  const apps = readdirSync(resolve(root, 'apps'), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();
  assert.deepEqual(apps, ['install', ...requiredApps].sort());
});

test('workspace has the expected package layout', () => {
  const workspace = readFileSync(resolve(root, 'pnpm-workspace.yaml'), 'utf8');
  const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
  assert.match(workspace, /apps\/\*/);
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
  assert.doesNotMatch(pkg.scripts['test:e2e:a11y'], /pnpm run build/);
  for (const packageName of [
    '@vben/web-antd',
    '@vben/web-ele',
    '@vben/web-naive',
  ]) {
    assert.match(
      pkg.scripts['test:e2e:a11y'],
      new RegExp(`pnpm --filter ${packageName.replace('/', '\\/')} run build`),
    );
  }
  assert.match(pkg.scripts['test:e2e:a11y'], /build:installer/);
  assert.match(pkg.scripts['test:e2e:a11y'], /playwright test/);
  assert.equal(Object.hasOwn(pkg.scripts, 'preinstall'), false);
  for (const command of ['preinstall', 'install', 'postinstall']) {
    assert.doesNotMatch(pkg.scripts[command] ?? '', /\bnpx\b|only-allow/);
  }
});

test('workspace contains its frontend build closure', () => {
  for (const path of ['packages', 'internal', 'scripts', 'pnpm-lock.yaml']) {
    assert.ok(existsSync(resolve(root, path)), path);
  }
});

test('every management template provides complete runtime environment examples', () => {
  for (const app of requiredApps) {
    for (const mode of ['development', 'production']) {
      const template = readFileSync(
        resolve(root, 'apps', app, `.env.${mode}.example`),
        'utf8',
      );
      assert.match(
        template,
        /^VITE_APP_TITLE=Gin Vben Admin$/m,
        `${app} ${mode} title`,
      );
      assert.match(
        template,
        /^VITE_GLOB_API_URL=\/api$/m,
        `${app} ${mode} API base`,
      );
    }
  }
});

test('management templates expose Gin Vben Admin branding and the supplied logo', () => {
  const workspacePackage = JSON.parse(
    readFileSync(resolve(root, 'package.json'), 'utf8'),
  );
  assert.equal(workspacePackage.name, 'gin-vben-admin-monorepo');
  assert.equal(
    workspacePackage.homepage,
    'https://github.com/ByteJason/Gin-Vben-Admin',
  );
  assert.equal(
    workspacePackage.bugs,
    'https://github.com/ByteJason/Gin-Vben-Admin/issues',
  );
  assert.equal(workspacePackage.repository, 'ByteJason/Gin-Vben-Admin.git');
  assert.equal(workspacePackage.author.name, 'Gin-Vben-Admin contributors');
  assert.ok(workspacePackage.keywords.includes('gin vben admin'));

  const workspaceGenerator = readFileSync(
    resolve(root, 'scripts/vsh/src/code-workspace/index.ts'),
    'utf8',
  );
  assert.match(
    workspaceGenerator,
    /CODE_WORKSPACE_FILE = join\('gin-vben-admin\.code-workspace'\)/,
  );
  assert.doesNotMatch(
    workspaceGenerator,
    /CODE_WORKSPACE_FILE = join\('vben-admin\.code-workspace'\)/,
  );

  const productBrandFiles = [
    'internal/vite-config/src/options.ts',
    'packages/@core/preferences/src/config.ts',
    'packages/effects/layouts/src/basic/copyright/copyright.vue',
  ];

  for (const path of productBrandFiles) {
    const source = readFileSync(resolve(root, path), 'utf8');
    assert.match(source, /Gin Vben Admin/, path);
    assert.doesNotMatch(source, /(?<!Gin )Vben Admin/, path);
  }

  const about = readFileSync(
    resolve(root, 'packages/effects/common-ui/src/ui/about/about.vue'),
    'utf8',
  );
  const projectConstants = readFileSync(
    resolve(root, 'packages/@core/base/shared/src/constants/vben.ts'),
    'utf8',
  );
  assert.match(
    projectConstants,
    /GIN_VBEN_ADMIN_GITHUB_URL\s*=\s*'https:\/\/github\.com\/ByteJason\/Gin-Vben-Admin'/,
  );
  assert.match(about, /:href="GIN_VBEN_ADMIN_GITHUB_URL"/);
  assert.match(about, /title: '上游前端项目'/);
  assert.match(about, /renderLink\(VBEN_GITHUB_URL, 'Vue Vben Admin'\)/);
  const pwaOptions = readFileSync(
    resolve(root, 'internal/vite-config/src/options.ts'),
    'utf8',
  );
  const applicationConfig = readFileSync(
    resolve(root, 'internal/vite-config/src/config/application.ts'),
    'utf8',
  );
  assert.match(
    applicationConfig,
    /'Gin-Vben-Admin': 'https:\/\/github\.com\/ByteJason\/Gin-Vben-Admin'/,
  );
  assert.match(
    applicationConfig,
    /'Vue Vben Admin Docs \(upstream\)': 'https:\/\/doc\.vben\.pro'/,
  );
  for (const size of brandPwaLogoSha256.keys()) {
    assert.match(pwaOptions, new RegExp(`sizes: '${size}x${size}'`));
    assert.match(
      pwaOptions,
      new RegExp(`src: 'gin-vben-admin-logo-${size}\\.png'`),
    );
  }
  assert.doesNotMatch(pwaOptions, /static-source.*pwa-icon/);

  for (const app of requiredApps) {
    const index = readFileSync(
      resolve(root, 'apps', app, 'index.html'),
      'utf8',
    );
    const preferences = readFileSync(
      resolve(root, 'apps', app, 'src/preferences.ts'),
      'utf8',
    );
    const basicLayout = readFileSync(
      resolve(root, 'apps', app, 'src/layouts/basic.vue'),
      'utf8',
    );
    const routeModule = readFileSync(
      resolve(root, 'apps', app, 'src/router/routes/modules/vben.ts'),
      'utf8',
    );
    const englishDemos = JSON.parse(
      readFileSync(
        resolve(root, 'apps', app, 'src/locales/langs/en-US/demos.json'),
        'utf8',
      ),
    );
    const chineseDemos = JSON.parse(
      readFileSync(
        resolve(root, 'apps', app, 'src/locales/langs/zh-CN/demos.json'),
        'utf8',
      ),
    );
    const logo = readFileSync(
      resolve(root, 'apps', app, 'public', brandLogoFilename),
    );

    assert.match(
      index,
      /content="Gin Vben Admin Vue3 Vite"/,
      `${app} keywords`,
    );
    assert.match(
      index,
      new RegExp(`href="/${brandLogoFilename}"`),
      `${app} favicon`,
    );
    assert.match(preferences, /source: brandLogoUrl/, `${app} logo preference`);
    assert.equal(
      createHash('sha256').update(logo).digest('hex'),
      brandLogoSha256,
      `${app} supplied logo`,
    );
    assert.equal(
      existsSync(resolve(root, 'apps', app, 'public/favicon.ico')),
      false,
      `${app} legacy favicon`,
    );
    assert.match(basicLayout, /GIN_VBEN_ADMIN_GITHUB_URL/);
    assert.doesNotMatch(basicLayout, /VBEN_GITHUB_URL/);
    assert.match(
      basicLayout,
      /text: `Vue Vben Admin \$\{\$t\('ui\.widgets\.document'\)\}`/,
    );
    assert.match(routeModule, /path: '\/gin-vben-admin\/about'/);
    assert.match(
      routeModule,
      /path: '\/vben-admin\/about',[\s\S]*?redirect: '\/gin-vben-admin\/about'/,
    );
    assert.equal(englishDemos.vben.title, 'Vue Vben Admin (Upstream)');
    assert.equal(chineseDemos.vben.title, 'Vue Vben Admin（上游）');

    for (const size of [192, 512]) {
      const pwaIcon = readFileSync(
        resolve(root, 'apps', app, 'public', `gin-vben-admin-logo-${size}.png`),
      );
      assert.equal(pwaIcon.readUInt32BE(16), size, `${app} PWA icon width`);
      assert.equal(pwaIcon.readUInt32BE(20), size, `${app} PWA icon height`);
      assert.equal(
        createHash('sha256').update(pwaIcon).digest('hex'),
        brandPwaLogoSha256.get(size),
        `${app} PWA icon ${size}`,
      );
    }
  }
});

test('all management templates expose equivalent observability settings', () => {
  for (const app of requiredApps) {
    const viewPath = resolve(
      root,
      'apps',
      app,
      'src/views/system/observability/index.vue',
    );
    const routePath = resolve(
      root,
      'apps',
      app,
      'src/router/routes/modules/system.ts',
    );
    assert.ok(existsSync(viewPath), `${app} observability view`);
    assert.ok(existsSync(routePath), `${app} observability route`);
    const view = readFileSync(viewPath, 'utf8');
    for (const token of [
      'observability.metrics.enabled',
      'observability.tracing.enabled',
      'observability.tracing.endpoint',
      'observability.tracing.protocol',
      'observability.tracing.sample_rate',
      '<section',
      'aria-labelledby="observability-title"',
      'aria-live="polite"',
    ]) {
      assert.match(view, new RegExp(token.replaceAll('.', '\\.')));
    }
    assert.doesNotMatch(view, /<main\b/);
  }
});
