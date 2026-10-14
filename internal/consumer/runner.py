"""Restartable consumer session. Run on the worker; no controller RPCs."""
from contextlib import contextmanager
import fcntl
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parent


def write(name, value):
    tmp = ROOT / (name + ".tmp." + str(os.getpid()))
    tmp.write_text(json.dumps(value), encoding="utf-8")
    os.replace(tmp, ROOT / name)


def read_state():
    try:
        return json.loads((ROOT / "status.json").read_text())
    except FileNotFoundError:
        return {}


@contextmanager
def locked():
    # All lifecycle writers use this shared-filesystem lock. Never unlink it:
    # flock is released by the OS even if a launcher or watcher is killed.
    with (ROOT / "lifecycle.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def identity(pid):
    if pid <= 0:
        return None
    if Path("/proc").exists():
        try:
            parts = (Path("/proc") / str(pid) / "stat").read_text().rsplit(")", 1)[1].split()
        except (FileNotFoundError, ProcessLookupError):
            return None
        return None if parts[0] == "Z" else parts[19]
    result = subprocess.run(["ps", "-p", str(pid), "-o", "lstart=", "-o", "stat="], capture_output=True, text=True)
    value = result.stdout.strip()
    if result.returncode == 1 and not value:
        return None
    if result.returncode:
        raise RuntimeError("cannot inspect process: " + result.stderr)
    return None if not value or value.split()[-1].startswith("Z") else " ".join(value.split()[:-1])


def group_members(state):
    pid = state.get("pid", 0)
    if not pid:
        return {}
    token = identity(pid)
    if token is not None and token != state.get("pid_start"):
        return {}  # PID has been reused; this is not our process group.
    listing = subprocess.run(["ps", "-axo", "pid=,pgid="], capture_output=True, text=True, check=True)
    members = {}
    for line in listing.stdout.splitlines():
        child, group = map(int, line.split())
        if group == state.get("pgid", pid):
            start = identity(child)
            if start is not None:
                members[child] = start
    return members


def watch(run_id):
    cancelled_at = None
    while True:
        with locked():
            state = read_state()
            if state.get("run_id") != run_id:
                return  # A previous watcher must never write into a new session.
            members = group_members(state)
            request = ROOT / "cancel.request"
            cancelled = request.exists() and json.loads(request.read_text()).get("run_id") == run_id
            if cancelled and members:
                if cancelled_at is None:
                    cancelled_at = time.monotonic()
                    sig = signal.SIGTERM
                elif time.monotonic() - cancelled_at >= 30:
                    sig = signal.SIGKILL
                else:
                    sig = None
                if sig is not None:
                    try:
                        os.killpg(state["pgid"], sig)
                    except ProcessLookupError:
                        pass
            if not members:
                code = None
                try:
                    code = json.loads((ROOT / ("exit." + run_id + ".json")).read_text())["exit_code"]
                except (OSError, ValueError, KeyError):
                    pass
                state.update(status="killed" if cancelled else ("stopped" if code == 0 else "failed" if code is not None else "lost"),
                             exit_code=code, finished_at=int(time.time()), process_exited=True)
                if code is None and not cancelled:
                    state["reason"] = "process exited; exit code unknown"
                write("status.json", state)
                return
            state["last_heartbeat_at"] = int(time.time())
            for key, name in [("last_output_at", "log.txt"), ("last_data_at", "results.jsonl")]:
                try:
                    state[key] = int((ROOT / name).stat().st_mtime)
                except FileNotFoundError:
                    pass
            write("status.json", state)
        # Check cancellation promptly, publishing the normal heartbeat every minute.
        for _ in range(60):
            time.sleep(1)
            if request.exists() or identity(state["pid"]) != state["pid_start"]:
                break


def launch(confirm_stopped=False):
    with locked():
        previous = read_state()
        if previous.get("pid"):
            details = "host={host} pid={pid}".format(**previous)
            if not previous.get("process_exited") and time.time() - previous.get("last_heartbeat_at", 0) <= 180:
                raise SystemExit("consumer heartbeat is fresh; refusing duplicate start: " + details)
            if previous.get("host") == socket.gethostname():
                if group_members(previous):
                    raise SystemExit("consumer process group is still alive: " + details)
            elif not previous.get("process_exited") and not confirm_stopped:
                raise SystemExit("consumer exit is unconfirmed: " + details +
                                 "; inspect the old host/allocation, then rerun run.sh --confirm-stopped only after confirming the whole process group has ended")
        command = (ROOT / "command.sh").read_text()
        os.setpgid(0, 0)
        pid = os.getpid()
        token = identity(pid)
        if token is None:
            raise SystemExit("cannot establish process identity")
        run_id = uuid.uuid4().hex
        # Business output and ingest offsets survive. Only execution evidence resets.
        for name in ("cancel.request", "exit.json"):
            (ROOT / name).unlink(missing_ok=True)
        for entry in ROOT.glob("exit.*.json"):
            entry.unlink(missing_ok=True)
        write("status.json", {"status": "running", "run_id": run_id, "host": socket.gethostname(), "pid": pid, "pgid": pid,
                              "pid_start": token, "started_at": int(time.time()), "last_heartbeat_at": int(time.time())})
        os.environ["RUNQ_CONSUMER_RUN_ID"] = run_id
        with (ROOT / "watcher.log").open("ab", buffering=0) as log:
            subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "watch", run_id],
                             stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True, close_fds=True)
    os.execv("/bin/bash", ["bash", "-c", command])


def cancel():
    with locked():
        state = read_state()
        if state.get("pid") and not state.get("process_exited"):
            write("cancel.request", {"requested_at": int(time.time()), "run_id": state["run_id"]})
        else:
            state.update(status="killed", finished_at=int(time.time()), process_exited=True, reason="cancelled while stopped")
            write("status.json", state)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "watch":
        watch(sys.argv[2])
    elif len(sys.argv) > 1 and sys.argv[1] == "cancel":
        cancel()
    elif sys.argv[1:] in ([], ["--confirm-stopped"]):
        launch(confirm_stopped=bool(sys.argv[1:]))
    else:
        raise SystemExit("usage: run.sh [--confirm-stopped]")
