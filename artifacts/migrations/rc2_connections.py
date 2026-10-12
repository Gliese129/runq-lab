#!/usr/bin/env python3
"""Explicit, reversible local cache migration. Run after stopping runq serve.

Usage: python3 rc2_connections.py DATA_DIR CONFIG_FILE BACKUP_DIR
Only project/job config JSON is normalized. Execution records stay untouched.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import sys


def digest(rows):
    return hashlib.sha256(json.dumps(rows, ensure_ascii=False, separators=(",", ":"), default=str).encode()).hexdigest()


def fingerprints(db):
    out = {}
    for (table,) in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"):
        columns = [r[1] for r in db.execute(f'PRAGMA table_info("{table}")')]
        # Only these two columns are authorized for normalization.
        if table in ("projects", "jobs"):
            columns.remove("config_json")
        quoted = ",".join('"' + col.replace('"', '""') + '"' for col in columns)
        rows = list(db.execute(f'SELECT {quoted} FROM "{table}" ORDER BY rowid'))
        out[table] = {"count": len(rows), "sha256": digest(rows)}
    return out


def migrate(data_dir, config_file, backup_dir):
    backup_dir.mkdir(parents=True, exist_ok=False)
    os.chmod(backup_dir, 0o700)
    source = data_dir / "runq.db"
    db = sqlite3.connect(source, timeout=10)
    db.execute("PRAGMA foreign_keys=ON")
    if db.execute("PRAGMA quick_check").fetchone()[0] != "ok":
        raise RuntimeError("source integrity check failed")
    before = fingerprints(db)
    backup_file = backup_dir / "runq-before.db"
    backup = sqlite3.connect(backup_file)
    db.backup(backup)  # Includes committed WAL records.
    assert backup.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
    backup.close()
    os.chmod(backup_file, 0o600)
    shutil.copy2(config_file, backup_dir / "config-before.yaml")
    os.chmod(backup_dir / "config-before.yaml", 0o600)
    changes = {"projects": 0, "jobs": 0}
    db.execute("BEGIN IMMEDIATE")
    try:
        for name, raw in list(db.execute("SELECT name,config_json FROM projects")):
            cfg = json.loads(raw)
            original = dict(cfg)
            if "command_template" in cfg:
                old = cfg.pop("command_template")
                if cfg.get("command") and cfg["command"] != old:
                    raise RuntimeError(f"conflicting commands in project {name}")
                cfg.setdefault("command", old)
            # Keep all other cached fields; parameter metadata remains in the
            # backup and is ignored by the new parser. Source YAML is unchanged.
            if cfg != original:
                db.execute("UPDATE projects SET config_json=? WHERE name=?", (json.dumps(cfg, ensure_ascii=False, separators=(",", ":")), name))
                changes["projects"] += 1
        for job_id, target, raw in list(db.execute("SELECT id,target,config_json FROM jobs")):
            cfg = json.loads(raw)
            if not cfg.get("target") and target:
                # This is the recorded execution target. The original choice
                # source/effective config cannot be reconstructed retrospectively.
                cfg["target"] = target
                db.execute("UPDATE jobs SET config_json=? WHERE id=?", (json.dumps(cfg, ensure_ascii=False, separators=(",", ":")), job_id))
                changes["jobs"] += 1
        after = fingerprints(db)
        if before != after:
            raise RuntimeError("execution records or table counts changed; rolling back")
        if db.execute("PRAGMA quick_check").fetchone()[0] != "ok":
            raise RuntimeError("post-migration integrity check failed")
        db.commit()
    except BaseException:
        db.rollback()
        raise
    finally:
        db.close()
    manifest = {"changes": changes, "preserved_tables": before, "backup": str(backup_file), "integrity": "ok"}
    (backup_dir / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    os.chmod(backup_dir / "manifest.json", 0o600)
    print(json.dumps({"changes": changes, "counts": {k: v["count"] for k, v in before.items()}, "integrity": "ok", "backup": str(backup_file)}, ensure_ascii=False))


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit(__doc__)
    migrate(*(Path(arg).resolve() for arg in sys.argv[1:]))
