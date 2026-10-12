"""Offline additive RC3 migration with a WAL-aware backup and row verification.

Stop runq serve first. This script never reinterprets historical started_at,
changes task IDs/attempts, or rewrites project configuration.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import sqlite3


def quote(name):
    return '"' + name.replace('"', '""') + '"'


def snapshot(db, columns):
    hashes = {}
    counts = {}
    for table, cols in columns.items():
        rows = db.execute(f'SELECT {",".join(map(quote, cols))} FROM {quote(table)}').fetchall()
        # Include type/bytes; stable ordering works for WITHOUT ROWID tables too.
        encoded = sorted(repr(tuple(row)).encode() for row in rows)
        digest = hashlib.sha256()
        for row in encoded:
            digest.update(len(row).to_bytes(8, "big"))
            digest.update(row)
        hashes[table] = digest.hexdigest()
        counts[table] = len(rows)
    return {"counts": counts, "sha256": hashes}


def migrate(db_path, backup_dir):
    db_path, backup_dir = Path(db_path).resolve(), Path(backup_dir).resolve()
    backup_dir.mkdir(parents=True, exist_ok=False)
    backup_path = backup_dir / "runq-before.db"
    db = sqlite3.connect(f"file:{db_path}?mode=rw", uri=True, isolation_level=None)
    try:
        with sqlite3.connect(backup_path) as backup:
            db.backup(backup)
            if backup.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
                raise RuntimeError("backup integrity check failed")
        tables = [r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")]
        columns = {table: [r[1] for r in db.execute(f"PRAGMA table_info({quote(table)})")]
                   for table in tables}
        db.execute("BEGIN IMMEDIATE")
        try:
            before = snapshot(db, columns)
            added = "timing_json" not in columns["tasks"]
            if added:
                db.execute("ALTER TABLE tasks ADD COLUMN timing_json TEXT")
            after = snapshot(db, columns)
            if before != after:
                raise RuntimeError("an existing row or column changed; rolling back")
            if db.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
                raise RuntimeError("migrated DB integrity check failed")
            db.execute("COMMIT")
        except BaseException:
            db.execute("ROLLBACK")
            raise
        manifest = {"database": str(db_path), "backup": str(backup_path),
                    "added_timing_json": added, "existing_data_unchanged": True,
                    "before": before, "after": after}
        (backup_dir / "manifest.json").write_text(json.dumps(manifest, indent=2))
        print(json.dumps({k: v for k, v in manifest.items() if k not in ("before", "after")}, indent=2))
        return manifest
    finally:
        db.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", required=True)
    parser.add_argument("--backup-dir", required=True)
    args = parser.parse_args()
    migrate(args.db, args.backup_dir)
