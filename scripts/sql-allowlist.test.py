#!/usr/bin/env python3
"""Unit tests for check-sql-allowlist.py (stdlib only)."""

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-sql-allowlist.py")
SPEC = importlib.util.spec_from_file_location("sql_allowlist", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class SQLAllowlistTest(unittest.TestCase):
    def test_allowlisted_line_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "server/internal/platform/tasks/gorm_repository.go"
            source.parent.mkdir(parents=True)
            source.write_text('rows := gorm.G[row](db).Raw(sql).Find(ctx)\n', encoding="utf-8")
            rules = [{"path": "server/internal/platform/tasks/*.go", "line_regex": r"\.Raw\(sql"}]
            self.assertEqual(MODULE.scan(root, rules), [])

    def test_unreviewed_line_is_reported(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "server/internal/platform/tasks/gorm_repository.go"
            source.parent.mkdir(parents=True)
            source.write_text('db.Table("users").Find(&rows)\n', encoding="utf-8")
            self.assertEqual(len(MODULE.scan(root, [])), 1)


if __name__ == "__main__":
    unittest.main()
