"""Tests for runq.stats and how runq.result / runq.log take its values."""
import itertools
import json
import math
import subprocess
import sys
from collections import Counter

import pytest

import runq

np = pytest.importorskip("numpy")
from runq import stats  # noqa: E402


def _lines(path):
    return [json.loads(line) for line in path.read_text().splitlines() if line]


# ---- est ----------------------------------------------------------

def test_est_of_units():
    e = stats.est([1.0, 2.0, 3.0, float("nan")])
    assert e.value == 2.0 and e.n == 3
    assert e.std == pytest.approx(1.0)
    assert e.stderr == pytest.approx(1 / math.sqrt(3))
    assert e.ci95 == pytest.approx((2 - 1.959964 / math.sqrt(3), 2 + 1.959964 / math.sqrt(3)), rel=1e-5)
    assert e.show == stats.STDERR  # the default
    assert e.to_json()["show"] == ["STDERR"]


def test_est_given():
    e = stats.est(0.437, stderr=0.038, n=125, show=stats.STDERR | stats.N, unit=stats.PCT, better=stats.HIGHER)
    assert e.to_json() == {"value": 0.437, "stderr": 0.038, "n": 125,
                           "ci95": [pytest.approx(0.437 - 1.959964 * 0.038), pytest.approx(0.437 + 1.959964 * 0.038)],
                           "show": ["STDERR", "N"], "unit": "pct", "better": "higher"}
    assert stats.est(0.5).show == stats.STDERR.__class__(0)  # nothing to show by default
    with pytest.raises(ValueError, match="no std="):
        stats.est(0.5, stderr=0.1, show=stats.STD)


def test_est_rejects_2d_and_empty_is_none():
    with pytest.raises(ValueError, match="1-D"):
        stats.est(np.ones((3, 4)))
    e = stats.est([float("nan")])
    assert e.value is None and e.n == 0


def test_bootstrap_is_seeded():
    x = np.random.default_rng(3).random(50)
    a, b = stats.est(x, ci=stats.BOOTSTRAP), stats.est(x, ci=stats.BOOTSTRAP)
    assert a.ci95 == b.ci95 and a.ci95[0] < a.value < a.ci95[1]
    with pytest.raises(ValueError, match="per-unit"):
        stats.est(0.5, stderr=0.1, ci=stats.BOOTSTRAP)


# ---- flags --------------------------------------------------------

def test_show_takes_one_error_kind():
    with pytest.raises(ValueError, match="pick one of STD, STDERR, CI95"):
        stats.est([1.0, 2.0], show=stats.STD | stats.STDERR)
    assert stats.est([1.0, 2.0], show=stats.CI95 | stats.N).to_json()["show"] == ["CI95", "N"]


def test_categories_do_not_mix():
    with pytest.raises(TypeError, match="unit="):
        stats.STD | stats.PCT
    with pytest.raises(TypeError, match="unit="):
        stats.PCT | stats.STD
    with pytest.raises(TypeError, match="takes one value"):
        stats.HIGHER | stats.LOWER
    with pytest.raises(TypeError, match="unit="):
        stats.est([1.0], unit=stats.HIGHER)


# ---- pass@k -------------------------------------------------------

def _brute_pass(row, k):
    subsets = list(itertools.combinations(row, k))
    return sum(any(s) for s in subsets) / len(subsets)


def test_pass_at_k_matches_brute_force_and_default_k():
    rng = np.random.default_rng(1)
    correct = rng.random((20, 6)) < 0.3
    s = stats.pass_at_k(correct)
    assert s.axis == "k" and list(s) == [1, 2, 4, 6]
    for k in s:
        per = [_brute_pass(row, k) for row in correct]
        assert s[k].value == pytest.approx(np.mean(per))
        assert s[k].stderr == pytest.approx(np.std(per, ddof=1) / math.sqrt(20))
        assert s[k].n == 20 and s[k].better == stats.HIGHER
    assert list(stats.pass_at_k(np.zeros((2, 16), bool))) == [1, 2, 4, 8, 16]
    one = stats.pass_at_k(correct, k=1)
    assert isinstance(one, stats.Est) and one.value == pytest.approx(correct.mean())


def test_pass_at_k_validates():
    with pytest.raises(ValueError, match="2-D"):
        stats.pass_at_k([True, False])
    with pytest.raises(ValueError, match="k=9"):
        stats.pass_at_k(np.ones((2, 4), bool), k=[1, 9])


# ---- maj@k --------------------------------------------------------

def _brute_maj(row, gold, k):
    total = 0.0
    subsets = list(itertools.combinations(row, k))
    for s in subsets:
        counts = Counter(s)
        top = max(counts.values())
        tied = [a for a, c in counts.items() if c == top]
        total += (gold in tied and gold is not None) / len(tied)
    return total / len(subsets)


def test_maj_at_k_exact():
    answers = [["a", "b", "a", None, "c"], ["x", "x", "y", "y", "z"], [None, None, "q", "q", "r"]]
    gold = ["a", "y", None]
    s = stats.maj_at_k(answers, gold, k=[1, 2, 3, 5])
    for k in s:
        per = [_brute_maj(row, g, k) for row, g in zip(answers, gold)]
        assert s[k].value == pytest.approx(np.mean(per))


def test_maj_at_k_monte_carlo_is_reproducible():
    rng = np.random.default_rng(2)
    answers = rng.integers(0, 3, size=(4, 20))
    gold = [0, 1, 2, 0]
    a = stats.maj_at_k(answers, gold, k=10)  # C(20, 10) = 184756 subsets → sampled
    b = stats.maj_at_k(answers, gold, k=10)
    assert a == b and 0 <= a.value <= 1


# ---- runq.result --------------------------------------------------

def test_result_expands_a_series(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    correct = np.array([[1, 0, 1, 1], [0, 0, 1, 0], [1, 1, 1, 1]], bool)
    lang_ok = np.array([[1, 1, 0, 1], [1, 1, 1, 1], [1, 0, 1, 1]], bool)
    runq.result({"准确率": stats.pass_at_k(correct, unit=stats.PCT),
                 "交付率": stats.pass_at_k(correct & lang_ok, unit=stats.PCT)},
                section="PolyMath high")
    rows = _lines(tmp_path / "runq_results.jsonl")
    assert [r["axes"] for r in rows] == [{"k": 1}, {"k": 2}, {"k": 4}]
    assert all(r["index"] == ["k"] and r["section"] == "PolyMath high" for r in rows)
    acc = rows[0]["metrics"]["准确率"]
    assert acc["value"] == pytest.approx(correct.mean()) and acc["n"] == 3
    assert acc["unit"] == "pct" and acc["better"] == "higher" and acc["show"] == ["STDERR"]


def test_result_takes_an_est_without_index(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    runq.result({"acc": stats.est([0.0, 1.0, 1.0])}, section="math")
    (row,) = _lines(tmp_path / "runq_results.jsonl")
    assert "index" not in row and row["metrics"]["acc"]["value"] == pytest.approx(2 / 3)


def test_result_refuses_mixed_series_and_scalars(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    s = stats.pass_at_k(np.ones((2, 4), bool))
    with pytest.raises(ValueError, match="call of their own"):
        runq.result({"pass": s, "n_problems": 2})
    with pytest.raises(ValueError, match=r"'a' has k=\[1, 2, 4\], 'b' has k=\[1, 2\]"):
        runq.result({"a": s, "b": stats.pass_at_k(np.ones((2, 4), bool), k=[1, 2])})
    with pytest.raises(ValueError, match="series index"):
        runq.result({"a": s}, k=3)
    assert not (tmp_path / "runq_results.jsonl").exists()


def test_version_result_takes_series(clean_env, tmp_path):
    from runq._job import _task
    v = _task(tmp_path, {"id": "tk1", "task_dir": str(tmp_path / "tk1")}, "score")
    (tmp_path / "tk1").mkdir()
    v.result({"pass": stats.pass_at_k(np.ones((2, 2), bool))}, section="s")
    assert [r["axes"]["k"] for r in _lines(v.results_file)] == [1, 2]


# ---- runq.log -----------------------------------------------------

class FakeRun:
    url = "https://wandb.ai/x/y/runs/1"

    def __init__(self):
        self.logged = []
        self.summary = {}

    def log(self, data, step=None):
        self.logged.append((data, step))


def test_log_est_and_series_and_wandb_mirror(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setitem(sys.modules, "wandb", None)  # the SDK never imports wandb
    runq.context()
    run = FakeRun()
    runq.link("wandb", run)
    with runq.log_group("eval"):
        runq.log({"loss": 0.5, "reward": stats.est([0.0, 1.0]),
                  "pass": stats.pass_at_k(np.array([[1, 0], [1, 1]], bool))}, step=10)
    events = _lines(tmp_path / "runq_metrics.jsonl")
    assert [e["key"] for e in events] == ["eval/loss", "eval/reward", "eval/pass@1", "eval/pass@2"]
    assert events[1]["est"]["stderr"] == pytest.approx(0.5) and events[1]["value"] == 0.5
    assert events[2]["series"] == {"name": "eval/pass", "axis": "k", "at": 1}
    (data, step), = run.logged
    assert step == 10
    assert set(data) == {"eval/loss", "eval/reward", "eval/reward_stderr",
                         "eval/pass@1", "eval/pass@1_stderr", "eval/pass@2", "eval/pass@2_stderr"}


def test_unlinked_log_does_not_forward_and_result_is_not_mirrored(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    runq.context()
    run = FakeRun()
    runq.log({"loss": 1.0})
    runq.link("wandb", run)
    runq.result({"acc": 0.5})
    assert run.logged == []


def test_link_writes_where_the_results_are(clean_env, tmp_path, monkeypatch):
    monkeypatch.setenv("RUNQ_TASK_ID", "tk1")
    monkeypatch.setenv("RUNQ_EXPERIMENT_ID", "ex9")
    monkeypatch.setenv("RUNQ_CELL", "A1")
    monkeypatch.setenv("RUNQ_VERSION", "2")
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    runq.context()
    run = FakeRun()
    runq.link("wandb", run)
    assert run.summary == {"runq": "ex9/A1@v2"}
    monkeypatch.setenv("RUNQ_RESULTS_URL", "http://127.0.0.1:8077/#/projects/p/ex9")
    runq.link("wandb", run)
    assert run.summary == {"runq": "http://127.0.0.1:8077/#/projects/p/ex9"}


# ---- numpy stays optional -----------------------------------------

def test_import_runq_without_numpy():
    code = (
        "import sys; sys.modules['numpy'] = None\n"
        "import runq\n"
        "try:\n    from runq import stats\nexcept ImportError as e:\n    print(e)\n"
    )
    out = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True, check=True)
    assert "runq.stats needs numpy" in out.stdout
