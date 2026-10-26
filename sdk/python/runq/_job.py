"""``runq.job()`` — read a job, write results for its tasks.

A scoring script (or any script that is not the task itself) reads a job's
tasks and records results for each of them::

    import runq

    job = runq.job("/work/kl/jb7817…")       # the job directory
    for t in job.tasks:
        if t.status != "success":
            continue
        acc = score(t.output_dir / "preds.jsonl")
        t.record({"acc": acc}, section="math", split="test")

Reads ``<job_dir>/job.json``, which runq writes at submit and whenever a
refresh sees the job change. ``t.record`` is ``runq.record`` for that task:
same validation, same schema freezing, written to
``<task_dir>/results.<producer>.jsonl`` (producer: the script's file name
unless given). runq ingests those files like the task's own results.jsonl;
views tell producers apart.

Run the script where the job directory is readable (the cluster's login
node for an HPC target). No runq service is needed, except to resolve a
``jb…`` id instead of a path.
"""
from __future__ import annotations

import http.client
import json
import os
import re
import socket
import sys
from dataclasses import dataclass, field
from pathlib import Path

from ._record import _writer

_PRODUCER = re.compile(r"^[A-Za-z0-9_.-]+$")
_JOB_ID = re.compile(r"^jb[0-9a-zA-Z]+$")


@dataclass(frozen=True)
class JobTask:
    """One task of a job, as of the job.json snapshot."""

    id: str
    run_name: str
    status: str
    params: dict
    task_dir: Path
    output_dir: Path
    checkpoint_dir: Path
    started_at: int | None
    finished_at: int | None
    last_output_at: int | None
    batch_seq: int
    producer: str = field(repr=False)

    @property
    def results_file(self) -> Path:
        return self.task_dir / f"results.{self.producer}.jsonl"

    def record(self, metrics: dict | None = None, *, section: str | None = None, **axes) -> None:
        """``runq.record`` for this task, written as this script's results."""
        _writer(self.results_file).write(metrics, section, axes, stacklevel=3)


@dataclass(frozen=True)
class Job:
    """A job read from its job.json (``runq.job()``)."""

    id: str
    name: str
    project: str
    status: str
    target: str
    created_at: int | None
    dir: Path
    tasks: list[JobTask]
    updated_at: int | None
    observed_at: int | None
    observation_state: str


def job(ref: str | os.PathLike | None = None, *, producer: str | None = None) -> Job:
    """Read a job: ``ref`` is its directory (the folder holding job.json).

    Inside a runq task, ``runq.job()`` with no ref reads the task's own job.
    A ``jb…`` id also works when the runq service is reachable from here
    (the local socket, or ``RUNQ_SOCKET``).

    ``producer`` names this script's results (``results.<producer>.jsonl``);
    it defaults to the script's file name without ``.py`` and may use
    letters, digits, ``_``, ``.`` and ``-``.
    """
    producer = _producer(producer)
    job_dir = _job_dir(ref)
    path = job_dir / "job.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise FileNotFoundError(
            f"runq.job: {path} does not exist — runq writes it at submit and on refresh; "
            "for an older job run `runq ps <jb…> --fresh` once"
        ) from None
    except ValueError as e:
        raise ValueError(f"runq.job: {path} is not valid JSON: {e}") from None
    if not isinstance(data, dict) or not isinstance(data.get("tasks"), list):
        raise ValueError(f"runq.job: {path} is not a job snapshot")
    head = data.get("job") or {}
    return Job(
        id=head.get("id", ""),
        name=head.get("name", ""),
        project=head.get("project", ""),
        status=head.get("status", ""),
        target=head.get("target", ""),
        created_at=head.get("created_at"),
        dir=job_dir,
        tasks=[_task(job_dir, t, producer) for t in data["tasks"]],
        updated_at=data.get("updated_at"),
        observed_at=data.get("observed_at"),
        observation_state=data.get("observation_state", ""),
    )


def _task(job_dir: Path, t: dict, producer: str) -> JobTask:
    # job.json records target paths. Rebase them on the directory actually
    # read, so a job read through another mount point still writes in place.
    recorded = t.get("task_dir") or t["id"]
    task_dir = job_dir / Path(recorded).name

    def under(key: str, default: str) -> Path:
        raw = t.get(key)
        if raw:
            try:
                return task_dir / Path(raw).relative_to(recorded)
            except ValueError:
                return Path(raw)
        return task_dir / default

    return JobTask(
        id=t["id"],
        run_name=t.get("run_name", ""),
        status=t.get("status", ""),
        params=dict(t.get("params") or {}),
        task_dir=task_dir,
        output_dir=under("output_dir", "outputs"),
        checkpoint_dir=under("checkpoint_dir", "checkpoints"),
        started_at=t.get("started_at"),
        finished_at=t.get("finished_at"),
        last_output_at=t.get("last_output_at"),
        batch_seq=t.get("batch_seq", 1),
        producer=producer,
    )


def _producer(name: str | None) -> str:
    if name is None:
        script = sys.argv[0] if sys.argv else ""
        name = Path(script).stem if script not in ("", "-c", "-") else ""
        if not name:
            raise ValueError("runq.job: no script name to use as producer; pass producer=\"...\"")
    if not isinstance(name, str) or not _PRODUCER.match(name):
        raise ValueError(
            f"runq.job: producer {name!r} must be letters, digits, '_', '.' or '-' "
            "(it names results.<producer>.jsonl)"
        )
    return name


def _job_dir(ref) -> Path:
    if ref is None:
        task_dir = os.environ.get("RUNQ_TASK_DIR")
        if not task_dir:
            raise ValueError("runq.job: pass the job directory (the folder holding job.json)")
        return Path(task_dir).parent
    path = Path(ref)
    if path.name == "job.json" and path.is_file():
        return path.parent
    if path.is_dir() or not _JOB_ID.match(str(ref)):
        return path
    return _resolve_job_id(str(ref))


def _socket_path() -> str:
    if os.environ.get("RUNQ_SOCKET"):
        return os.environ["RUNQ_SOCKET"]
    data = os.environ.get("RUNQ_DATA_DIR")
    if not data:
        root = getattr(os, "geteuid", lambda: 1)() == 0
        data = "/var/lib/runq" if root else str(Path.home() / ".local/share/runq")
    return str(Path(data) / "runq.sock")


class _UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path: str):
        super().__init__("localhost", timeout=10)
        self._path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self._path)


def _resolve_job_id(job_id: str) -> Path:
    sock = _socket_path()
    try:
        conn = _UnixHTTP(sock)
        conn.request("GET", f"/api/v1/jobs/{job_id}")
        resp = conn.getresponse()
        body = resp.read()
    except OSError as e:
        raise FileNotFoundError(
            f"runq.job({job_id!r}): no runq service at {sock} ({e}); pass the job directory instead"
        ) from None
    if resp.status != 200:
        raise LookupError(f"runq.job({job_id!r}): {body.decode(errors='replace').strip()}")
    detail = json.loads(body)
    data_dir = detail.get("data_dir")
    if not data_dir:
        raise FileNotFoundError(f"runq.job({job_id!r}): the job has no workspace directory")
    path = Path(data_dir)
    if not path.is_dir():
        target = (detail.get("job") or {}).get("target", "?")
        raise FileNotFoundError(
            f"runq.job({job_id!r}): {path} is on target {target}; run this script there "
            "(e.g. the cluster login node) and pass that path"
        )
    return path
