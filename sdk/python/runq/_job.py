"""``runq.experiment()`` — read an experiment, write results for its cells.

A scoring script (or any script that is not the task itself) reads an
experiment's cells and writes results for each of them::

    import runq
    from runq import stats

    exp = runq.experiment("/work/kl/ex7817…")   # the experiment directory
    for cell in exp.cells:
        v = cell.selected                      # the version the cell shows
        if v.status != "success":
            continue
        correct = score(v.output_dir / "preds.jsonl")   # bool (problems, samples)
        v.result({"acc": stats.pass_at_k(correct)}, section="math")

``cell.params`` are the selected version's params; ``cell.versions`` (and
``exp.versions``) also hold the versions a cell no longer shows. Reads
``<experiment_dir>/job.json``, which runq writes at submit and whenever a
refresh sees the experiment change. ``v.result`` is ``runq.result`` for that
version: same validation, same schema freezing, written to
``<task_dir>/results.<producer>.jsonl`` (producer: the script's file name
unless given). runq ingests those files like the version's own results.jsonl;
views tell producers apart.

Run the script where the job directory is readable (the cluster's login
node for an HPC target). An experiment that ran on several targets lists
every version in each target's job.json; a version from another target
keeps its recorded paths, has ``v.local`` false, and ``v.result`` raises
naming ``v.target`` — score it on that target. No runq service is needed,
except to resolve an ``ex…`` (or older ``jb…``) id instead of a path. ``runq.job()`` is the
older name of ``runq.experiment()``; ``JobTask``, ``exp.tasks`` and
``.record()`` are the older names of ``Version``, the cells' selected
versions and ``.result()``.
"""
from __future__ import annotations

import http.client
import json
import os
import re
import socket
import sys
import warnings
from dataclasses import dataclass, field
from pathlib import Path

from ._result import _writer

_PRODUCER = re.compile(r"^[A-Za-z0-9_.-]+$")
_JOB_ID = re.compile(r"^(ex|jb)[0-9a-zA-Z]+$")


@dataclass(frozen=True)
class Version:
    """One version of a cell (one execution), as of the job.json snapshot."""

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
    version: int  # its number within its cell (1 = the cell's first)
    selected: bool  # the version its cell shows
    producer: str = field(repr=False)
    target: str = ""  # where it ran
    # Its directory is on the filesystem the experiment was read from. False
    # for another target's version: its paths are that target's, unread here.
    local: bool = True

    @property
    def results_file(self) -> Path:
        return self.task_dir / f"results.{self.producer}.jsonl"

    def result(self, values: dict | None = None, *, section: str | None = None, **axes) -> None:
        """``runq.result`` for this version, written as this script's results."""
        self._writer().write_values(values, section, axes, fn=".result", stacklevel=3)

    def record(self, metrics: dict | None = None, *, section: str | None = None, **axes) -> None:
        """Deprecated: renamed :meth:`result`."""
        warnings.warn(".record() is renamed .result()", DeprecationWarning, stacklevel=2)
        self._writer().write_values(metrics, section, axes, fn=".result", stacklevel=3)

    def _writer(self):
        if not self.local:
            raise FileNotFoundError(
                f"version {self.id} ({self.run_name}) ran on target {self.target}; its directory "
                f"{self.task_dir} is not on this filesystem — run this script on {self.target} "
                "with the experiment directory there"
            )
        return _writer(self.results_file)


def __getattr__(name: str):
    if name == "JobTask":  # the older name of Version
        warnings.warn("JobTask is renamed Version", DeprecationWarning, stacklevel=2)
        return Version
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


@dataclass(frozen=True)
class Cell:
    """One cell: its versions (oldest first) and the one it shows."""

    name: str
    selected: Version
    versions: list[Version]

    @property
    def params(self) -> dict:
        """The selected version's params."""
        return self.selected.params


@dataclass(frozen=True)
class Experiment:
    """An experiment read from its job.json (``runq.experiment()``)."""

    id: str
    name: str
    project: str
    status: str
    target: str
    created_at: int | None
    dir: Path
    versions: list[Version]  # every version of every cell
    updated_at: int | None
    observed_at: int | None
    observation_state: str

    @property
    def tasks(self) -> list[Version]:
        """Deprecated: ``[c.selected for c in exp.cells]``."""
        warnings.warn("exp.tasks is deprecated; use [c.selected for c in exp.cells]",
                      DeprecationWarning, stacklevel=2)
        return [c.selected for c in self.cells]

    @property
    def cells(self) -> list[Cell]:
        """The cells in order of first appearance."""
        by_name: dict[str, list[Version]] = {}
        for t in self.versions:
            by_name.setdefault(t.run_name or t.id, []).append(t)
        out = []
        for name, versions in by_name.items():
            versions = sorted(versions, key=lambda v: v.version)
            chosen = next((v for v in versions if v.selected), versions[-1])
            out.append(Cell(name=name, selected=chosen, versions=versions))
        return out


Job = Experiment  # the older name


def experiment(ref: str | os.PathLike | None = None, *, producer: str | None = None) -> Experiment:
    """Read an experiment: ``ref`` is its directory (the folder holding job.json).

    Inside a runq task, ``runq.experiment()`` with no ref reads the task's
    own experiment. An ``ex…`` (or older ``jb…``) id also works when the runq
    service is reachable from here (the local socket, or ``RUNQ_SOCKET``).

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
            f"runq.experiment: {path} does not exist — runq writes it at submit and on refresh; "
            "for an older experiment run `runq ps <ex…> --fresh` once"
        ) from None
    except ValueError as e:
        raise ValueError(f"runq.experiment: {path} is not valid JSON: {e}") from None
    if not isinstance(data, dict) or not isinstance(data.get("tasks"), list):
        raise ValueError(f"runq.experiment: {path} is not an experiment snapshot")
    head = data.get("job") or {}
    here = _targets_here(job_dir, data["tasks"], head.get("target", ""))
    return Experiment(
        id=head.get("id", ""),
        name=head.get("name", ""),
        project=head.get("project", ""),
        status=head.get("status", ""),
        target=head.get("target", ""),
        created_at=head.get("created_at"),
        dir=job_dir,
        versions=[_task(job_dir, t, producer, head.get("target", ""), here) for t in data["tasks"]],
        updated_at=data.get("updated_at"),
        observed_at=data.get("observed_at"),
        observation_state=data.get("observation_state", ""),
    )


def job(ref: str | os.PathLike | None = None, *, producer: str | None = None) -> Experiment:
    """The older name of :func:`experiment`."""
    warnings.warn("runq.job() is renamed runq.experiment()", DeprecationWarning, stacklevel=2)
    return experiment(ref, producer=producer)


def _targets_here(job_dir: Path, tasks: list, default: str) -> set[str]:
    """The targets whose versions live in job_dir: those with a version
    recorded in it or a version directory present in it (the same directory
    read through another mount point). Falls back to the experiment's target."""
    try:
        present = {p.name for p in job_dir.iterdir()}
    except OSError:
        present = set()
    here = set()
    for t in tasks:
        recorded = Path(t.get("task_dir") or t["id"])
        if recorded.parent == job_dir or recorded.name in present:
            here.add(t.get("target") or default)
    return here or {default}


def _task(job_dir: Path, t: dict, producer: str, default_target: str = "",
          here: set[str] | None = None) -> Version:
    # job.json records target paths. Rebase this target's on the directory
    # actually read, so a job read through another mount point still writes
    # in place; another target's versions keep their recorded paths.
    target = t.get("target") or default_target
    local = here is None or target in here
    recorded = t.get("task_dir") or t["id"]
    task_dir = job_dir / Path(recorded).name if local else Path(recorded)

    def under(key: str, default: str) -> Path:
        raw = t.get(key)
        if raw:
            try:
                return task_dir / Path(raw).relative_to(recorded)
            except ValueError:
                return Path(raw)
        return task_dir / default

    return Version(
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
        version=t.get("version", 1),
        selected=t.get("selected", True),  # a version-1 snapshot lists selected versions only
        producer=producer,
        target=target,
        local=local,
    )


def _producer(name: str | None) -> str:
    if name is None:
        script = sys.argv[0] if sys.argv else ""
        name = Path(script).stem if script not in ("", "-c", "-") else ""
        if not name:
            raise ValueError(
                "runq.experiment: no script name to use as producer; pass producer=\"...\""
            )
    if not isinstance(name, str) or not _PRODUCER.match(name):
        raise ValueError(
            f"runq.experiment: producer {name!r} must be letters, digits, '_', '.' or '-' "
            "(it names results.<producer>.jsonl)"
        )
    return name


def _job_dir(ref) -> Path:
    if ref is None:
        task_dir = os.environ.get("RUNQ_TASK_DIR")
        if not task_dir:
            raise ValueError(
                "runq.experiment: pass the experiment directory (the folder holding job.json)"
            )
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
            f"runq.experiment({job_id!r}): no runq service at {sock} ({e}); "
            "pass the experiment directory instead"
        ) from None
    if resp.status != 200:
        raise LookupError(f"runq.experiment({job_id!r}): {body.decode(errors='replace').strip()}")
    detail = json.loads(body)
    data_dir = detail.get("data_dir")
    if not data_dir:
        raise FileNotFoundError(
            f"runq.experiment({job_id!r}): the experiment has no workspace directory"
        )
    path = Path(data_dir)
    if not path.is_dir():
        target = (detail.get("job") or {}).get("target", "?")
        raise FileNotFoundError(
            f"runq.experiment({job_id!r}): {path} is on target {target}; run this script there "
            "(e.g. the cluster login node) and pass that path"
        )
    return path
