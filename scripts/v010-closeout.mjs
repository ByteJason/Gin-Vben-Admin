#!/usr/bin/env node

import { spawnSync } from "node:child_process";
/**
 * 0.10.0-dev version-closeout contract.
 *
 * The default check is offline and deterministic. It verifies the single
 * fresh-install GORM registry and persistence models (with both supported drivers), installer retry/rollback surface, and the existing DEC-025
 * performance contract without opening sockets or mutating a database. A real
 * migration rehearsal remains opt-in through MIGRATION_SMOKE_INTEGRATION=1 and
 * --integration, matching the repository's isolated-loopback guard.
 */
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const VERSION = "0.10.0-dev";
const args = process.argv.slice(2);
const integration =
  process.env.MIGRATION_SMOKE_INTEGRATION === "1" && args.includes("--integration");
const outputIndex = args.indexOf("--output");
const outputPath = outputIndex >= 0 ? args[outputIndex + 1] : "";

function run(command, commandArgs, options = {}) {
  const runtimeRoot = join(ROOT, ".runtime");
  const goCache = join(runtimeRoot, "go-cache");
  const goTmp = join(runtimeRoot, "go-tmp");
  mkdirSync(goCache, { recursive: true });
  mkdirSync(goTmp, { recursive: true });
  return spawnSync(command, commandArgs, {
    cwd: ROOT,
    encoding: "utf8",
    timeout: options.timeout ?? 180_000,
    env: {
      ...process.env,
      GOCACHE: process.env.GOCACHE ?? goCache,
      GOTMPDIR: process.env.GOTMPDIR ?? goTmp,
      ...(options.env ?? {}),
    },
  });
}

function source(path) {
  return readFileSync(join(ROOT, path), "utf8");
}

function migrationAssets() {
  const drivers = ["mysql", "postgres"];
  const modelFiles = [
    "server/internal/platform/persistence/model/types.go",
    "server/internal/platform/persistence/model/shared_models.go",
    "server/internal/platform/persistence/model/identity_models.go",
    "server/internal/platform/persistence/model/admin_iam_models.go",
    "server/internal/platform/persistence/model/audit_models.go",
    "server/internal/platform/persistence/model/admin_settings_models.go",
    "server/internal/platform/persistence/model/admin_file_models.go",
    "server/internal/platform/persistence/model/admin_mail_models.go",
    "server/internal/platform/persistence/model/admin_dictionary_models.go",
    "server/internal/platform/persistence/model/admin_tasks_models.go",
    "server/internal/platform/persistence/model/admin_importexport_models.go",
    "server/internal/platform/persistence/model/registry.go",
    "server/internal/platform/persistence/model/comments.go",
    "server/internal/platform/persistence/model/relations.go",
  ];
  const files = ["server/migrations/schema.go", ...modelFiles];
  for (const file of files) {
    if (!existsSync(join(ROOT, file))) throw new Error(`missing ${file}`);
  }
  const schema = source("server/migrations/schema.go");
  const models = modelFiles.map(source).join("\n");
  for (const token of ["CreateSchema", "DropSchema", "CreateTable", "TableNames", "Models"]) {
    if (!schema.includes(token)) throw new Error(`GORM schema is missing ${token}`);
  }
  for (const token of ["type User struct", "type TaskDefinition struct", "func All()", "func Relations()", "comment:"]) {
    if (!models.includes(token)) throw new Error(`persistence model is missing ${token}`);
  }
  const sourceWithoutComments = `${schema}\n${models}`
    .replace(/\/\/.*$/gm, "")
    .replace(/\/\*[\s\S]*?\*\//g, "");
  if (/FOREIGN\s+KEY|REFERENCES\s+/i.test(sourceWithoutComments)) {
    throw new Error("GORM schema must not declare foreign-key constraints");
  }
  const migrationTests = run("go", [
    "-C",
    "server",
    "test",
    "./internal/platform/persistence/model",
    "./migrations",
    "-count=1",
  ]);
  if (migrationTests.status !== 0) {
    throw new Error("GORM schema contract tests failed");
  }
  return { drivers, files, latest: "000001" };
}

function installerRetryRollback() {
  const openapi = source("contracts/openapi/install-v1.yaml");
  const client = source("admin/apps/install/src/app.js");
  const smoke = run(process.execPath, ["admin/apps/install/tests/smoke.test.mjs"]);
  if (smoke.status !== 0) {
    throw new Error(`installer smoke failed (${smoke.status ?? 1})`);
  }
  const installerTests = run("go", [
    "-C",
    "server",
    "test",
    "./internal/application/installer",
    "./internal/transport/http/install",
    "-count=1",
  ]);
  if (installerTests.status !== 0) {
    throw new Error("installer retry/rollback tests failed");
  }
  for (const token of [
    "/api/system/install/v1/retry",
    "/api/system/install/v1/rollback",
    "confirmRollback",
    "canRetry",
    "canRollback",
  ]) {
    if (!openapi.includes(token) && !client.includes(token)) {
      throw new Error(`installer retry/rollback token missing: ${token}`);
    }
  }
  return { smoke: "PASS", retry: "PASS", rollback: "PASS" };
}

function performanceContract() {
  const workspace = mkdtempSync(join(tmpdir(), "gin-vben-admin-v010-perf-"));
  const output = join(workspace, "baseline.json");
  try {
    const generated = run(process.execPath, ["scripts/perf-baseline.mjs", "--output", output]);
    if (generated.status !== 0) throw new Error("performance baseline generation failed");
    const report = JSON.parse(readFileSync(output, "utf8"));
    if (report.acceptance?.production_capacity_claim !== false) {
      throw new Error("performance contract must not claim production capacity");
    }
    const checked = run(process.execPath, [
      "scripts/perf-baseline.mjs",
      "--check",
      "--output",
      output,
    ]);
    if (checked.status !== 0) throw new Error("performance baseline drift check failed");
    return {
      claim: false,
      fixtures: report.fixtures,
      version: report.version,
    };
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
}

function migrationSmoke() {
  const smoke = run(process.execPath, ["scripts/migration-smoke.mjs", "--check"]);
  if (smoke.status !== 0) throw new Error("migration smoke contract failed");
  const output = `${smoke.stdout ?? ""}${smoke.stderr ?? ""}`;
  if (!output.includes("MIGRATION_SMOKE_STATUS=OK")) {
    throw new Error("migration smoke did not report OK");
  }
  if (integration) {
    const rehearsal = run(process.execPath, ["scripts/migration-smoke.mjs", "--integration"]);
    if (rehearsal.status !== 0) {
      throw new Error("opt-in dual database rehearsal failed");
    }
    return { mode: "integration", status: "PASS" };
  }
  return { mode: "contract-only", status: "CONDITIONAL" };
}

function report(checks) {
  return {
    schema: 1,
    version: VERSION,
    mode: integration ? "integration" : "offline-check",
    migration_latest: checks["migration-assets"].latest,
    checks,
    status: integration ? "PASS" : "PASS_WITH_CONDITIONS",
    conditions: integration
      ? []
      : ["MySQL/PostgreSQL live DSNs were not supplied; offline contracts passed"],
  };
}

function main() {
  process.stdout.write(`V010_CLOSEOUT_VERSION=${VERSION}\n`);
  process.stdout.write(`V010_CLOSEOUT_MODE=${integration ? "integration" : "check"}\n`);
  const checks = {};
  try {
    checks["migration-assets"] = migrationAssets();
    process.stdout.write("V010_CLOSEOUT_CHECK migration-assets=PASS\n");
    checks["installer-retry-rollback"] = installerRetryRollback();
    process.stdout.write("V010_CLOSEOUT_CHECK installer-retry-rollback=PASS\n");
    checks["performance-contract"] = performanceContract();
    process.stdout.write("V010_CLOSEOUT_CHECK performance-contract=PASS\n");
    checks["migration-smoke"] = migrationSmoke();
    process.stdout.write(
      `V010_CLOSEOUT_CHECK migration-smoke=${checks["migration-smoke"].status === "PASS" ? "PASS" : "CONDITIONAL"}\n`,
    );
    const final = report(checks);
    if (outputPath) {
      const absolute = resolve(ROOT, outputPath);
      writeFileSync(absolute, `${JSON.stringify(final, null, 2)}\n`, {
        mode: 0o600,
      });
      process.stdout.write(`V010_CLOSEOUT_REPORT=${absolute}\n`);
    }
    process.stdout.write(`V010_CLOSEOUT_STATUS=${final.status}\n`);
  } catch (error) {
    process.stderr.write(`V010_CLOSEOUT_ERROR=${error.message}\n`);
    process.stdout.write("V010_CLOSEOUT_STATUS=FAIL\n");
    process.exitCode = 1;
  }
}

main();
