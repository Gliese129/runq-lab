"""Tests for runq.record — sectioned latest-result emission and leniency.

record() is pure data emission: no ctx.current_step
writeback. Its leniency doctrine (drop invalid keys individually, never
raise) is the contract the Go-side ingest relies on — every case here
mirrors a clause of the RQ2-1 alignment (#6).
"""
import json
import math

import pytest

import runq


def _read_records(path):
    return [json.loads(line) for line in path.read_text().splitlines()]


class FakeNumpyScalar:
    """Duck-typed numpy scalar: .item() unwraps to a python scalar."""

    def __init__(self, value):
        self._value = value

    def item(self):
        return self._value


class FakeNumpyArray:
    """Duck-typed >0-d numpy array: .item() raises like numpy's does."""

    def item(self):
        raise ValueError("can only convert an array of size 1 to a Python scalar")


# ---- basic format ----

def test_record_basic_format(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"math-bench": 24.8}, model="baseline-8b", step=2000)
    runq.result({"math-bench": 25.1}, model="baseline-8b", step=4000)
    recs = _read_records(tmp_path / "runq_results.jsonl")
    assert len(recs) == 2
    r = recs[0]
    assert r["metrics"] == {"math-bench": 24.8}
    assert r["axes"] == {"model": "baseline-8b", "step": 2000}
    assert isinstance(r["ts"], int)
    assert r["section"].startswith("auto-")
    assert recs[1]["section"] == r["section"]
    assert r["section_keys"] == {"axes": ["model"], "metrics": ["math-bench"]}
    # SDK does NOT write task_id / job_id (daemon fills on reap).
    assert "task_id" not in r
    assert "job_id" not in r
    assert "type" not in r  # results.jsonl is single-purpose: no discriminator


def test_record_explicit_section_freezes_first_schema(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result(
        {"loss": 0.4, "score": 8},
        section="eval",
        model="a",
        data="validation",
        step=1,
    )
    with pytest.warns(UserWarning, match="missing keys become null, new keys are ignored"):
        runq.result(
            {"score": 9, "extra_metric": 10},
            section="eval",
            model="b",
            extra_axis="ignored",
            step=2,
        )

    first, second = _read_records(tmp_path / "runq_results.jsonl")
    assert first["section_keys"] == {
        "axes": ["data", "model"],
        "metrics": ["loss", "score"],
    }
    assert second["section_keys"] == first["section_keys"]
    assert second["axes"] == {"data": None, "model": "b", "step": 2}
    assert second["metrics"] == {"loss": None, "score": 9.0}


def test_result_keeps_first_metric_order(clean_env, tmp_path, monkeypatch, recwarn):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"准确率": 0.5, "交付率": 0.4, "a": 1}, section="eval", split="x")
    runq.result({"a": 2, "交付率": 0.3, "准确率": 0.6}, section="eval", split="y")  # same keys: no warning
    runq.result({"准确率": 0.5, "a": 1}, split="x")
    runq.result({"a": 1, "准确率": 0.5}, split="y")
    first, second, auto1, auto2 = _read_records(tmp_path / "runq_results.jsonl")
    assert first["section_keys"]["metrics"] == ["准确率", "交付率", "a"]
    assert list(first["metrics"]) == list(second["metrics"]) == ["准确率", "交付率", "a"]
    # The auto section's name does not depend on the order.
    assert auto1["section"] == auto2["section"]
    assert list(auto2["metrics"]) == ["准确率", "a"]
    assert not [w for w in recwarn if "keys changed" in str(w.message)]
    # A restarted writer keeps the order the file has.
    runq._result._reset_for_tests()
    runq.context()
    runq.result({"a": 3, "准确率": 0.1, "交付率": 0.2}, section="eval", split="z")
    assert list(_read_records(tmp_path / "runq_results.jsonl")[-1]["metrics"]) == ["准确率", "交付率", "a"]


def test_record_writes_to_results_not_metrics(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"acc": 0.9}, model="a")
    assert (tmp_path / "runq_results.jsonl").exists()
    assert not (tmp_path / "runq_metrics.jsonl").exists()


def test_record_env_path_derived_from_metrics_file(clean_env, tmp_path, monkeypatch):
    """daemon mode: results.jsonl is metrics_file's sibling, fixed name."""
    metrics = tmp_path / "task" / "metrics.jsonl"
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(metrics))
    monkeypatch.setenv("RUNQ_SOCKET_PATH", "/fake")
    runq.context()
    runq.result({"acc": 0.9}, model="a")
    assert (tmp_path / "task" / "results.jsonl").exists()


# ---- leniency: metrics ----

def test_record_metrics_none_is_empty(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result(None, model="a")
    recs = _read_records(tmp_path / "runq_results.jsonl")
    assert recs[0]["metrics"] == {}
    assert recs[0]["axes"] == {"model": "a"}


def test_record_metrics_empty_dict_is_legal(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({}, model="a")
    recs = _read_records(tmp_path / "runq_results.jsonl")
    assert recs[0]["metrics"] == {}


def test_record_metrics_non_dict_warns_and_ignores(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.warns(UserWarning, match="metrics must be a dict"):
        runq.result("oops", model="a")
    recs = _read_records(tmp_path / "runq_results.jsonl")
    assert recs[0]["metrics"] == {}
    assert recs[0]["axes"] == {"model": "a"}


def test_record_metric_values_coerced_to_float(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"count": 42, "np": FakeNumpyScalar(0.5)}, model="a")
    recs = _read_records(tmp_path / "runq_results.jsonl")
    assert recs[0]["metrics"] == {"count": 42.0, "np": 0.5}


def test_record_invalid_metric_dropped_individually(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.warns(UserWarning, match="dropped"):
        runq.result({"good": 1.0, "nan": math.nan, "text": "no"}, model="a")
    recs = _read_records(tmp_path / "runq_results.jsonl")
    assert recs[0]["metrics"] == {"good": 1.0}


# ---- leniency: axes ----

def test_record_axis_types_preserved(clean_env, tmp_path, monkeypatch):
    """str stays str, bool stays bool, int stays int — axes are NOT floatified."""
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"acc": 0.9}, model="a", frozen=True, step=5, lr=0.1)
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"] == {"model": "a", "frozen": True, "step": 5, "lr": 0.1}
    assert isinstance(r["axes"]["frozen"], bool)
    assert isinstance(r["axes"]["step"], int)


def test_record_numpy_axis_coerced(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"acc": 0.9}, step=FakeNumpyScalar(7))
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"]["step"] == 7


def test_record_invalid_axis_dropped_individually(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.warns(UserWarning, match="axis"):
        runq.result({"acc": 0.9}, model="a", blob=[1, 2], arr=FakeNumpyArray(),
                    inf=math.inf)
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"] == {"model": "a"}


def test_record_all_invalid_writes_nothing(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.warns(UserWarning) as caught:
        runq.result({"nan": math.nan}, blob=[1, 2])
    assert any("discarded" in str(w.message) for w in caught)
    assert not (tmp_path / "runq_results.jsonl").exists()


def test_record_one_valid_key_is_enough(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.warns(UserWarning):
        runq.result({"nan": math.nan}, model="a")
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"] == {"model": "a"}
    assert r["metrics"] == {}


def test_record_no_args_is_legal(clean_env, tmp_path, monkeypatch):
    """Providing nothing ≠ providing garbage: an empty record is written."""
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result()
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"] == {}
    assert r["metrics"] == {}


# ---- step auto-fill ----

def test_record_step_autofilled_from_ctx(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    ctx.current_step = 300
    runq.result({"acc": 0.9}, model="a")
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"]["step"] == 300


def test_record_explicit_step_wins_over_ctx(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    ctx.current_step = 300
    runq.result({"acc": 0.9}, step=5)
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert r["axes"]["step"] == 5


def test_record_no_step_when_ctx_unset(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"acc": 0.9}, model="a")
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert "step" not in r["axes"]


def test_record_invalid_explicit_step_not_substituted(clean_env, tmp_path, monkeypatch):
    """An explicitly passed but invalid step must NOT be silently replaced
    with the ctx cursor — that would fabricate data."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    ctx.current_step = 300
    with pytest.warns(UserWarning, match="axis"):
        runq.result({"acc": 0.9}, step=math.nan)
    r = _read_records(tmp_path / "runq_results.jsonl")[0]
    assert "step" not in r["axes"]


# ---- purity: no ctx writeback ----

def test_record_does_not_touch_current_step(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    ctx.current_step = 10
    runq.result({"acc": 0.9}, step=99)
    assert ctx.current_step == 10


def test_record_requires_context():
    with pytest.raises(RuntimeError, match="context"):
        runq.result({"acc": 0.9})


@pytest.mark.parametrize("axes", [{"cell": "test"}, {"producer": "judge"}, {"param.lr": 0.1}])
def test_result_refuses_runqs_own_names(clean_env, tmp_path, monkeypatch, axes):
    """An axis named like what runq gives each record (its cell, scoring
    script, params) would take that one's place; it is refused, nothing is
    written."""
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.raises(ValueError, match="name runq gives each record"):
        runq.result({"acc": 0.5}, **axes)
    out = tmp_path / "runq_results.jsonl"
    assert not out.exists() or _read_records(out) == []


def test_result_refuses_a_series_on_runqs_own_name(clean_env, tmp_path, monkeypatch):
    """A Series' index axis is an axis too: one named cell is refused before
    any row is written."""
    from runq._values import Series
    monkeypatch.chdir(tmp_path)
    runq.context()
    with pytest.raises(ValueError, match="name runq gives each record"):
        runq.result({"acc": Series("cell", ["a", "b"], [0.5, 0.6])})
    out = tmp_path / "runq_results.jsonl"
    assert not out.exists() or _read_records(out) == []
