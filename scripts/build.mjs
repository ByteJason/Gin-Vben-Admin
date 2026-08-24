#!/usr/bin/env node

import { createHash } from 'node:crypto';
import {
  accessSync,
  cpSync,
  constants,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import {
  delimiter,
  dirname,
  isAbsolute,
  join,
  relative,
  resolve,
  sep,
} from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const MODES = new Set(['embedded', 'standalone', 'api_only', 'dev']);
const UIS = new Set(['antd', 'ele', 'naive', 'all']);
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

function fail(message, exitCode = 2) {
  process.stderr.write(`BUILD_ERROR=${message}\n`);
  process.exit(exitCode);
}

function buildError(message) {
  const error = new Error(message);
  error.name = 'BuildError';
  return error;
}

function parseArgs(argv) {
  const options = { check: false, mode: '', output: '', ui: '' };

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--check') {
      options.check = true;
      continue;
    }
    if (argument === '--mode' || argument === '--output' || argument === '--ui') {
      const value = argv[index + 1];
      if (!value || value.startsWith('--')) {
        fail(`missing value for ${argument}`);
      }
      options[argument.slice(2)] = value;
      index += 1;
      continue;
    }
    fail(`unknown argument: ${argument}`);
  }

  if (!MODES.has(options.mode)) {
    fail(`unsupported mode: ${options.mode || '<empty>'}`);
  }
  if (options.mode === 'api_only') {
    if (options.ui && !UIS.has(options.ui)) {
      fail(`unsupported ui: ${options.ui}`);
    }
    options.ui = 'none';
  } else if (!UIS.has(options.ui)) {
    fail(`unsupported ui: ${options.ui || '<empty>'}`);
  }
  if (options.ui === 'all' && options.mode !== 'embedded') {
    fail('ui all is only valid for embedded mode');
  }
  if (options.output && options.mode !== 'api_only') {
    fail('--output is only valid for api_only mode');
  }
  if (!options.check && !['api_only', 'embedded', 'standalone'].includes(options.mode)) {
    fail(`build execution is not available yet for mode: ${options.mode}`);
  }

  return options;
}

function boundedApiOutput(value) {
  const outputRoot = join(root, 'server', 'bin');
  const defaultName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';
  const output = resolve(root, value || join('server', 'bin', defaultName));
  const child = relative(outputRoot, output);
  if (!child || child.startsWith('..') || isAbsolute(child)) {
    fail('output must be a file below server/bin');
  }
  return output;
}

function runCommand(command, args) {
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: 'utf8',
    env: process.env,
    shell: false,
  });
  if (result.status !== 0) {
    process.stderr.write(result.stdout || '');
    process.stderr.write(result.stderr || '');
    throw buildError(`${command} failed with status ${result.status ?? 'unknown'}`);
  }
}

function runGoBuild(output, tags = []) {
  const runtimeRoot = join(root, '.runtime');
  const goCache = join(runtimeRoot, 'go-cache');
  const goTmp = join(runtimeRoot, 'go-tmp');
  mkdirSync(dirname(output), { recursive: true });
  mkdirSync(goCache, { recursive: true });
  mkdirSync(goTmp, { recursive: true });

  const outputFromServer = relative(join(root, 'server'), output);
  const args = ['-C', 'server', 'build', '-trimpath'];
  if (tags.length > 0) {
    args.push('-tags', tags.join(','));
  }
  args.push('-o', outputFromServer, './cmd/api');
  const result = spawnSync(
    'go',
    args,
    {
      cwd: root,
      encoding: 'utf8',
      env: {
        ...process.env,
        GOCACHE: process.env.GOCACHE || goCache,
        GOTMPDIR: process.env.GOTMPDIR || goTmp,
      },
      shell: false,
    },
  );
  if (result.status !== 0) {
    process.stderr.write(result.stdout || '');
    process.stderr.write(result.stderr || '');
    rmSync(output, { force: true });
    throw buildError(`go build failed with status ${result.status ?? 'unknown'}`);
  }

  return createHash('sha256').update(readFileSync(output)).digest('hex');
}

function runApiOnlyBuild(outputValue) {
  const output = boundedApiOutput(outputValue);
  const digest = runGoBuild(output);

  process.stdout.write(`BUILD_ARTIFACT=${relative(root, output)}\n`);
  process.stdout.write(`BUILD_SHA256=${digest}\n`);
  process.stdout.write('BUILD_OK\n');
}

function portablePath(value) {
  return value.split(sep).join('/');
}

function fileManifest(directory) {
  const files = {};
  const walk = (current) => {
    for (const entry of readdirSync(current, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const absolute = join(current, entry.name);
      if (entry.isSymbolicLink()) {
        throw buildError(`generated artifact contains a symbolic link: ${portablePath(relative(directory, absolute))}`);
      }
      if (entry.isDirectory()) {
        walk(absolute);
        continue;
      }
      if (!entry.isFile()) {
        throw buildError(`generated artifact contains an unsupported entry: ${portablePath(relative(directory, absolute))}`);
      }
      const name = portablePath(relative(directory, absolute));
      files[name] = createHash('sha256').update(readFileSync(absolute)).digest('hex');
    }
  };
  walk(directory);
  return files;
}

function beginDirectorySwap(staging, output, backup) {
  rmSync(backup, { force: true, recursive: true });
  let previousMoved = false;
  try {
    if (existsSync(output)) {
      renameSync(output, backup);
      previousMoved = true;
    }
    renameSync(staging, output);
  } catch (error) {
    if (!existsSync(output) && previousMoved && existsSync(backup)) {
      renameSync(backup, output);
    }
    throw error;
  }
  return previousMoved;
}

function rollbackDirectorySwap(output, backup, previousMoved) {
  rmSync(output, { force: true, recursive: true });
  if (previousMoved && existsSync(backup)) {
    renameSync(backup, output);
  } else {
    rmSync(backup, { force: true, recursive: true });
  }
}

function publishDirectory(staging, output, backup) {
  beginDirectorySwap(staging, output, backup);
  rmSync(backup, { force: true, recursive: true });
}

function selectedUis(ui) {
  return ui === 'all' ? ['antd', 'ele', 'naive'] : [ui];
}

function buildFrontends(ui) {
  runCommand('pnpm', ['--dir', 'admin', '--filter', '@gin-vben-admin/install', 'run', 'build:installer']);
  for (const selected of selectedUis(ui)) {
    runCommand('pnpm', ['--dir', 'admin', '--filter', `@vben/web-${selected}`, 'run', 'build']);
  }
}

function runStandaloneBuild(ui) {
  const buildRoot = join(root, '.runtime', 'build');
  const output = join(buildRoot, 'standalone');
  const staging = join(buildRoot, `.standalone-staging-${process.pid}`);
  const backup = join(buildRoot, `.standalone-backup-${process.pid}`);
  const binaryName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';

  rmSync(staging, { force: true, recursive: true });
  mkdirSync(staging, { recursive: true });

  try {
    buildFrontends(ui);

    const uiDist = join(root, 'admin', 'apps', `web-${ui}`, 'dist');
    const installDist = join(root, 'admin', 'apps', 'install', 'dist');
    if (!statSync(uiDist).isDirectory() || !statSync(installDist).isDirectory()) {
      throw buildError('frontend build did not produce the expected dist directories');
    }
    cpSync(uiDist, join(staging, 'html'), { recursive: true });
    cpSync(installDist, join(staging, 'html', 'install'), { recursive: true });
    cpSync(join(root, 'admin', 'nginx.conf'), join(staging, 'nginx.conf'));
    runGoBuild(join(staging, binaryName));

    const manifest = {
      schema: 1,
      mode: 'standalone',
      ui,
      files: fileManifest(staging),
    };
    writeFileSync(
      join(staging, 'artifact-manifest.json'),
      `${JSON.stringify(manifest, null, 2)}\n`,
      { mode: 0o644 },
    );
    publishDirectory(staging, output, backup);
  } catch (error) {
    rmSync(staging, { force: true, recursive: true });
    throw error;
  }

  process.stdout.write(`BUILD_ARTIFACT=${portablePath(relative(root, output))}\n`);
  process.stdout.write('BUILD_MANIFEST=artifact-manifest.json\n');
  process.stdout.write('BUILD_OK\n');
}

function runEmbeddedBuild(ui) {
  const buildRoot = join(root, '.runtime', 'build');
  const artifactOutput = join(buildRoot, 'embedded');
  const artifactStaging = join(buildRoot, `.embedded-staging-${process.pid}`);
  const artifactBackup = join(buildRoot, `.embedded-backup-${process.pid}`);
  const assetRoot = join(root, 'server', 'internal', 'platform', 'webassets');
  const assetOutput = join(assetRoot, 'dist');
  const assetStaging = join(assetRoot, `.dist-staging-${process.pid}`);
  const assetBackup = join(assetRoot, `.dist-backup-${process.pid}`);
  const binaryName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';
  let assetsPublished = false;
  let previousAssetsMoved = false;

  rmSync(artifactStaging, { force: true, recursive: true });
  rmSync(assetStaging, { force: true, recursive: true });
  mkdirSync(artifactStaging, { recursive: true });
  mkdirSync(assetStaging, { recursive: true });

  try {
    buildFrontends(ui);
    const installDist = join(root, 'admin', 'apps', 'install', 'dist');
    if (!statSync(installDist).isDirectory()) {
      throw buildError('installer build did not produce its dist directory');
    }
    cpSync(installDist, join(assetStaging, 'install'), { recursive: true });

    for (const selected of selectedUis(ui)) {
      const uiDist = join(root, 'admin', 'apps', `web-${selected}`, 'dist');
      if (!statSync(uiDist).isDirectory()) {
        throw buildError(`management UI build did not produce dist: ${selected}`);
      }
      cpSync(uiDist, join(assetStaging, 'admin', selected), { recursive: true });
    }

    const assetManifest = {
      schema: 1,
      mode: 'embedded',
      ui,
      files: fileManifest(assetStaging),
    };
    writeFileSync(
      join(assetStaging, 'asset-manifest.json'),
      `${JSON.stringify(assetManifest, null, 2)}\n`,
      { mode: 0o644 },
    );

    previousAssetsMoved = beginDirectorySwap(assetStaging, assetOutput, assetBackup);
    assetsPublished = true;
    runGoBuild(join(artifactStaging, binaryName), ['embed']);
    cpSync(
      join(assetOutput, 'asset-manifest.json'),
      join(artifactStaging, 'asset-manifest.json'),
    );
    const artifactManifest = {
      schema: 1,
      mode: 'embedded',
      ui,
      files: fileManifest(artifactStaging),
    };
    writeFileSync(
      join(artifactStaging, 'artifact-manifest.json'),
      `${JSON.stringify(artifactManifest, null, 2)}\n`,
      { mode: 0o644 },
    );
    publishDirectory(artifactStaging, artifactOutput, artifactBackup);
    rmSync(assetBackup, { force: true, recursive: true });
  } catch (error) {
    rmSync(artifactStaging, { force: true, recursive: true });
    rmSync(assetStaging, { force: true, recursive: true });
    if (assetsPublished) {
      rollbackDirectorySwap(assetOutput, assetBackup, previousAssetsMoved);
    }
    throw error;
  }

  process.stdout.write(`BUILD_ARTIFACT=${portablePath(relative(root, artifactOutput))}\n`);
  process.stdout.write(`BUILD_ASSETS=${portablePath(relative(root, assetOutput))}\n`);
  process.stdout.write('BUILD_MANIFEST=artifact-manifest.json\n');
  process.stdout.write('BUILD_OK\n');
}

function requirePath(relativePath) {
  const absolutePath = join(root, relativePath);
  try {
    accessSync(absolutePath, constants.R_OK);
  } catch {
    fail(`required path is not readable: ${relativePath}`, 1);
  }
}

function commandExists(command) {
  const pathValue = process.env.PATH ?? '';
  const extensions = process.platform === 'win32'
    ? (process.env.PATHEXT ?? '.EXE;.CMD;.BAT;.COM').split(';')
    : [''];

  for (const directory of pathValue.split(delimiter).filter(Boolean)) {
    for (const extension of extensions) {
      try {
        accessSync(join(directory, `${command}${extension}`), constants.X_OK);
        return true;
      } catch {
        // Continue through the bounded PATH candidates.
      }
    }
  }
  return false;
}

function validate(options) {
  requirePath('server/go.mod');
  if (!commandExists('go')) {
    fail('required command is not executable: go', 1);
  }

  if (options.mode !== 'api_only') {
    requirePath('admin/apps/install/package.json');
    if (!commandExists('pnpm')) {
      fail('required command is not executable: pnpm', 1);
    }

    for (const ui of selectedUis(options.ui)) {
      requirePath(`admin/apps/web-${ui}/package.json`);
    }
  }
}

const options = parseArgs(process.argv.slice(2));
validate(options);

process.stdout.write(`BUILD_MODE=${options.mode}\n`);
process.stdout.write(`BUILD_UI=${options.ui}\n`);
process.stdout.write(`BUILD_PLATFORM=${process.platform}\n`);
try {
  if (options.check) {
    process.stdout.write('BUILD_CHECK_OK\n');
  } else {
    if (options.mode === 'api_only') {
      runApiOnlyBuild(options.output);
    } else if (options.mode === 'embedded') {
      runEmbeddedBuild(options.ui);
    } else {
      runStandaloneBuild(options.ui);
    }
  }
} catch (error) {
  process.stderr.write(`BUILD_ERROR=${error instanceof Error ? error.message : 'unknown build failure'}\n`);
  process.exit(1);
}
