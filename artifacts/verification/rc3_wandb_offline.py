"""Pinned W&B offline smoke; no login, network upload or real scheduler submit.

Run with the isolated W&B 0.23.1 environment, passing a disposable root:
 .cache/verification/wandb-0.23.1-venv/bin/python artifacts/verification/rc3_wandb_offline.py /absolute/root
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys
import time


def child():
    import runq
    import wandb

    assert wandb.__version__ == "0.23.1"
    ctx = runq.context()
    assert Path.cwd() != ctx.task_dir
    product = ctx.output_dir / "result.txt"
    product.write_text("task output")
    checkpoint = ctx.checkpoint_dir / "model.bin"
    checkpoint.write_bytes(b"checkpoint")
    run = wandb.init(**ctx.wandb_like_cfg, settings=wandb.Settings(quiet=True))
    ctx.link_wandb(run)
    result = {"id": run.id, "root": str(ctx.wandb_dir), "rank": int(os.environ["RANK"])}
    if result["rank"] == 0:
        run_dir = Path(run.dir).resolve()
        assert run_dir.is_relative_to(ctx.wandb_dir.resolve() / "wandb"), run_dir
        artifact = wandb.Artifact("smoke-result", type="result")
        artifact.add_file(str(product))
        staging = Path(os.environ["WANDB_DATA_DIR"])
        # add_file copies the upload's source into W&B's separate staging area.
        assert any(p.is_file() for p in staging.rglob("*")), staging
        run.log_artifact(artifact)
        run.log({"score": 1}, step=0)
        result.update(run_dir=str(run_dir), sync_dir=run.settings.sync_dir,
                      staging=str(staging), cache=os.environ["WANDB_CACHE_DIR"])
    run.finish()
    assert not ctx.links_file.exists(), "offline/disabled run should not publish a guessed URL"
    print(json.dumps(result))


def main():
    if len(sys.argv) > 2 and sys.argv[2] == "child":
        child()
        return
    root = Path(sys.argv[1]).resolve()
    code = root / "code"
    code.mkdir(parents=True, exist_ok=True)
    shared_cache = root / "shared-cache"
    records = []
    script = str(Path(__file__).resolve())
    for task, rank, override in [("task-a", 0, False), ("task-b", 0, False),
                                 ("task-a", 0, False), ("task-c", 0, True),
                                 ("task-rank-8", 8, False)]:
        task_dir = root / task
        wandb_root = root / "override-root" if override else task_dir
        task_dir.mkdir(parents=True, exist_ok=True)
        config = task_dir / "wandb_config.json"
        config.write_text(json.dumps({"project": "runq-rc3-offline", "mode": "offline"}))
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(("RUNQ_", "WANDB_"))}
        env.update(RUNQ_TASK_ID=task, RUNQ_JOB_ID="offline-fixture", RUNQ_TASK_DIR=str(task_dir),
                   RUNQ_NO_DAEMON="1", RUNQ_METRICS_FILE=str(task_dir / "metrics.jsonl"),
                   RUNQ_CHECKPOINT_DIR=str(task_dir / "checkpoints"),
                   RUNQ_OUTPUT_DIR=str(task_dir / "outputs"), RUNQ_WANDB_CONFIG_FILE=str(config),
                   WANDB_DIR=str(wandb_root), WANDB_DATA_DIR=str(task_dir / "artifact-staging"),
                   WANDB_CACHE_DIR=str(shared_cache), WANDB_CONFIG_DIR=str(root / "config"),
                   WANDB_MODE="offline", RANK=str(rank), LOCAL_RANK="0")
        proc = subprocess.run([sys.executable, script, str(root), "child"], cwd=code,
                              env=env, text=True, capture_output=True, timeout=45)
        if proc.returncode:
            raise RuntimeError(proc.stdout + proc.stderr)
        records.append(json.loads(proc.stdout.strip().splitlines()[-1]))
        time.sleep(1.05)  # W&B's offline bundle names include a second-resolution stamp.
    assert records[0]["sync_dir"] != records[1]["sync_dir"]
    assert records[0]["id"] == records[2]["id"] == "task-a"
    assert records[0]["sync_dir"] != records[2]["sync_dir"]
    assert Path(records[0]["sync_dir"]).is_dir(), "retry erased first offline bundle"
    assert Path(records[2]["sync_dir"]).is_dir()
    assert records[3]["root"] == str(root / "override-root")
    assert not (code / "wandb").exists(), "W&B fell back into project code directory"
    assert shared_cache.is_dir()
    report = {"wandb_version": "0.23.1", "mode": "offline", "records": records,
              "covered": ["two-task isolation", "same-id retry bundle retention", "explicit root",
                          "global rank 8 disabled with local rank 0", "artifact staging and shared cache"],
              "not_covered": ["real online resume", "online URL ingestion", "artifact download", "official sync upload"]}
    (root / "report.json").write_text(json.dumps(report, indent=2))
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
