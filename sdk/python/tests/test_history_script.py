"""The controller's curve query (internal/workspace/history.py) against what
runq.log writes: a NaN/Inf (written as null) is a gap, never removed, and a
point logged without a step still gets a defined x (its write-order position
within the curve)."""
import json
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parents[3] / "internal" / "workspace" / "history.py"

pytestmark = pytest.mark.skipif(not SCRIPT.exists(), reason="needs the runq repo checkout")


def _curves(root: Path, events) -> dict:
    (root / "metrics.jsonl").write_text("".join(json.dumps(e) + "\n" for e in events))
    out = subprocess.check_output([sys.executable, str(SCRIPT), str(root), "metrics", "400", "step"])
    return {s["key"]: s for s in json.loads(out)["series"]}


def test_null_value_stays_a_gap(tmp_path):
    series = _curves(tmp_path, [
        {"type": "metric", "key": "gap", "value": 1, "step": 1, "ts": 1},
        {"type": "metric", "key": "gap", "value": None, "step": 2, "ts": 2},
        {"type": "metric", "key": "gap", "value": 3, "step": 3, "ts": 3},
    ])
    assert series["gap"]["step"] == [1, 2, 3]
    assert series["gap"]["value"] == [1, None, 3]


def test_decimation_keeps_gaps(tmp_path):
    events = [{"type": "metric", "key": "m", "value": None if s in (7, 51) else float(s), "step": s, "ts": s}
              for s in range(100)]
    (tmp_path / "metrics.jsonl").write_text("".join(json.dumps(e) + "\n" for e in events))
    out = subprocess.check_output([sys.executable, str(SCRIPT), str(tmp_path), "metrics", "10", "step"])
    m = json.loads(out)["series"][0]
    # 10 picked points plus each gap they skip over; values stay exact.
    assert [s for s, v in zip(m["step"], m["value"]) if v is None] == [7, 51]
    assert m["step"] == sorted(m["step"]) and len(m["step"]) == 12
    assert all(v == s for s, v in zip(m["step"], m["value"]) if v is not None)


def test_stepless_points_use_write_order(tmp_path):
    series = _curves(tmp_path, [
        {"type": "metric", "key": "acc", "value": 0.5, "step": None, "ts": 10},
        {"type": "metric", "key": "loss", "value": 2.0, "step": 7, "ts": 11},
        {"type": "metric", "key": "acc", "value": None, "ts": 12},
        {"type": "metric", "key": "acc", "value": 0.7, "ts": 13},
    ])
    # Positions count only this curve's own points.
    assert series["acc"]["step"] == [0, 1, 2]
    assert series["acc"]["value"] == [0.5, None, 0.7]
    assert series["loss"]["step"] == [7]


def test_line_without_value_is_not_a_point(tmp_path):
    series = _curves(tmp_path, [
        {"type": "metric", "key": "m", "step": 1, "ts": 1},
        {"type": "metric", "key": "m", "value": "high", "step": 2, "ts": 2},
        {"type": "metric", "key": "m", "value": 4, "step": 3, "ts": 3},
    ])
    assert series["m"]["step"] == [3]


def test_older_index_cache_is_rebuilt(tmp_path):
    events = [
        {"type": "metric", "key": "gap", "value": 1, "step": 1, "ts": 1},
        {"type": "metric", "key": "gap", "value": None, "step": 2, "ts": 2},
    ]
    _curves(tmp_path, events)
    cache = tmp_path / "metrics.exact.pyr.json"
    index = json.loads(cache.read_text())
    # What an index built before gaps were kept looks like: no version, the
    # null point never indexed, offset already at the end of the file.
    del index["version"]
    index["series"]["gap"]["points"] = index["series"]["gap"]["points"][:1]
    cache.write_text(json.dumps(index))
    out = subprocess.check_output([sys.executable, str(SCRIPT), str(tmp_path), "metrics", "400", "step"])
    assert json.loads(out)["series"][0]["value"] == [1, None]


def test_frames_report_each_sections_range(tmp_path, monkeypatch):
    """The animation frames a view's section, not another section's span:
    the frames query reports each section's own [min, max]."""
    monkeypatch.chdir(tmp_path)
    monkeypatch.delenv("RUNQ_TASK_DIR", raising=False)
    import runq
    runq.context()
    for step in (10, 20):
        runq.result({"acc": 0.5}, section="eval", step=step)
    runq.result({"loss": 1.0}, section="train", step=1_000_000)
    (tmp_path / "runq_results.jsonl").rename(tmp_path / "results.jsonl")  # manual mode's name
    out = subprocess.check_output([sys.executable, str(SCRIPT), str(tmp_path), "results", "2", "step"])
    ranges = json.loads(out)["ranges"]
    assert ranges == {"eval": [10, 20], "train": [1_000_000, 1_000_000]}


def test_time_frames_follow_the_tables_latest(tmp_path, monkeypatch):
    """Along time, a frame shows what the results table showed then: a
    later re-evaluation of an older checkpoint (smaller step) does not
    replace the larger step's result."""
    monkeypatch.chdir(tmp_path)
    monkeypatch.delenv("RUNQ_TASK_DIR", raising=False)
    import runq
    runq.context()
    runq.result({"acc": 0.9}, section="eval", step=200)
    runq.result({"acc": 0.6}, section="eval", step=100)
    (tmp_path / "runq_results.jsonl").rename(tmp_path / "results.jsonl")
    out = json.loads(subprocess.check_output([sys.executable, str(SCRIPT), str(tmp_path), "results", "2", "time"]))
    last = out["frames"][-1]["records"]
    assert [r["metrics"]["acc"] for r in last] == [0.9]
