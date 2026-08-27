import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const root = join(import.meta.dirname, "..", "..");
const runner = join(root, "scripts", "v010-closeout.mjs");

test("B10.8 closeout runner exists and keeps the 0.10 scope bounded", () => {
  assert.equal(existsSync(runner), true, runner);
  const source = readFileSync(runner, "utf8");
  for (const token of [
    "0.10.0-dev",
    "migration-assets",
    "installer-retry-rollback",
    "performance-contract",
    "PASS_WITH_CONDITIONS",
    "MIGRATION_SMOKE_INTEGRATION",
    "confirmRollback",
    "server/migrations/schema.go",
    "server/internal/platform/persistence/model/registry.go",
    "server/internal/platform/persistence/model/relations.go",
    "CreateSchema",
  ]) {
    assert.match(source, new RegExp(token.replace(/[.-]/g, "\\$&")), token);
  }
  assert.doesNotMatch(source, /1\.0\.0.*implement/i);
  assert.equal(
    existsSync(join(root, "server/internal/platform/persistence/model/registry.go")),
    true,
    "persistence model registry",
  );
});

test("B10.8 closeout runner reports migration, installer and perf checks", () => {
  const result = spawnSync(process.execPath, [runner, "--check"], {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, MIGRATION_SMOKE_INTEGRATION: "" },
  });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  for (const token of [
    "V010_CLOSEOUT_VERSION=0.10.0-dev",
    "V010_CLOSEOUT_CHECK migration-assets=PASS",
    "V010_CLOSEOUT_CHECK installer-retry-rollback=PASS",
    "V010_CLOSEOUT_CHECK performance-contract=PASS",
    "V010_CLOSEOUT_STATUS=PASS_WITH_CONDITIONS",
  ]) {
    assert.match(result.stdout, new RegExp(token.replace(/[.-]/g, "\\$&")), token);
  }
});
