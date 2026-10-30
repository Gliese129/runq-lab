# Setup: what runq declares for you, and code freshness

Read this when a project has a wrapper script, when you set up the environment,
or before submitting to a target other than the one holding the code.

## Let runq carry the setup

A wrapper script around the command is usually doing things runq can declare.
Moving them into YAML keeps the command a plain `python train.py`, makes each
setting visible in the draft's preview, and applies it the same way to every version:

| Typical wrapper line | Declare it in runq |
|---|---|
| `export HF_HOME=… OMP_NUM_THREADS=…` | project `environment:` (experiment `overrides.env` for one experiment) |
| `source .env`, tokens | `<working_dir>/.env` — sourced at every version start, never stored |
| `cd $PROJECT` | already done — versions start in `working_dir` |
| `module load cuda/…`, cluster profile | the target's `env_setup` in `~/.runq/config.yaml` (runs before every version) |
| `source .venv/bin/activate` | `python_env: { type: uv, path: .venv }` (or `venv`; conda: `{ type: conda, name: … }`) |
| one-off install / model download | `setup_command` (once per submit, on the login node) |
| `mkdir -p $OUT $CKPT` | already done — `ctx.output_dir` / `ctx.checkpoint_dir` exist at start |
| `test -f …`, `python -c 'import x'` | a smoke experiment (same command, small `fixed_params`); `preflight: { hf: [repo] }` for models that must be cached |
| building `--lr $LR --seed $SEED` | `fixed_params` / `sweep`, passed as `{{args}}` (or `ctx.params` for a one-off test) |
| choosing `--resume` | a resume experiment: `fixed_params: { resume_from: <checkpoint path> }`, passed as `--resume_from {{resume_from}}` |
| rsync to another cluster | project `targets:` + `<working_dir>/.rsync` (mirrored on submit) |
| waiting on `qstat` | `runq ps <job> --json` when there is a reason to look |

A few needs have no runq feature yet: a deadline derived from the walltime,
multi-stage prepare→train→eval with skip-if-done, in-version sharding, GPU
telemetry. Implement those in the training code (Python) where possible, and
mention it in your report so the researcher knows where that logic lives.

For scoring alongside an experiment, run a script yourself (login node, or your own
iqrsh/tmux session) that reads it with `runq.experiment("<experiment_dir>")` and
writes results per cell (`cell.selected.result(...)`, with `runq.stats` for pass@k
and errors); see `references/views.md`.

## Code freshness

`submit` runs the project tree as it is on the target at that moment. With
`targets:` + `.rsync`, submitting to a non-source target mirrors the selected files
first; otherwise sync your edits to the target before submitting and say so.
`.rsync` uses rsync include-file semantics: `/src/**` whole tree,
`/configs/**.yaml` files, `/data/` traversal only, `!pattern` always excludes.
