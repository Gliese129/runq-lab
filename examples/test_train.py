"""Example training script for runq.

Swept values arrive through the SDK — no argparse plumbing — and the same
file runs by hand without runq (ctx.params is then empty, so the defaults
below apply):

    python test_train.py

Under runq (`command: python test_train.py`), every value from the job's
fixed_params / sweep is in ctx.params, metrics go to the dashboard, and one
result row per cell is recorded for comparison.
"""
import math
import random

import runq

ctx = runq.context()
lr = float(ctx.params.get("lr", 0.001))           # defaults live in code
batch_size = int(ctx.params.get("batch_size", 32))
optimizer = ctx.params.get("optimizer", "adam")
epochs = int(ctx.params.get("epochs", 5))

rng = random.Random(f"{lr}-{batch_size}-{optimizer}")
loss = 2.5
for epoch in range(1, epochs + 1):
    # A fake but well-behaved curve: lr and batch size shape the floor.
    floor = 0.3 + abs(math.log10(lr / 0.01)) * 0.1 + (32 / batch_size) * 0.05
    loss = max(floor, loss * 0.7 + rng.uniform(-0.02, 0.02))
    print(f"epoch {epoch}/{epochs} lr={lr} bs={batch_size} opt={optimizer} loss={loss:.4f}")
    runq.report({"loss": loss}, step=epoch)

runq.record({"final_loss": loss}, lr=lr, batch_size=batch_size, optimizer=optimizer)
print("done.")
