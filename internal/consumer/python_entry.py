"""Execute Python user code in the process observed by the consumer watcher."""
import json
import os
from pathlib import Path
import runpy
import signal
import sys

root = Path(os.environ["RUNQ_CONSUMER_DIR"])
code = 0

def terminate(signum, frame):
    raise SystemExit(128 + signum)

signal.signal(signal.SIGTERM, terminate)
try:
    args = sys.argv[1:]
    while args and args[0] in ("-u", "-B"):
        args.pop(0)
    if not args:
        raise RuntimeError("consumer Python command requires a script, -m module or -c code")
    if args[0] == "-m":
        sys.argv = args[1:]
        runpy.run_module(args[1], run_name="__main__", alter_sys=True)
    elif args[0] == "-c":
        sys.argv = ["-c"] + args[2:]
        exec(compile(args[1], "<string>", "exec"), {"__name__": "__main__"})
    else:
        sys.argv = args
        sys.path.insert(0, str(Path(args[0]).resolve().parent))
        runpy.run_path(args[0], run_name="__main__")
except SystemExit as exc:
    code = exc.code if isinstance(exc.code, int) else 0 if exc.code is None else 1
    raise
except BaseException:
    code = 1
    raise
finally:
    name = "exit." + os.environ["RUNQ_CONSUMER_RUN_ID"] + ".json"
    temp = root / (name + ".tmp." + str(os.getpid()))
    temp.write_text(json.dumps({"exit_code": code}))
    os.replace(temp, root / name)
