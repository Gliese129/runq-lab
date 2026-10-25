# examples/

Copy-paste starting points for runq configs. Every file is fully annotated —
reading them IS the schema reference (also see
[docs/configuration.md](../docs/configuration.md)).

| You want to... | Copy |
|---|---|
| Submit your first sweep with minimum ceremony | [`job_simple.yaml`](./job_simple.yaml) |
| See every job.yaml feature: grid × zip blocks, name, overrides | [`job.yaml`](./job.yaml) |
| See every project.yaml feature: constraints, mandatory choices, env | [`project.yaml`](./project.yaml) |
| Run model × benchmark evals on an HPC cluster, each benchmark with its own walltime | [`hpc/`](./hpc/) |
| A training script that reads params via the SDK and feeds the dashboard charts | [`test_train.py`](./test_train.py) |

Quick start:

```bash
cp project.yaml job_simple.yaml ~/my-code/ && cd ~/my-code   # edit both
runq project add -f project.yaml
runq submit job_simple.yaml --dry   # preview the exact expansion — free
runq submit job_simple.yaml --json
```

Two rules that explain most of the schema:

- **project.yaml is the code base** (where it lives, how it runs, its
  environment); **job.yaml is one experiment** (command/target if different,
  `fixed_params`, `sweep`). New experiment = new job file, not a new project.
  Defaults for params live in the training code (`ctx.params.get(...)`).
- **`grid` = cartesian product, `list` = zip 1-to-1.** Values that belong
  together (benchmark + its walltime, model + its parser) go in one `list`
  block; independent dimensions go in `grid`. Blocks combine by
  cross-product.
