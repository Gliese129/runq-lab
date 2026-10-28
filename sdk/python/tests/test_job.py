"""Tests for runq.experiment() — reading job.json and writing per-version results."""
import http.server
import json
import os
import socketserver
import sys
import tempfile
import threading
from pathlib import Path

import pytest

import runq


def _snapshot(job_dir, recorded_root=None):
    root = recorded_root or str(job_dir)
    return {
        "version": 1,
        "job": {"id": "jb1", "name": "kl sweep", "project": "kl", "status": "done",
                "target": "tsubame", "created_at": 100},
        "tasks": [
            {"id": "tk1", "run_name": "R1", "status": "success", "params": {"kl": 0.1},
             "task_dir": f"{root}/tk1", "output_dir": f"{root}/tk1/outputs",
             "checkpoint_dir": f"{root}/tk1/checkpoints", "started_at": 1, "finished_at": 2,
             "last_output_at": 2, "batch_seq": 1},
            {"id": "tk2", "run_name": "R2", "status": "failed", "params": {"kl": 0.2},
             "task_dir": f"{root}/tk2", "output_dir": f"{root}/tk2/outputs",
             "checkpoint_dir": f"{root}/tk2/checkpoints", "started_at": 1, "finished_at": None,
             "last_output_at": None, "batch_seq": 2},
        ],
        "updated_at": 50, "observed_at": 40, "observation_state": "ok",
    }


@pytest.fixture
def job_dir(tmp_path):
    d = tmp_path / "jb1"
    for t in ("tk1", "tk2"):
        (d / t).mkdir(parents=True)
    (d / "job.json").write_text(json.dumps(_snapshot(d)))
    return d


def _lines(path):
    return [json.loads(line) for line in path.read_text().splitlines()]


def test_job_reads_snapshot(clean_env, job_dir):
    job = runq.experiment(job_dir, producer="score")
    assert (job.id, job.name, job.project, job.status, job.observed_at) == ("jb1", "kl sweep", "kl", "done", 40)
    assert job.dir == job_dir
    t1, t2 = [c.selected for c in job.cells]
    assert (t1.id, t1.run_name, t1.status, t1.params) == ("tk1", "R1", "success", {"kl": 0.1})
    assert t1.task_dir == job_dir / "tk1"
    assert t1.output_dir == job_dir / "tk1" / "outputs"
    assert t1.checkpoint_dir == job_dir / "tk1" / "checkpoints"
    assert (t1.started_at, t1.finished_at, t1.last_output_at, t1.batch_seq) == (1, 2, 2, 1)
    assert t2.finished_at is None and t2.batch_seq == 2
    # job.json itself is accepted as the ref too
    assert runq.experiment(job_dir / "job.json", producer="score").id == "jb1"


def test_job_rebases_recorded_paths_on_the_dir_read(clean_env, tmp_path):
    """The same job read through another mount point writes where it was read."""
    d = tmp_path / "mnt" / "jb1"
    (d / "tk1").mkdir(parents=True)
    (d / "tk2").mkdir()
    (d / "job.json").write_text(json.dumps(_snapshot(d, recorded_root="/gs/work/kl/jb1")))
    t1 = runq.experiment(d, producer="score").cells[0].selected
    assert t1.task_dir == d / "tk1"
    assert t1.output_dir == d / "tk1" / "outputs"


def _mixed(job_dir, root_a, root_b):
    snap = _snapshot(job_dir, recorded_root=root_a)
    snap["version"] = 2
    for t in snap["tasks"]:
        t["target"] = "tsubame"
    snap["tasks"].append(
        {"id": "tk3", "run_name": "R3", "status": "success", "params": {"kl": 0.3},
         "task_dir": f"{root_b}/tk3", "output_dir": f"{root_b}/tk3/outputs",
         "checkpoint_dir": f"{root_b}/tk3/checkpoints", "batch_seq": 2, "version": 1,
         "selected": True, "target": "abci"})
    (job_dir / "job.json").write_text(json.dumps(snap))


@pytest.mark.parametrize("recorded_a", [None, "/gs/work/kl/jb1"])
def test_job_keeps_another_targets_paths(clean_env, tmp_path, recorded_a):
    """A version that ran on another target (--into) is not rebased onto this mount."""
    d = tmp_path / "mnt" / "jb1"
    (d / "tk1").mkdir(parents=True)
    (d / "tk2").mkdir()
    _mixed(d, recorded_a or str(d), "/groups/abci/kl/jb1")
    exp = runq.experiment(d, producer="score")
    t1, t2, t3 = [c.selected for c in exp.cells]
    assert (t1.target, t1.local, t1.output_dir) == ("tsubame", True, d / "tk1" / "outputs")
    assert t2.local and t2.task_dir == d / "tk2"
    assert (t3.target, t3.local) == ("abci", False)
    assert t3.task_dir == Path("/groups/abci/kl/jb1/tk3")
    assert t3.output_dir == Path("/groups/abci/kl/jb1/tk3/outputs")
    with pytest.raises(FileNotFoundError, match="target abci"):
        t3.result({"acc": 1.0})
    assert not (d / "tk3").exists()
    t1.result({"acc": 1.0})
    assert (d / "tk1" / "results.score.jsonl").exists()


def test_job_read_on_the_second_target(clean_env, tmp_path):
    """The copy on the --into target reads its own versions locally, the first batch's remotely."""
    d = tmp_path / "abci" / "jb1"
    (d / "tk3").mkdir(parents=True)
    _mixed(d, "/gs/work/kl/jb1", str(d))
    t1, _, t3 = [c.selected for c in runq.experiment(d, producer="score").cells]
    assert (t1.local, t1.task_dir) == (False, Path("/gs/work/kl/jb1/tk1"))
    assert (t3.local, t3.task_dir) == (True, d / "tk3")


def test_job_without_ref_inside_a_task_reads_its_own_job(clean_env, job_dir, monkeypatch):
    monkeypatch.setenv("RUNQ_TASK_DIR", str(job_dir / "tk1"))
    assert runq.experiment(producer="score").id == "jb1"


def test_job_without_ref_outside_a_task_asks_for_a_path(clean_env):
    with pytest.raises(ValueError, match="experiment directory"):
        runq.experiment(producer="score")


def test_job_missing_snapshot_explains(clean_env, tmp_path):
    with pytest.raises(FileNotFoundError, match="--fresh"):
        runq.experiment(tmp_path, producer="score")


def test_producer_defaults_to_the_script_name(clean_env, job_dir, monkeypatch):
    monkeypatch.setattr(sys, "argv", ["/home/me/eval/score_math.py", str(job_dir)])
    job = runq.experiment(job_dir)
    [c.selected for c in job.cells][0].result({"acc": 0.5})
    assert (job_dir / "tk1" / "results.score_math.jsonl").exists()


@pytest.mark.parametrize("bad", ["bad name", "a/b", "", "über"])
def test_producer_is_validated(clean_env, job_dir, bad):
    with pytest.raises(ValueError, match="producer"):
        runq.experiment(job_dir, producer=bad)


def test_producer_needs_a_script(clean_env, job_dir, monkeypatch):
    monkeypatch.setattr(sys, "argv", ["-c"])
    with pytest.raises(ValueError, match="producer="):
        runq.experiment(job_dir)


def test_task_record_matches_runq_record(clean_env, job_dir, tmp_path, monkeypatch):
    """Same writer: identical lines (but ts) as runq.record in a task."""
    task = tmp_path / "self"
    task.mkdir()
    monkeypatch.setenv("RUNQ_TASK_ID", "self")
    monkeypatch.setenv("RUNQ_TASK_DIR", str(task))
    monkeypatch.setenv("RUNQ_METRICS_FILE", str(task / "metrics.jsonl"))
    runq.context()
    with pytest.warns(UserWarning, match="dropped"):
        runq.result({"acc": 0.5, "bad": float("nan")}, section="math", split="test", step=100)
    with pytest.warns(UserWarning, match="dropped"):
        runq.experiment(job_dir, producer="score").cells[0].selected.result(
            {"acc": 0.5, "bad": float("nan")}, section="math", split="test", step=100)
    own, scored = _lines(task / "results.jsonl"), _lines(job_dir / "tk1" / "results.score.jsonl")
    for rec in own + scored:
        rec.pop("ts")
    assert own == scored
    assert scored[0]["metrics"] == {"acc": 0.5}


def test_task_record_freezes_schema_across_handles_and_restarts(clean_env, job_dir):
    t = runq.experiment(job_dir, producer="score").cells[0].selected
    t.result({"acc": 0.5}, section="math", split="test")
    # A second handle in the same process shares the frozen keys.
    with pytest.warns(UserWarning, match="keys changed"):
        runq.experiment(job_dir, producer="score").cells[0].selected.result({"acc": 0.6, "f1": 0.1}, section="math", split="test")
    # A restarted script recovers them from the file.
    from runq import _result as _record
    _record._reset_for_tests()
    with pytest.warns(UserWarning, match="keys changed"):
        runq.experiment(job_dir, producer="score").cells[0].selected.result({"f1": 0.2}, section="math", split="test")
    recs = _lines(job_dir / "tk1" / "results.score.jsonl")
    assert [r["metrics"] for r in recs] == [{"acc": 0.5}, {"acc": 0.6}, {"acc": None}]
    # Producers are separate sources: another producer starts fresh.
    runq.experiment(job_dir, producer="judge").cells[0].selected.result({"f1": 0.3}, section="math", split="test")
    assert _lines(job_dir / "tk1" / "results.judge.jsonl")[0]["metrics"] == {"f1": 0.3}


def test_task_record_without_context(clean_env, job_dir):
    """A scoring script is not a task: no runq.context() needed."""
    runq.experiment(job_dir, producer="score").cells[1].selected.result({"acc": 0.1})
    assert _lines(job_dir / "tk2" / "results.score.jsonl")[0]["metrics"] == {"acc": 0.1}


def test_job_id_resolves_through_the_service_socket(clean_env, job_dir, monkeypatch):
    detail = {"job": {"id": "jb1", "target": "local"}, "data_dir": str(job_dir)}

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            body = json.dumps(detail if self.path == "/api/v1/jobs/jb1" else {"error": "no job"}).encode()
            self.send_response(200 if self.path == "/api/v1/jobs/jb1" else 404)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def address_string(self):
            return "unix"

        def log_message(self, *a):
            pass

    sock_dir = tempfile.mkdtemp(dir="/tmp")
    sock = os.path.join(sock_dir, "s")
    server = socketserver.UnixStreamServer(sock, Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        monkeypatch.setenv("RUNQ_SOCKET", sock)
        assert runq.experiment("jb1", producer="score").dir == job_dir
        with pytest.raises(LookupError, match="no job"):
            runq.experiment("jb9", producer="score")
        detail["data_dir"] = "/nonexistent/jb1"
        with pytest.raises(FileNotFoundError, match="on target local"):
            runq.experiment("jb1", producer="score")
    finally:
        server.shutdown()
        server.server_close()
        os.unlink(sock)
        os.rmdir(sock_dir)


def test_job_id_without_service_says_pass_a_path(clean_env, monkeypatch, tmp_path):
    monkeypatch.setenv("RUNQ_SOCKET", str(tmp_path / "nope.sock"))
    with pytest.raises(FileNotFoundError, match="pass the experiment directory"):
        runq.experiment("jb1", producer="score")


def test_cells_and_versions(clean_env, tmp_path):
    d = tmp_path / "ex1"
    snap = _snapshot(d)
    snap["version"] = 2
    t1, t2 = snap["tasks"]
    t1.update(version=1, selected=True)
    t2.update(version=1, selected=False)
    t3 = dict(t2, id="tk3", status="success", version=2, selected=True, batch_seq=3,
              task_dir=f"{d}/tk3", output_dir=f"{d}/tk3/outputs", checkpoint_dir=f"{d}/tk3/checkpoints")
    snap["tasks"].append(t3)
    for t in ("tk1", "tk2", "tk3"):
        (d / t).mkdir(parents=True)
    (d / "job.json").write_text(json.dumps(snap))
    exp = runq.experiment(d, producer="score")
    assert [t.id for t in [c.selected for c in exp.cells]] == ["tk1", "tk3"]
    assert [t.id for t in exp.versions] == ["tk1", "tk2", "tk3"]
    assert [(c.name, c.selected.id, [v.version for v in c.versions]) for c in exp.cells] == [("R1", "tk1", [1]), ("R2", "tk3", [1, 2])]


def test_job_is_the_older_name(clean_env, job_dir):
    with pytest.warns(DeprecationWarning, match="runq.experiment"):
        assert runq.job(job_dir, producer="score").id == "jb1"


def test_cell_params_and_older_names(clean_env, job_dir):
    exp = runq.experiment(job_dir, producer="score")
    cell = exp.cells[0]
    assert cell.params == cell.selected.params
    with pytest.warns(DeprecationWarning, match="exp.cells"):
        assert [t.id for t in exp.tasks] == [c.selected.id for c in exp.cells]
    with pytest.warns(DeprecationWarning, match=r"\.result\(\)"):
        cell.selected.record({"acc": 0.5}, section="math")
    from runq import _job
    with pytest.warns(DeprecationWarning, match="Version"):
        assert _job.JobTask is _job.Version
