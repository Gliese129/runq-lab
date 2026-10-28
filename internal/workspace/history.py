"""Exact record queries over an incrementally indexed prefix + current tail.

Uses the metrics pyramid's 32-point base / fanout-4 layout. Nodes store
coordinate ranges and byte offsets, not averaged values: frames need exact
as-of records. All indexing and seeks happen on the data-owning machine.
"""
import fcntl
import hashlib
import json
import math
from pathlib import Path
import sys

root, stream, count, axis = Path(sys.argv[1]), sys.argv[2], int(sys.argv[3]), sys.argv[4]
raw_path = root / (stream + ".jsonl")
cache = root / (stream + ".exact.pyr.json")
BASE, FANOUT = 32, 4

def keys(value):
    return sorted(value)

def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)

def tree(points, coordinate):
    # points: [offset, length, step-or-null, ts]. Leaf coordinates preserve
    # file order for deterministic timestamp/step ties.
    entries = [[p[coordinate], p[0], p[1]] for p in points if p[coordinate] is not None]
    entries.sort(key=lambda p: (p[0], p[1]))
    if not entries:
        return None
    nodes = [{"min": x[0][0], "max": x[-1][0], "items": x}
             for x in (entries[i:i+BASE] for i in range(0, len(entries), BASE))]
    while len(nodes) > 1:
        nodes = [{"min": x[0]["min"], "max": x[-1]["max"], "children": x}
                 for x in (nodes[i:i+FANOUT] for i in range(0, len(nodes), FANOUT))]
    return nodes[0]

def before(node, at):
    if node is None or node["min"] > at:
        return None
    if "items" in node:
        for point in reversed(node["items"]):
            if point[0] <= at:
                return point
        return None
    for child in reversed(node["children"]):
        if child["min"] <= at:
            result = before(child, at)
            if result is not None:
                return result
    return None

def run():
    if not raw_path.exists():
        return {"by": axis, "frames": [], "series": []}
    with (root / (stream + ".exact.pyr.lock")).open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        index = {"offset": 0, "series": {}, "schemas": {}}
        try:
            index = json.loads(cache.read_text())
        except (OSError, ValueError):
            pass
        stat = raw_path.stat()
        fingerprint = [stat.st_dev, stat.st_ino]
        if index.get("file") != fingerprint or index["offset"] > stat.st_size:
            index = {"offset": 0, "series": {}, "schemas": {}}
        changed = False
        with raw_path.open("rb") as raw:
            raw.seek(index["offset"])
            while True:
                offset = raw.tell()
                line = raw.readline()
                if not line or not line.endswith(b"\n"):
                    break
                index["offset"] = raw.tell()
                try:
                    rec = json.loads(line)
                    if stream != "metrics":
                        axes = rec.get("axes") or {}
                        metrics = rec.get("metrics") or {}
                        if not isinstance(axes, dict) or not isinstance(metrics, dict):
                            continue
                        schema = [sorted(k for k in axes if k != "step"), keys(metrics)]
                        section = rec.get("section") or "auto-" + hashlib.sha1(canonical(schema).encode()).hexdigest()[:8]
                        frozen = index["schemas"].setdefault(section, schema)
                        identity = {k: axes.get(k) for k in frozen[0]}
                        key = canonical([section, identity])
                        step = axes.get("step")
                    else:
                        if rec.get("type") != "metric" or not isinstance(rec.get("value"), (int, float)):
                            continue
                        key = rec["key"]
                        step = rec.get("step")
                    if not isinstance(step, (int, float)) or isinstance(step, bool) or not math.isfinite(step):
                        step = None
                    ts = rec["ts"]
                    if not isinstance(ts, (int, float)) or not math.isfinite(ts):
                        continue
                    item = index["series"].setdefault(key, {"points": []})
                    item["points"].append([offset, len(line), step, ts])
                    changed = True
                except (ValueError, TypeError, KeyError):
                    continue
            if changed:
                for item in index["series"].values():
                    item["step"] = tree(item["points"], 2)
                    item["time"] = tree(item["points"], 3)
                index["file"] = fingerprint
                temp = cache.with_suffix(".tmp")
                temp.write_text(canonical(index))
                temp.replace(cache)
            def record(point):
                raw.seek(point[1])
                return json.loads(raw.read(point[2]))
            def band(r):
                # The shown error of a runq.stats estimate: (lo, hi), else None.
                est = r.get("est")
                v = r.get("value")
                if not isinstance(est, dict) or not isinstance(v, (int, float)):
                    return None
                for kind in est.get("show") or []:
                    if kind in ("STD", "STDERR"):
                        spread = est.get(kind.lower())
                        if isinstance(spread, (int, float)):
                            return [v - spread, v + spread]
                    if kind == "CI95":
                        ci = est.get("ci95")
                        if isinstance(ci, list) and len(ci) == 2:
                            return ci
                return None
            if stream == "metrics":
                series = []
                for key, item in sorted(index["series"].items()):
                    points = [p for p in item["points"] if p[2] is not None]
                    # Alignment is based on the complete actual step sequence,
                    # before display decimation. No interpolation or averaging.
                    signature = hashlib.sha256(canonical([p[2] for p in points]).encode()).hexdigest()
                    if len(points) > count:
                        points = [points[round(i*(len(points)-1)/(count-1))] for i in range(count)]
                    records = [record([p[2], p[0], p[1]]) for p in points]
                    item = {"key": key, "step": [r["step"] for r in records],
                            "value": [r["value"] for r in records], "signature": signature}
                    bands = [band(r) for r in records]
                    if any(b is not None for b in bands):
                        item["lo"] = [b and b[0] for b in bands]
                        item["hi"] = [b and b[1] for b in bands]
                    meta = records[-1].get("series") if records else None
                    if isinstance(meta, dict):
                        item["series"] = meta
                    series.append(item)
                return {"series": series}
            trees = [item.get(axis) for item in index["series"].values() if item.get(axis)]
            if not trees:
                return {"by": axis, "frames": []}
            lo, hi = min(t["min"] for t in trees), max(t["max"] for t in trees)
            ats = json.loads(sys.argv[5]) if len(sys.argv)>5 else sorted(set(lo + i*(hi-lo)/(count-1) for i in range(count)))
            frames = []
            for at in ats:
                records = []
                for key, item in sorted(index["series"].items()):
                    point = before(item.get(axis), at)
                    if point is None:
                        continue
                    rec = record(point)
                    section, identity = json.loads(key)
                    frozen = index["schemas"][section]
                    rec["section"] = section
                    rec["axes"] = {k: rec.get("axes", {}).get(k) for k in frozen[0]}
                    if axis == "step":
                        rec["axes"]["step"] = point[0]
                    rec["metrics"] = {k: rec.get("metrics", {}).get(k) for k in frozen[1]}
                    records.append(rec)
                frames.append({"at": at, "records": records})
            return {"by": axis, "frames": frames}

print(json.dumps(run(), allow_nan=False, separators=(",", ":")))
