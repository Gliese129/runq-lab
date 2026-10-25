"""SDK Context object + initialization.

Three modes (see design doc §"Mode matrix"):

* daemon     — RUNQ_TASK_ID set + RUNQ_SOCKET_PATH set + RUNQ_NO_DAEMON unset
* no_daemon  — RUNQ_TASK_ID set + (RUNQ_SOCKET_PATH unset OR RUNQ_NO_DAEMON=1)
* manual     — RUNQ_TASK_ID unset (running outside runq, local debug)

The module-level singleton `_ctx` is set by `context()` and consumed by
other SDK modules. Users get the Context back from `context()` and can
also call `get_ctx()` (mainly for internal use).
"""
from __future__ import annotations

import json
import os
import uuid
import difflib
import hashlib
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

    Populated from `RUNQ_*` env vars (daemon-injected) plus a local
    `params.json`. Lazy users mostly interact via module-level helpers
    (`runq.log_metric`, `runq.report`, ...) which pull state from the
    singleton; this struct is the canonical home of all SDK state.

    Fields are kept descriptive on purpose — debugging from `print(ctx)`
    should be enough to see where the SDK thinks it lives.
    """

    mode: str                                 # "daemon" / "no_daemon" / "manual"
    task_id: str
    job_id: str = ""
    project: str = ""

    # Filesystem paths (None when the relevant env var wasn't set).
    task_dir: Path | None = None
    checkpoint_dir: Path | None = None
    output_dir: Path | None = None
    wandb_dir: Path | None = None
    metrics_file: Path | None = None
    # results.jsonl (runq.record data plane) lives NEXT TO metrics.jsonl;
    # derived from metrics_file rather than env-injected, so an older
    # daemon that only sets RUNQ_METRICS_FILE still yields the right path.
    results_file: Path | None = None
    # links.json (runq.link sink: name → URL, e.g. the W&B run page).
    links_file: Path | None = None

    # daemon-only fields
    socket_path: str | None = None

    # Loaded from RUNQ_PARAMS_FILE (or local params.json in manual mode).
    params: dict = field(default_factory=dict)

    # ---- step bookkeeping (step 6) ----
    #
    # Single source of truth for "what step is this?". Maintained by:
    # - ``report(metrics, step=N)`` — explicit step writes back here.
    # - ``log_metric(..., step=N)`` — explicit step writes back here too,
    #   so the two helpers stay in sync.
    #
    # When None, log_metric / report write step=None to jsonl. There is
    # NO auto-increment counter: pass step explicitly when you want one.
    current_step: int | None = None

    # Deterministic seed derived from task_id. Different per task in the
    # same sweep, but reproducible across reruns of the same task.
    seed: int = 0

    # ---- convenience accessors ----

    def job_tasks(self) -> dict:
        """Read the last complete controller snapshot; errors never mean done.

        Returns tasks plus updated_at / observed_at / observation_state.
        The caller decides whether to wait, finish processing or exit.
        """
        root = os.environ.get("RUNQ_CONSUMER_DIR")
        if not root:
            raise RuntimeError("job_tasks() requires a runq consumer")
        with (Path(root) / "job.json").open(encoding="utf-8") as stream:
            snapshot = json.load(stream)
        if not isinstance(snapshot, dict) or not isinstance(snapshot.get("tasks"), list):
            raise ValueError("invalid job task snapshot")
        return snapshot

    def get(self, name: str, default: Any = _NO_DEFAULT) -> Any:
        """Look up a param; the default must be given (see ParamDict.get).

        Equivalent to ``ctx.params.get(name, default)``.
        """
        params = self.params if isinstance(self.params, ParamDict) else ParamDict(self.params)
        return params.get(name, default)

    # ---- wandb-like shape helpers (F9) ----
    #
    # SDK never imports wandb. These properties produce dicts shaped to
    # be splatted into ``wandb.init(**...)`` / merged into
    # ``wandb.log(...)``. User owns the wandb calls themselves.
    #
    # Kept inline (rather than delegated to a sibling module) so the
    # only lazy import is the one ``wandb_like_metric`` needs for
    # ``_report.latest_report_metrics``. No ``wandb`` import anywhere.

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
                "name":    "lr=0.01_bs=32" or f"{job_id}/{task_id}",
                "group":   job name or job_id,
                "config":  dict(params),
            }

        Project-level ``wandb:`` settings arrive via ``RUNQ_WANDB_CONFIG_FILE``.
        Only rank 0 reports (``RANK`` unset counts as 0), so a multi-GPU task
        is one run; W&B's system monitor on rank 0 covers every GPU on that
        node. Every key is a default — pass your own to override, e.g.
        ``wandb.init(**{**ctx.wandb_like_cfg, "id": my_id})``. Then call
        ``ctx.link_wandb(run)`` so runq shows the run on the task.
        The SDK never imports wandb.
        """
        sweep_keys_raw = os.environ.get("RUNQ_SWEEP_KEYS", "")
        sweep_keys = [k.strip() for k in sweep_keys_raw.split(",") if k.strip()] if sweep_keys_raw else []
        # RUNQ_JOB_NOTE: tasks submitted before rc6.
        job_name = os.environ.get("RUNQ_JOB_NAME") or os.environ.get("RUNQ_JOB_NOTE", "")

        if sweep_keys and self.params:
            parts = [f"{k}={self.params[k]}" for k in sweep_keys if k in self.params]
            name = "_".join(parts) if parts else (f"{self.job_id}/{self.task_id}" if self.job_id else self.task_id)
        else:
            name = f"{self.job_id}/{self.task_id}" if self.job_id else self.task_id

        group = job_name if job_name else (self.job_id or None)
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
        """Record a nonempty run.url from the main process, including custom ids.

        Pass the framework's global rank if it is not exported as RANK.
        Offline/disabled runs with no URL leave the existing link untouched.
        The helper never imports W&B or constructs a URL.
        """
        rank = _int_or_zero(os.environ.get("RANK")) if global_rank is None else global_rank
        if rank != 0:
            return
        url = getattr(run, "url", None)
        if isinstance(url, str) and url.strip():
            from ._link import link
            link("wandb", url)

    @property
    def wandb_like_metric(self) -> dict:
        """Latest reported metrics for ``wandb.log({**ctx.wandb_like_metric, ...})``.

        Empty dict if no report has happened yet. Caller merges their
        own extras at the call site::

            wandb.log({**ctx.wandb_like_metric, "lr": lr}, step=epoch)

        The returned dict is a copy — runq's internal history is not
        exposed to mutation.
        """
        # Lazy import to avoid context ↔ report cycle (_report imports
        # from _context at module load).
        from . import _report
        return _report.latest_report_metrics()


# ---- wandb helpers ----

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


# ---- mode detection ----

def _truthy(s: str | None) -> bool:
    """Lenient truthy parse for env vars. Empty / "0" / "false" → False."""
    if not s:
        return False
    return s.strip().lower() not in ("0", "false", "no", "off")


def _detect_mode() -> str:
    """Resolve which mode the SDK is operating in.

    Order matters:
    1. No RUNQ_TASK_ID → user is running outside runq → manual.
    2. RUNQ_NO_DAEMON=1 (explicit HPC opt-out) → no_daemon.
    3. No socket path → daemon isn't reachable → no_daemon.
    4. Otherwise daemon.
    """
    if not (os.environ.get("RUNQ_TASK_ID") or os.environ.get("RUNQ_CONSUMER_ID")):
        return "manual"
    if _truthy(os.environ.get("RUNQ_NO_DAEMON")):
        return "no_daemon"
    if not os.environ.get("RUNQ_SOCKET_PATH"):
        return "no_daemon"
    return "daemon"


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
    the return value because module-level helpers (``runq.log_metric``,
    ``runq.report``) read the singleton themselves.

    Modes:

    * ``daemon`` — talks to a runq daemon over a unix socket.
    * ``no_daemon`` — HPC mode; daemon not reachable. Writes jsonl
      locally.
    * ``manual`` — user is running their script directly without runq.
      jsonl goes to ``./runq_metrics.jsonl``, task IDs are local UUIDs,
      no daemon comm. Useful for IDE debugging.
    """
    global _ctx
    if _ctx is not None:
        return _ctx

    mode = _detect_mode()
    env = os.environ

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
        # daemon / no_daemon: daemon injects all the RUNQ_* vars.
        task_id = env.get("RUNQ_TASK_ID") or env["RUNQ_CONSUMER_ID"]
        task_dir = Path(env["RUNQ_TASK_DIR"]) if env.get("RUNQ_TASK_DIR") else None
        checkpoint_dir = (
            Path(env["RUNQ_CHECKPOINT_DIR"]) if env.get("RUNQ_CHECKPOINT_DIR") else None
        )
        metrics_file = (
            Path(env["RUNQ_METRICS_FILE"]) if env.get("RUNQ_METRICS_FILE") else None
        )
        # Siblings of metrics.jsonl (see Context field comment). The daemon's
        # reap reads the same fixed names via workspace.ResultsPath/LinksPath.
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
        job_id=env.get("RUNQ_JOB_ID", ""),
        project=env.get("RUNQ_PROJECT_NAME", ""),
        task_dir=task_dir,
        checkpoint_dir=checkpoint_dir,
        output_dir=output_dir,
        wandb_dir=wandb_dir,
        metrics_file=metrics_file,
        results_file=results_file,
        links_file=links_file,
        socket_path=env.get("RUNQ_SOCKET_PATH"),
        params=ParamDict(params),
        seed=int(hashlib.sha256(task_id.encode()).hexdigest(), 16) % (2**32),
    )

    # Make sure dirs we will write to actually exist. Cheap, idempotent,
    # and prevents a confusing first-write failure mid-training.
    if _ctx.metrics_file is not None:
        _ctx.metrics_file.parent.mkdir(parents=True, exist_ok=True)
    for directory in (_ctx.checkpoint_dir, _ctx.output_dir, _ctx.wandb_dir):
        if directory is not None:
            directory.mkdir(parents=True, exist_ok=True)

    return _ctx


def get_ctx() -> Context:
    """Return the global context. Raises if ``context()`` was not called.

    Mostly internal — user code usually keeps the return value of
    ``context()`` directly. Exposed for SDK helpers that don't have a
    handle on it.
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
