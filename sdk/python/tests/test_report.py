"""Tests for runq.report — jsonl emission, step writeback, latest metrics."""
import json
import pytest

import runq


def _read_events(path):
    return [json.loads(line) for line in path.read_text().splitlines()]


# ---- report() basic mechanics ------------------------------------

def test_report_single_metric_writes_one_event(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.report({"loss": 0.42}, step=5)
    events = _read_events(tmp_path / "runq_metrics.jsonl")
    assert len(events) == 1
    assert events[0]["type"] == "metric"
    assert events[0]["key"] == "loss"
    assert events[0]["value"] == 0.42
    assert events[0]["step"] == 5


def test_report_multiple_metrics_writes_one_event_each(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.report({"loss": 0.5, "acc": 0.9, "lr": 1e-4}, step=3)
    events = _read_events(tmp_path / "runq_metrics.jsonl")
    assert len(events) == 3
    keys = sorted(e["key"] for e in events)
    assert keys == ["acc", "loss", "lr"]
    # All share the same step + ts (one report → one timestamp).
    assert {e["step"] for e in events} == {3}
    assert len({e["ts"] for e in events}) == 1


def test_report_empty_metrics_writes_no_events(clean_env, tmp_path, monkeypatch):
    """report({}) is valid and emits nothing."""
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.report({})
    metrics_file = tmp_path / "runq_metrics.jsonl"
    # Either no file or zero events.
    if metrics_file.exists():
        assert metrics_file.read_text() == ""


def test_report_non_dict_raises(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.raises(TypeError, match="must be a dict"):
        runq.report(["loss", 0.5])  # type: ignore[arg-type]


# ---- step semantics: writeback + fallback ------------------------

def test_report_explicit_step_writes_back_to_ctx(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    runq.report({"loss": 0.5}, step=7)
    assert ctx.current_step == 7


def test_report_step_none_uses_ctx_fallback(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.report({"a": 1.0}, step=4)
    runq.report({"a": 2.0})  # no step → falls back to ctx (4)
    events = _read_events(tmp_path / "runq_metrics.jsonl")
    assert [e["step"] for e in events] == [4, 4]


def test_report_step_zero_preserved(clean_env, tmp_path, monkeypatch):
    """step=0 is legal — does NOT fall back to ctx."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    ctx.current_step = 99  # noise that fallback would pick up if step=0 misread
    runq.report({"loss": 0.5}, step=0)
    events = _read_events(tmp_path / "runq_metrics.jsonl")
    assert events[0]["step"] == 0
    assert ctx.current_step == 0  # writeback also went through


def test_report_step_none_with_unset_ctx_writes_null(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.report({"loss": 0.5})
    events = _read_events(tmp_path / "runq_metrics.jsonl")
    assert events[0]["step"] is None


# ---- latest metrics ------------------------------------------------

def test_report_returns_none(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    assert runq.report({"loss": 0.5}, step=1) is None


# ---- value coercion ----------------------------------------------

def test_report_coerces_int_value_to_float(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.report({"count": 42}, step=1)
    events = _read_events(tmp_path / "runq_metrics.jsonl")
    assert events[0]["value"] == 42.0
    assert isinstance(events[0]["value"], float)
