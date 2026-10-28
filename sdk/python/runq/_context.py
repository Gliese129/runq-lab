"""SDK Context object + initialization.

Two modes:

* task   — RUNQ_TASK_ID set: runq started this process; every write goes to
  files in the task directory, which runq reads.
* manual — RUNQ_TASK_ID unset (running outside runq, local debug); files go
  to the working directory.

The module-level singleton `_ctx` is set by `context()` and consumed by
other SDK modules (through the internal `get_ctx()`).
"""
from __future__ import annotations

import difflib
import hashlib
import json
import os
import sys
import uuid
import warnings
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

# Module-level singleton. None until context() is called.
_ctx: Context | None = None


_NO_DEFAULT = object()


class ParamDict(dict):
    """Dict subclass for parameter access.

    ``params["kl"]`` means the param is required: a missing key raises with
    the available keys and close matches (catches "sweep says
    ``learning_rate``, code says ``lr``").

    ``params.get("kl", 0.0)`` must name its default. The default is what the
    task runs with whenever a job does not set the param, so it belongs in
    the code, in plain sight; a bare ``get("kl")`` would silently yield None
    and change behaviour without anyone deciding it.
    """

    def get(self, key, default=_NO_DEFAULT):
        if default is _NO_DEFAULT:
            raise TypeError(
                f"runq: ctx.params.get({key!r}) needs an explicit default, e.g. "
                f"ctx.params.get({key!r}, 0.0) — it is what this task uses when the job "
                f"does not set {key!r}. Use ctx.params[{key!r}] if the param is required."
            )
        return super().get(key, default)

    def __getitem__(self, key):
        try:
            return super().__getitem__(key)
        except KeyError:
            available = sorted(self.keys())
            close = difflib.get_close_matches(str(key), available, n=3, cutoff=0.5)
            msg = f"'{key}' not found in runq.params.\nAvailable: {available}"
            if close:
                msg += f"\nDid you mean: {close}?"
            raise KeyError(msg) from None


@dataclass
class Context:
    """Per-task runtime context.

    Populated from `RUNQ_*` env vars (set by runq for the task) plus a
    local `params.json`. Lazy users mostly interact via module-level helpers
    (`runq.log`, `runq.result`, ...) which pull state from the
    singleton; this struct is the canonical home of all SDK state.

    Fields are kept descriptive on purpose — debugging from `print(ctx)`
    should be enough to see where the SDK thinks it lives.
    """

    mode: str                                 # "task" / "manual"
    task_id: str
    experiment_id: str = ""                   # ex… (RUNQ_EXPERIMENT_ID)
    cell: str = ""                            # the cell's name (RUNQ_CELL)
    version: int = 1                          # this version's number in its cell
    project: str = ""

    # Filesystem paths (None when the relevant env var wasn't set).
    task_dir: Path | None = None
    checkpoint_dir: Path | None = None
    output_dir: Path | None = None
    wandb_dir: Path | None = None
    metrics_file: Path | None = None
    # results.jsonl (runq.result data plane) lives NEXT TO metrics.jsonl;
    # derived from metrics_file rather than env-injected, so an older
    # runq that only sets RUNQ_METRICS_FILE still yields the right path.
    results_file: Path | None = None
    # links.json (runq.link sink: name → URL, e.g. the W&B run page).
    links_file: Path | None = None

    # Loaded from RUNQ_PARAMS_FILE (or local params.json in manual mode).
    params: dict = field(default_factory=dict)

    # ---- step bookkeeping (step 6) ----
    #
    # Single source of truth for "what step is this?". Maintained by:
    # - ``log(metrics, step=N)`` — an explicit step writes back here.
    #
    # When None, log writes step=None to jsonl. There is
    # NO auto-increment counter: pass step explicitly when you want one.
    current_step: int | None = None

    # Deterministic seed derived from the experiment and cell (else the
    # task id): different per cell, the same for every version of a cell.
    seed: int = 0

    # ---- convenience accessors ----

    @property
    def job_id(self) -> str:
        """Deprecated: renamed ``ctx.experiment_id``."""
        warnings.warn("ctx.job_id is renamed ctx.experiment_id", DeprecationWarning, stacklevel=2)
        return self.experiment_id

    # ---- wandb-like shape helpers (F9) ----
    #
    # SDK never imports wandb. These properties produce dicts shaped to
    # be splatted into ``wandb.init(**...)`` / merged into
    # ``wandb.log(...)``. User owns the wandb calls themselves.
    #
    # Kept inline (rather than delegated to a sibling module) so the
    # SDK has no ``wandb`` import anywhere.

    @property
    def wandb_like_cfg(self) -> dict:
        """Kwargs dict for ``wandb.init(**ctx.wandb_like_cfg)``.

        Shape::

            {
                "project": <project wandb.project> or runq project or "runq",
                "entity":  <project wandb.entity>          # only when configured
                "tags":    <project wandb.tags> + sweep keys
                "mode":    <project wandb.mode>; "disabled" on RANK > 0
                "id":      task_id,  "resume": "allow",    # retries continue one run
                "name":    run name ("R1 lr", "kl=0.1, seed=2") or f"{experiment_id}/{task_id}",
                "group":   experiment name or experiment_id,
                "config":  dict(params),
            }

        Project-level ``wandb:`` settings arrive via ``RUNQ_WANDB_CONFIG_FILE``.
        Only rank 0 reports (``RANK`` unset counts as 0), so a multi-GPU task
        is one run; W&B's system monitor on rank 0 covers every GPU on that
        node. Every key is a default — pass your own to override, e.g.
        ``wandb.init(**{**ctx.wandb_like_cfg, "id": my_id})``. Then call
        ``runq.link("wandb", run)`` so runq shows the run on the task.
        The SDK never imports wandb.
        """
        sweep_keys_raw = os.environ.get("RUNQ_SWEEP_KEYS", "")
        sweep_keys = [k.strip() for k in sweep_keys_raw.split(",") if k.strip()] if sweep_keys_raw else []
        # RUNQ_JOB_NOTE: tasks submitted before rc6.
        job_name = os.environ.get("RUNQ_JOB_NAME") or os.environ.get("RUNQ_JOB_NOTE", "")

        run_name = os.environ.get("RUNQ_CELL") or os.environ.get("RUNQ_RUN_NAME", "")
        version = os.environ.get("RUNQ_VERSION", "")
        if run_name:
            # A cell's later versions are "<cell> #N"; the W&B id stays the task id.
            name = f"{run_name} #{version}" if version not in ("", "1") else run_name
        elif sweep_keys and self.params:
            parts = [f"{k}={self.params[k]}" for k in sweep_keys if k in self.params]
            name = "_".join(parts) if parts else (f"{self.experiment_id}/{self.task_id}" if self.experiment_id else self.task_id)
        else:
            name = f"{self.experiment_id}/{self.task_id}" if self.experiment_id else self.task_id

        group = job_name if job_name else (self.experiment_id or None)
        project_cfg = _load_wandb_project_cfg()

        cfg: dict[str, Any] = {
            "project": project_cfg.get("project") or self.project or "runq",
            "name": name,
            "group": group,
            "id": self.task_id,
            "resume": "allow",
            "config": dict(self.params or {}),
        }
        if self.wandb_dir is not None:
            cfg["dir"] = str(self.wandb_dir)
        if project_cfg.get("entity"):
            cfg["entity"] = project_cfg["entity"]
        tags = [t for t in project_cfg.get("tags") or [] if isinstance(t, str)]
        tags += [k for k in sweep_keys if k not in tags]
        if tags:
            cfg["tags"] = tags
        if project_cfg.get("mode"):
            cfg["mode"] = project_cfg["mode"]
        if _int_or_zero(os.environ.get("RANK")) > 0:
            cfg["mode"] = "disabled"
        return cfg

    def link_wandb(self, run: Any, *, global_rank: int | None = None) -> None:
        """Deprecated: use ``runq.link("wandb", run)``.

        Records a nonempty run.url from the main process (``global_rank``, or
        RANK, is 0); offline/disabled runs with no URL record nothing.
        """
        warnings.warn(
            'ctx.link_wandb(run) is deprecated; use runq.link("wandb", run)',
            DeprecationWarning,
            stacklevel=2,
        )
        rank = _int_or_zero(os.environ.get("RANK")) if global_rank is None else global_rank
        if rank != 0:
            return
        from ._link import link
        link("wandb", getattr(run, "url", None))

def _load_wandb_project_cfg() -> dict:
    """Project-level ``wandb:`` block (runq writes it per task); {} if absent."""
    path = os.environ.get("RUNQ_WANDB_CONFIG_FILE")
    if not path:
        return {}
    try:
        data = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def _int_or_zero(raw: str | None) -> int:
    try:
        return int(raw) if raw else 0
    except ValueError:
        return 0


def _load_params(path: Path | None) -> dict:
    """Strict params.json loader. Missing file → empty dict; broken file
    → RuntimeError. We surface load failures because silent {} would let
    the user run with the wrong hyperparameters and only notice hours later.
    """
    if path is None or not path.exists():
        return {}
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, json.JSONDecodeError) as e:
        raise RuntimeError(f"runq: failed to load params from {path}: {e}") from e
    if not isinstance(data, dict):
        # Out of the try block on purpose — this is a structural error,
        # not a parse failure, and we want the message to be specific.
        raise RuntimeError(
            f"runq: params file {path} must contain a JSON object, "
            f"got {type(data).__name__}"
        )
    return data


def context() -> Context:
    """Initialize the runq SDK context.

    Idempotent: calling twice returns the same singleton. Reads
    ``RUNQ_*`` env vars, loads ``params.json``, sets up the
    ``metrics.jsonl`` sink, and stores the singleton for other SDK
    modules to consume.

    Returns the :class:`Context`. Lazy users usually do not need to keep
    the return value because module-level helpers (``runq.log``,
    ``runq.result``) read the singleton themselves.

    Modes:

    * ``task`` — started by runq (``RUNQ_TASK_ID`` set). Writes go to files
      in the task directory; ``metrics.jsonl``, ``results.jsonl`` and
      ``links.json`` are created up front, and a filesystem under 5% free
      space or inodes gets one warning on stderr.
    * ``manual`` — user is running their script directly without runq.
      jsonl goes to ``./runq_metrics.jsonl``, task IDs are local UUIDs.
      Useful for IDE debugging.
    """
    global _ctx
    if _ctx is not None:
        return _ctx

    env = os.environ
    mode = "task" if env.get("RUNQ_TASK_ID") else "manual"

    if mode == "manual":
        # Local mode: generate a short id, write jsonl into cwd, look for
        # a sibling params.json (optional — most local debug runs have
        # nothing).
        task_id = "manual-" + uuid.uuid4().hex[:8]
        task_dir: Path | None = None
        checkpoint_dir: Path | None = None
        metrics_file: Path | None = Path.cwd() / "runq_metrics.jsonl"
        results_file: Path | None = Path.cwd() / "runq_results.jsonl"
        links_file: Path | None = Path.cwd() / "runq_links.json"
        local_params = Path.cwd() / "params.json"
        params = _load_params(local_params) if local_params.exists() else {}
    else:
        task_id = env["RUNQ_TASK_ID"]
        task_dir = Path(env["RUNQ_TASK_DIR"]) if env.get("RUNQ_TASK_DIR") else None
        checkpoint_dir = (
            Path(env["RUNQ_CHECKPOINT_DIR"]) if env.get("RUNQ_CHECKPOINT_DIR") else None
        )
        metrics_file = (
            Path(env["RUNQ_METRICS_FILE"]) if env.get("RUNQ_METRICS_FILE") else None
        )
        # Siblings of metrics.jsonl (see Context field comment). runq's reap
        # reads the same fixed names via workspace.ResultsPath/LinksPath.
        results_file = metrics_file.with_name("results.jsonl") if metrics_file else None
        links_file = metrics_file.with_name("links.json") if metrics_file else None
        params_file = env.get("RUNQ_PARAMS_FILE")
        params = _load_params(Path(params_file)) if params_file else {}

    output_raw = env.get("RUNQ_OUTPUT_DIR")
    wandb_raw = env.get("WANDB_DIR")
    output_dir = Path(output_raw) if output_raw else (task_dir / "outputs" if task_dir else None)
    wandb_dir = Path(wandb_raw) if wandb_raw else task_dir

    _ctx = Context(
        mode=mode,
        task_id=task_id,
        experiment_id=env.get("RUNQ_EXPERIMENT_ID") or env.get("RUNQ_JOB_ID", ""),
        cell=env.get("RUNQ_CELL") or env.get("RUNQ_RUN_NAME", ""),
        version=_int_or_zero(env.get("RUNQ_VERSION")) or 1,
        project=env.get("RUNQ_PROJECT_NAME", ""),
        task_dir=task_dir,
        checkpoint_dir=checkpoint_dir,
        output_dir=output_dir,
        wandb_dir=wandb_dir,
        metrics_file=metrics_file,
        results_file=results_file,
        links_file=links_file,
        params=ParamDict(params),
        seed=_seed(env, task_id),
    )

    # Make sure dirs we will write to actually exist. Cheap, idempotent,
    # and prevents a confusing first-write failure mid-training.
    if _ctx.metrics_file is not None:
        _ctx.metrics_file.parent.mkdir(parents=True, exist_ok=True)
    for directory in (_ctx.checkpoint_dir, _ctx.output_dir, _ctx.wandb_dir):
        if directory is not None:
            directory.mkdir(parents=True, exist_ok=True)
    if mode == "task":
        _precreate(_ctx)
        _warn_low_disk(_ctx.task_dir, _ctx.checkpoint_dir, _ctx.output_dir)

    return _ctx


def _precreate(ctx: Context) -> None:
    """Create the task's output files now, while there is room: later appends
    then need no new inode, so a full filesystem mid-run still takes metrics."""
    for path, empty in ((ctx.metrics_file, ""), (ctx.results_file, ""), (ctx.links_file, "{}")):
        if path is None:
            continue
        try:
            with open(path, "x", encoding="utf-8") as f:
                f.write(empty)
        except FileExistsError:
            pass
        except OSError as e:
            print(f"runq: failed to create {path}: {e}", file=sys.stderr)


# Below this share of free space or free inodes a filesystem gets a warning.
_LOW_DISK = 0.05


def _warn_low_disk(*dirs: Path | None) -> None:
    """One stderr warning per filesystem (among ``dirs``) under 5% free."""
    if not hasattr(os, "statvfs"):
        return
    seen: set = set()
    for d in dirs:
        if d is None:
            continue
        try:
            dev = os.stat(d).st_dev
            if dev in seen:
                continue
            seen.add(dev)
            st = os.statvfs(d)
        except OSError:
            continue
        space = st.f_bavail / st.f_blocks if st.f_blocks else 1.0
        inodes = st.f_favail / st.f_files if st.f_files else 1.0  # 0 = not reported
        if min(space, inodes) < _LOW_DISK:
            print(
                f"runq: low on disk at {d}: {space:.1%} space and {inodes:.1%} inodes free "
                "— checkpoints, outputs and logs may fail to write",
                file=sys.stderr,
            )


def get_ctx() -> Context:
    """Return the global context. Raises if ``context()`` was not called.

    Internal — user code keeps the return value of ``context()``.
    """
    if _ctx is None:
        raise RuntimeError(
            "runq: context() must be called before using other runq APIs. "
            "Add `runq.context()` near the top of your script."
        )
    return _ctx


# ---- testing helper ----
#
# Not part of the public API but useful in tests to reset the singleton
# between test cases. Marked with underscore to discourage user calls.
def _reset_for_tests() -> None:
    global _ctx
    _ctx = None


def _seed(env, task_id: str) -> int:
    """sha256 of "<experiment>/<cell>" (or the task id), mod 2**32."""
    exp = env.get("RUNQ_EXPERIMENT_ID") or env.get("RUNQ_JOB_ID", "")
    cell = env.get("RUNQ_CELL") or env.get("RUNQ_RUN_NAME", "")
    key = f"{exp}/{cell}" if exp and cell else task_id
    return int(hashlib.sha256(key.encode()).hexdigest(), 16) % (2**32)
