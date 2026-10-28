"""Tests for runq.context() — mode detection, env parsing, params loading."""
import json
import os

import pytest

import runq
from runq import _context

# ---- mode detection ----

def test_manual_mode_no_env(clean_env, tmp_path, monkeypatch):
    """No RUNQ_TASK_ID → manual mode, jsonl in cwd, local UUID task_id."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    assert ctx.mode == "manual"
    assert ctx.task_id.startswith("manual-")
    assert ctx.metrics_file == tmp_path / "runq_metrics.jsonl"
    assert ctx.checkpoint_dir is None  # manual has no ckpt dir
    assert not ctx.metrics_file.exists()  # manual runs leave cwd alone until written


def test_task_mode(clean_env, tmp_path, monkeypatch):
    """RUNQ_TASK_ID set → task mode; a leftover socket env changes nothing."""
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    monkeypatch.setenv("RUNQ_SOCKET_PATH", "/tmp/runq.sock")
    ctx = runq.context()
    assert ctx.mode == "task"
    assert ctx.task_id == "t1"
    assert not hasattr(ctx, "socket_path")


def test_task_mode_precreates_output_files(clean_env, tmp_path, monkeypatch):
    """metrics.jsonl / results.jsonl / links.json exist after context(), so
    later appends need no new inode; existing content is kept (retries)."""
    (tmp_path / "metrics.jsonl").write_text('{"type":"metric"}\n')
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    runq.context()
    assert (tmp_path / "metrics.jsonl").read_text() == '{"type":"metric"}\n'
    assert (tmp_path / "results.jsonl").read_text() == ""
    assert json.loads((tmp_path / "links.json").read_text()) == {}


def _statvfs(free, total=1000, ffree=500, files=1000):
    return os.statvfs_result((4096, 4096, total, free, free, files, ffree, ffree, 0, 255))


def test_low_disk_warns_once_per_filesystem(clean_env, tmp_path, monkeypatch, capsys):
    """Three dirs on one filesystem under 5% free space → one warning."""
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    monkeypatch.setenv("RUNQ_CHECKPOINT_DIR", str(tmp_path / "checkpoints"))
    monkeypatch.setattr(os, "statvfs", lambda p: _statvfs(free=30))
    runq.context()
    err = capsys.readouterr().err
    assert err.count("runq: low on disk") == 1
    assert "3.0% space" in err


def test_low_inodes_warn_and_healthy_disk_is_quiet(clean_env, tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setattr(os, "statvfs", lambda p: _statvfs(free=900, ffree=10))
    runq.context()
    assert "1.0% inodes" in capsys.readouterr().err

    from runq import _context
    _context._reset_for_tests()
    monkeypatch.setattr(os, "statvfs", lambda p: _statvfs(free=900))
    runq.context()
    assert capsys.readouterr().err == ""


def test_removed_public_names(clean_env):
    for name in ("Context", "ParamDict", "get_ctx", "params", "seed"):
        assert not hasattr(runq, name), name
    assert not hasattr(runq._log, "latest_report_metrics")


# ---- params loading ----

def test_params_loaded_from_env_path(clean_env, tmp_path, monkeypatch):
    params_file = tmp_path / "params.json"
    params_file.write_text(json.dumps({"lr": 1e-4, "bs": 32}))
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    monkeypatch.setenv("RUNQ_PARAMS_FILE", str(params_file))

    ctx = runq.context()
    assert ctx.params == {"lr": 1e-4, "bs": 32}
    assert ctx.params.get("lr", 0.0) == 1e-4
    assert ctx.params.get("missing", 99) == 99


def test_params_manual_mode_picks_up_local(clean_env, tmp_path, monkeypatch):
    """In manual mode, a local params.json in cwd is loaded automatically."""
    monkeypatch.chdir(tmp_path)
    (tmp_path / "params.json").write_text(json.dumps({"x": 1}))
    ctx = runq.context()
    assert ctx.mode == "manual"
    assert ctx.params == {"x": 1}


def test_params_missing_is_empty_dict(clean_env, tmp_path, monkeypatch):
    """No params file in any mode → empty dict, not error."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    assert ctx.params == {}


def test_params_broken_json_raises(clean_env, tmp_path, monkeypatch):
    """Malformed params.json should surface, not silently produce {}."""
    monkeypatch.chdir(tmp_path)
    (tmp_path / "params.json").write_text("{not valid json")
    with pytest.raises(RuntimeError, match="failed to load params"):
        runq.context()


def test_params_non_dict_raises(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "params.json").write_text(json.dumps([1, 2, 3]))
    with pytest.raises(RuntimeError, match="must contain a JSON object"):
        runq.context()


# ---- singleton + get_ctx ----

def test_context_is_idempotent(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    a = runq.context()
    b = runq.context()
    assert a is b


def test_get_ctx_before_init_raises(clean_env):
    with pytest.raises(RuntimeError, match="context\\(\\) must be called"):
        _context.get_ctx()


def test_get_ctx_after_init(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    assert _context.get_ctx() is ctx


# ---- dir creation ----

def test_checkpoint_dir_auto_created(clean_env, tmp_path, monkeypatch):
    """context() pre-creates checkpoint_dir to avoid first-save failure."""
    ckpt = tmp_path / "deep" / "nested" / "ckpts"
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    monkeypatch.setenv("RUNQ_CHECKPOINT_DIR", str(ckpt))
    runq.context()
    assert ckpt.is_dir()


def test_params_get_requires_an_explicit_default():
    import pytest

    from runq._context import ParamDict

    p = ParamDict({"lr": 1e-4})
    assert p.get("lr", 0.0) == 1e-4
    assert p.get("missing", None) is None  # an explicit None is a decision
    with pytest.raises(TypeError, match=r"needs an explicit default"):
        p.get("missing")
    with pytest.raises(TypeError, match=r"needs an explicit default"):
        p.get("lr")  # even when present: the default must be in the code
    with pytest.raises(KeyError, match=r"Did you mean"):
        p["lrr"]




def test_seed_is_per_cell_not_per_version(clean_env, tmp_path, monkeypatch):
    import runq._context as rc

    seeds = []
    for task in ("t1", "t2"):
        monkeypatch.setenv("RUNQ_TASK_ID", task)
        monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path / task))
        monkeypatch.setenv("RUNQ_EXPERIMENT_ID", "ex1")
        monkeypatch.setenv("RUNQ_CELL", "base")
        rc._ctx = None
        seeds.append(runq.context().seed)
    assert seeds[0] == seeds[1]


def test_experiment_cell_version(clean_env, tmp_path, monkeypatch):
    monkeypatch.setenv("RUNQ_TASK_ID", "tk1")
    monkeypatch.setenv("RUNQ_EXPERIMENT_ID", "ex9")
    monkeypatch.setenv("RUNQ_JOB_ID", "ex9")
    monkeypatch.setenv("RUNQ_CELL", "A1-ja")
    monkeypatch.setenv("RUNQ_VERSION", "3")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path / "tk1"))
    ctx = runq.context()
    assert (ctx.experiment_id, ctx.cell, ctx.version) == ("ex9", "A1-ja", 3)
    with pytest.warns(DeprecationWarning, match="experiment_id"):
        assert ctx.job_id == "ex9"


def test_version_defaults_to_1(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    assert runq.context().version == 1
