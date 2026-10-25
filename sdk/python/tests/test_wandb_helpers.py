"""Tests for ctx.wandb_like_cfg / ctx.wandb_like_metric — F9.

Contract recap:
- Both properties return fresh dicts. No SDK import of wandb.
- wandb_like_cfg pulls project / name / group / config from ctx.
- wandb_like_metric returns the latest reported metrics dict, or {}.
- Returned dicts are copies — user mutation must not poison ctx /
  history.

These tests deliberately do not import wandb. If they ever start
needing wandb installed, the F9 design promise was broken.
"""
import sys

import pytest

import runq

# ---- pure-Python guarantee ----------------------------------------

def test_wandb_helpers_do_not_import_wandb(clean_env, tmp_path, monkeypatch):
    """Touching either property must not trigger ``import wandb``.

    We block wandb at the module-resolver level; any internal import
    inside the helpers would surface as a clear ImportError.
    """
    monkeypatch.setitem(sys.modules, "wandb", None)
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()

    # Both properties must succeed even with wandb explicitly disabled.
    cfg = ctx.wandb_like_cfg
    metric = ctx.wandb_like_metric

    assert isinstance(cfg, dict)
    assert isinstance(metric, dict)


# ---- wandb_like_cfg shape ----------------------------------------

def test_wandb_like_cfg_manual_mode(clean_env, tmp_path, monkeypatch):
    """Manual mode: empty job_id → group=None, name=task_id, project='runq'."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    cfg = ctx.wandb_like_cfg
    assert cfg["project"] == "runq"
    assert cfg["name"] == ctx.task_id
    assert cfg["group"] is None
    assert cfg["config"] == {}


def test_wandb_like_cfg_daemon_mode_shape(clean_env, tmp_path, monkeypatch):
    """Daemon mode: project / job_id / task_id present → all fields populated."""
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_JOB_ID", "j-abc")
    monkeypatch.setenv("RUNQ_PROJECT_NAME", "myproj")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_SOCKET_PATH", "/fake")
    ctx = runq.context()
    cfg = ctx.wandb_like_cfg
    assert cfg["project"] == "myproj"
    assert cfg["name"] == "j-abc/t1"
    assert cfg["group"] == "j-abc"


def test_wandb_like_cfg_includes_params(clean_env, tmp_path, monkeypatch):
    """ctx.params should appear under ``config`` key."""
    import json
    params_file = tmp_path / "params.json"
    params_file.write_text(json.dumps({"lr": 1e-4, "bs": 32}))
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    cfg = ctx.wandb_like_cfg
    assert cfg["config"] == {"lr": 1e-4, "bs": 32}


def test_wandb_like_cfg_returns_copy_of_params(clean_env, tmp_path, monkeypatch):
    """Mutation of the returned config dict must not affect ctx.params."""
    import json
    params_file = tmp_path / "params.json"
    params_file.write_text(json.dumps({"lr": 1e-4}))
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    cfg = ctx.wandb_like_cfg
    cfg["config"]["lr"] = 9999.0
    cfg["config"]["new_key"] = "hacked"
    # ctx.params unchanged.
    assert ctx.params == {"lr": 1e-4}


def test_wandb_like_cfg_project_default_when_unset(clean_env, tmp_path, monkeypatch):
    """Missing RUNQ_PROJECT_NAME → 'runq' fallback (sensible default)."""
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_SOCKET_PATH", "/fake")
    ctx = runq.context()
    assert ctx.wandb_like_cfg["project"] == "runq"


# ---- wandb_like_metric shape -------------------------------------

def test_wandb_like_metric_empty_when_no_report(clean_env, tmp_path, monkeypatch):
    """Before any report(), should be {} (still safe to splat)."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    assert ctx.wandb_like_metric == {}


def test_wandb_like_metric_returns_latest_report(clean_env, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    runq.report({"loss": 0.5, "acc": 0.9}, step=1)
    runq.report({"loss": 0.4, "acc": 0.95}, step=2)
    m = ctx.wandb_like_metric
    assert m == {"loss": 0.4, "acc": 0.95}


def test_wandb_like_metric_returns_copy(clean_env, tmp_path, monkeypatch):
    """Caller mutation of the returned dict must not poison the snapshot."""
    from runq._report import latest_report_metrics
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    runq.report({"loss": 0.5}, step=1)
    m = ctx.wandb_like_metric
    m["loss"] = 9999.0
    m["new"] = "injected"
    assert latest_report_metrics() == {"loss": 0.5}


def test_wandb_like_metric_splat_pattern(clean_env, tmp_path, monkeypatch):
    """Verify the canonical usage shape works: {**ctx.wandb_like_metric, ...}."""
    monkeypatch.chdir(tmp_path)
    ctx = runq.context()
    runq.report({"loss": 0.5}, step=1)
    merged = {**ctx.wandb_like_metric, "lr": 1e-4, "loss": 999.0}
    # User's extras + override should win on collision.
    assert merged == {"loss": 999.0, "lr": 1e-4}


# ---- project wandb config, resume id, rank gating, link ----------

def _hpc_task_env(monkeypatch, tmp_path, task_id="tk1"):
    monkeypatch.setenv("RUNQ_TASK_ID", task_id)
    monkeypatch.setenv("RUNQ_JOB_ID", "jb1")
    monkeypatch.setenv("RUNQ_PROJECT_NAME", "runq-proj")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    monkeypatch.setenv("RUNQ_NO_DAEMON", "1")


def test_wandb_like_cfg_reads_project_wandb_config(clean_env, tmp_path, monkeypatch):
    import json
    _hpc_task_env(monkeypatch, tmp_path)
    cfg_file = tmp_path / "wandb_config.json"
    cfg_file.write_text(json.dumps({"project": "jp-rl", "entity": "lab", "tags": ["c0"], "mode": "online"}))
    monkeypatch.setenv("RUNQ_WANDB_CONFIG_FILE", str(cfg_file))
    monkeypatch.setenv("RUNQ_SWEEP_KEYS", "lr,c0")
    cfg = runq.context().wandb_like_cfg
    assert cfg["project"] == "jp-rl"            # project config beats runq project name
    assert cfg["entity"] == "lab"
    assert cfg["tags"] == ["c0", "lr"]          # deduped merge with sweep keys
    assert cfg["mode"] == "online"
    assert cfg["id"] == "tk1" and cfg["resume"] == "allow"


def test_wandb_like_cfg_disables_non_zero_rank(clean_env, tmp_path, monkeypatch):
    _hpc_task_env(monkeypatch, tmp_path)
    monkeypatch.setenv("RANK", "1")
    assert runq.context().wandb_like_cfg["mode"] == "disabled"


def test_wandb_like_cfg_omits_entity_without_config(clean_env, tmp_path, monkeypatch):
    _hpc_task_env(monkeypatch, tmp_path)
    cfg = runq.context().wandb_like_cfg
    assert "entity" not in cfg and "mode" not in cfg
    assert cfg["project"] == "runq-proj"


def test_link_writes_links_json_and_merges(clean_env, tmp_path, monkeypatch):
    import json
    _hpc_task_env(monkeypatch, tmp_path)
    runq.context()
    runq.link("wandb", "https://wandb.ai/lab/jp-rl/runs/abc")
    runq.link("tb", "http://tb/1")
    runq.link("wandb", "https://wandb.ai/lab/jp-rl/runs/def")   # last write wins
    data = json.loads((tmp_path / "links.json").read_text())
    assert data == {"tb": "http://tb/1", "wandb": "https://wandb.ai/lab/jp-rl/runs/def"}
    with pytest.raises(ValueError):
        runq.link("", "x")


def test_task_directories_and_explicit_wandb_root(clean_env, tmp_path, monkeypatch):
    task = tmp_path / "task"
    root = tmp_path / "tracking"
    outputs = tmp_path / "products"
    monkeypatch.setenv("RUNQ_TASK_ID", "tk-directories")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(task))
    monkeypatch.setenv("RUNQ_OUTPUT_DIR", str(outputs))
    monkeypatch.setenv("WANDB_DIR", str(root))
    ctx = runq.context()
    assert ctx.output_dir == outputs and ctx.wandb_dir == root
    assert outputs.is_dir() and root.is_dir()
    assert ctx.wandb_like_cfg["dir"] == str(root)
    assert ctx.wandb_like_cfg["dir"] != str(root / "wandb")


def test_wandb_default_root_and_global_rank_not_local_rank(clean_env, tmp_path, monkeypatch):
    monkeypatch.setenv("RUNQ_TASK_ID", "tk-default")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.delenv("WANDB_DIR", raising=False)
    monkeypatch.setenv("LOCAL_RANK", "0")
    monkeypatch.setenv("RANK", "8")
    ctx = runq.context()
    assert ctx.wandb_like_cfg["dir"] == str(tmp_path)
    assert ctx.wandb_like_cfg["mode"] == "disabled"


def test_link_wandb_uses_real_url_only_on_global_main(clean_env, tmp_path, monkeypatch):
    import json
    from types import SimpleNamespace
    monkeypatch.setenv("RUNQ_TASK_ID", "tk1")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(tmp_path / "metrics.jsonl"))
    ctx = runq.context()
    run = SimpleNamespace(id="custom", url="https://wandb.ai/custom/entity/runs/custom")
    ctx.link_wandb(run, global_rank=1)
    assert not ctx.links_file.exists()
    ctx.link_wandb(SimpleNamespace(url=None), global_rank=0)
    assert not ctx.links_file.exists()
    ctx.link_wandb(run, global_rank=0)
    assert json.loads(ctx.links_file.read_text())["wandb"] == run.url
    ctx.link_wandb(SimpleNamespace(url=""), global_rank=0)
    assert json.loads(ctx.links_file.read_text())["wandb"] == run.url


def test_wandb_like_cfg_uses_run_and_job_names(clean_env, tmp_path, monkeypatch):
    """rc6: the row's run name is the W&B run name; the job name its group."""
    monkeypatch.setenv("RUNQ_TASK_ID", "t1")
    monkeypatch.setenv("RUNQ_JOB_ID", "j-abc")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(tmp_path))
    monkeypatch.setenv("RUNQ_SWEEP_KEYS", "lr")
    monkeypatch.setenv("RUNQ_RUN_NAME", "R1 lr")
    monkeypatch.setenv("RUNQ_JOB_NAME", "A1 第一批消融")
    cfg = runq.context().wandb_like_cfg
    assert cfg["name"] == "R1 lr"
    assert cfg["group"] == "A1 第一批消融"
