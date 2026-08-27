#!/usr/bin/env python3
"""Fail when production Go code introduces an unreviewed SQL escape hatch.

The scanner intentionally checks source text instead of generated SQL.  Normal
CRUD should use gorm.G[T]; only the fixed projections and migration comments
listed in docs/sql-allowlist.json may call Raw/Exec/Table/Joins/Model directly.
"""

from __future__ import annotations

import argparse
import fnmatch
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path


CALL_RE = re.compile(r"\.(?:Raw|Exec|Table|Joins|Model)\s*\(")


@dataclass(frozen=True)
class Violation:
    path: str
    line: int
    text: str


def load_rules(path: Path) -> list[dict[str, str]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("version") != 1:
        raise ValueError(f"unsupported allowlist version: {payload.get('version')!r}")
    rules = payload.get("rules")
    if not isinstance(rules, list):
        raise ValueError("allowlist rules must be a list")
    required = {"id", "path", "line_regex", "reason", "test"}
    for rule in rules:
        missing = required.difference(rule)
        if missing:
            raise ValueError(f"rule missing {sorted(missing)}: {rule!r}")
        re.compile(rule["line_regex"])
    return rules


def _allowed(rel: str, line: str, rules: list[dict[str, str]]) -> bool:
    return any(
        fnmatch.fnmatch(rel, rule["path"])
        and re.search(rule["line_regex"], line)
        for rule in rules
    )


def scan(root: Path, rules: list[dict[str, str]]) -> list[Violation]:
    violations: list[Violation] = []
    roots = [root / "server" / "internal" / "platform", root / "server" / "migrations"]
    for scan_root in roots:
        if not scan_root.exists():
            continue
        for source in sorted(scan_root.rglob("*.go")):
            if source.name.endswith("_test.go"):
                continue
            rel = source.relative_to(root).as_posix()
            for number, line in enumerate(source.read_text(encoding="utf-8").splitlines(), 1):
                if CALL_RE.search(line) and not _allowed(rel, line, rules):
                    violations.append(Violation(rel, number, line.strip()))
    return violations


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--allowlist", type=Path, default=None)
    args = parser.parse_args(argv)
    root = args.root.resolve()
    allowlist = (args.allowlist or root / "docs" / "sql-allowlist.json").resolve()
    try:
        rules = load_rules(allowlist)
        violations = scan(root, rules)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"sql-allowlist: configuration error: {exc}", file=sys.stderr)
        return 2
    if violations:
        print("sql-allowlist: unreviewed SQL calls:")
        for item in violations:
            print(f"  {item.path}:{item.line}: {item.text}")
        print("Add a narrowly-scoped rule with ID, reason, and test to docs/sql-allowlist.json.")
        return 1
    print(f"sql-allowlist: PASS ({len(rules)} rules, production source clean)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
