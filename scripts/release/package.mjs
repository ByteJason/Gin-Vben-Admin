#!/usr/bin/env node

/**
 * Build the local RC package boundary. `--check` is intentionally offline and
 * deterministic. `--build` may cross-compile API/embedded binaries into a
 * local archive directory; registry publishing and signing are separate
 * operations and are rejected until their identities are configured.
 */
import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import {
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const VERSION = '0.9.0-rc';
const TARGETS = [
  { os: 'linux', arch: 'amd64' },
  { os: 'linux', arch: 'arm64' },
  { os: 'darwin', arch: 'amd64' },
  { os: 'darwin', arch: 'arm64' },
  { os: 'windows', arch: 'amd64' },
];
const args = process.argv.slice(2);

function has(name) { return args.includes(name); }
function value(name, fallback = '') {
  const index = args.indexOf(name);
  return index >= 0 ? (args[index + 1] ?? fallback) : fallback;
}
function fail(message) {
  process.stderr.write(`RELEASE_PACKAGE_ERROR=${message}\n`);
  process.exit(2);
}
function canonical(data) { return `${JSON.stringify(data, null, 2)}\n`; }
function sha256(buffer) { return createHash('sha256').update(buffer).digest('hex'); }

function sourceCommit() {
  const result = spawnSync('git', ['rev-parse', 'HEAD'], { cwd: ROOT, encoding: 'utf8' });
  return result.status === 0 ? result.stdout.trim() : 'unknown';
}

function targetMatrix() {
  return TARGETS.flatMap((target) => [
    { ...target, mode: 'api_only', artifact: `gin-vben-admin-${target.os}-${target.arch}-api-only.tar.gz` },
    { ...target, mode: 'embedded', artifact: `gin-vben-admin-${target.os}-${target.arch}-embedded.tar.gz` },
  ]);
}

function baseReport() {
  return {
    schema: 1,
    version: VERSION,
    registryPublish: false,
    signed: false,
    targets: targetMatrix(),
    artifactModes: ['api_only', 'embedded', 'standalone'],
    checksums: { algorithm: 'sha256', manifest: 'SHA256SUMS' },
    provenance: {
      builder: 'local',
      sourceCommit: sourceCommit(),
      registryPublish: false,
      signed: false,
      signingIdentity: null,
    },
    policy: {
      standaloneFormalPlatforms: ['linux/amd64', 'linux/arm64'],
      remoteRegistry: 'pending-user-registry-and-oidc-identity',
    },
  };
}

function parseOptions() {
  if (has('--publish') || has('--sign')) fail('remote registry publishing/signing is not enabled for local RC artifacts');
  const mode = value('--mode', 'api_only');
  if (!['api_only', 'embedded'].includes(mode)) fail(`local build mode must be api_only or embedded, got ${mode}`);
  const ui = value('--ui', 'antd');
  if (!['antd', 'ele', 'naive', 'all'].includes(ui)) fail(`unsupported ui: ${ui}`);
  return { check: has('--check'), build: has('--build'), mode, ui, output: resolve(ROOT, value('--output', '.runtime/release')) };
}

function collectFiles(directory) {
  const files = [];
  const walk = (current) => {
    for (const entry of readdirSync(current, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const absolute = join(current, entry.name);
      if (entry.isDirectory()) walk(absolute);
      else if (entry.isFile()) files.push(absolute);
    }
  };
  walk(directory);
  return files;
}

function tarHeader(name, size, mode = 0o644) {
  const header = Buffer.alloc(512, 0);
  const write = (offset, length, text) => Buffer.from(String(text)).copy(header, offset, 0, length);
  write(0, 100, name);
  write(100, 8, `${mode.toString(8).padStart(7, '0')}\0`);
  write(108, 8, '0000000\0');
  write(116, 8, '0000000\0');
  write(124, 12, `${size.toString(8).padStart(11, '0')}\0`);
  write(136, 12, '00000000000\0');
  header.fill(0x20, 148, 156);
  write(156, 1, '0');
  write(257, 6, 'ustar\0');
  write(263, 2, '00');
  const sum = [...header].reduce((total, byte) => total + byte, 0);
  write(148, 8, `${sum.toString(8).padStart(6, '0')}\0 `);
  return header;
}

function deterministicArchive(directory, archivePath) {
  const chunks = [];
  for (const absolute of collectFiles(directory)) {
    const name = relative(directory, absolute).split(/\\/g).join('/');
    const content = readFileSync(absolute);
    chunks.push(tarHeader(name, content.length, statSync(absolute).mode & 0o777));
    chunks.push(content);
    const padding = (512 - (content.length % 512)) % 512;
    if (padding) chunks.push(Buffer.alloc(padding));
  }
  chunks.push(Buffer.alloc(1024));
  const compressed = gzipSync(Buffer.concat(chunks), { level: 9, mtime: 0 });
  writeFileSync(archivePath, compressed, { mode: 0o644 });
  return sha256(compressed);
}

function goBuild(target, mode, output) {
  const env = {
    ...process.env,
    GOOS: target.os,
    GOARCH: target.arch,
    CGO_ENABLED: '0',
    GOCACHE: process.env.GOCACHE || join(ROOT, '.runtime', 'go-cache'),
    GOTMPDIR: process.env.GOTMPDIR || join(ROOT, '.runtime', 'go-tmp'),
  };
  mkdirSync(dirname(output), { recursive: true });
  const buildArgs = ['-C', 'server', 'build', '-trimpath'];
  if (mode === 'embedded') buildArgs.push('-tags', 'embed');
  buildArgs.push('-o', relative(join(ROOT, 'server'), output), './cmd/api');
  const result = spawnSync('go', buildArgs, { cwd: ROOT, env, encoding: 'utf8' });
  if (result.status !== 0) throw new Error(`go build ${target.os}/${target.arch} failed: ${result.stderr || result.stdout}`);
}

function buildArtifacts(options, report) {
  mkdirSync(options.output, { recursive: true });
  const stagingRoot = join(options.output, `.staging-${process.pid}`);
  rmSync(stagingRoot, { recursive: true, force: true });
  mkdirSync(stagingRoot, { recursive: true });
  const artifacts = [];
  try {
    const targets = options.mode === 'embedded' ? TARGETS : TARGETS;
    for (const target of targets) {
      const packageRoot = join(stagingRoot, `${target.os}-${target.arch}`);
      mkdirSync(packageRoot, { recursive: true });
      const binaryName = target.os === 'windows' ? 'server-api.exe' : 'server-api';
      goBuild(target, options.mode, join(packageRoot, 'bin', binaryName));
      cpSync(join(ROOT, 'LICENSE'), join(packageRoot, 'LICENSE'));
      cpSync(join(ROOT, 'NOTICE'), join(packageRoot, 'NOTICE'));
      cpSync(join(ROOT, 'server', 'configs', 'server.example.yaml'), join(packageRoot, 'server.example.yaml'));
      const archive = join(options.output, `gin-vben-admin-${target.os}-${target.arch}-${options.mode}.tar.gz`);
      const digest = deterministicArchive(packageRoot, archive);
      artifacts.push({ mode: options.mode, os: target.os, arch: target.arch, path: relative(ROOT, archive).replaceAll('\\', '/'), sha256: digest });
    }
    const checksums = artifacts.map((item) => `${item.sha256}  ${item.path}`).join('\n') + '\n';
    writeFileSync(join(options.output, 'SHA256SUMS'), checksums, { mode: 0o644 });
    report.artifacts = artifacts;
    report.provenance.artifacts = artifacts.map(({ path, sha256: digest }) => ({ path, sha256: digest }));
    writeFileSync(join(options.output, 'provenance.json'), canonical(report.provenance), { mode: 0o644 });
    writeFileSync(join(options.output, 'release-manifest.json'), canonical(report), { mode: 0o644 });
  } finally {
    rmSync(stagingRoot, { recursive: true, force: true });
  }
}

const options = parseOptions();
const report = baseReport();
if (options.check) {
  const existingPath = join(options.output, 'release-manifest.json');
  if (existsSync(existingPath)) {
    const existing = JSON.parse(readFileSync(existingPath, 'utf8'));
    if (canonical(existing) !== canonical(report)) fail('release manifest drift; rebuild local artifacts');
  }
  process.stdout.write(canonical(report));
} else if (options.build) {
  buildArtifacts(options, report);
  process.stdout.write(canonical(report));
} else {
  process.stdout.write(canonical(report));
}
