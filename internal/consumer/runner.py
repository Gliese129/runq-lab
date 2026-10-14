"""One execution per consumer ID. Run on the worker; no controller RPCs."""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent

def write(name, value):
    tmp = ROOT / (name + ".tmp." + str(os.getpid()))
    tmp.write_text(json.dumps(value), encoding="utf-8")
    os.replace(tmp, ROOT / name)

def identity(pid):
    try:
        proc = Path("/proc") / str(pid) / "stat"
        if Path("/proc").exists():
            parts = proc.read_text().rsplit(")", 1)[1].split()
            if parts[0] == "Z":
                return None
            return parts[19]
        result = subprocess.run(["ps", "-p", str(pid), "-o", "lstart=", "-o", "stat="], capture_output=True, text=True)
        text = result.stdout.strip()
        if result.returncode or not text or text.split()[-1].startswith("Z"):
            return None
        return " ".join(text.split()[:-1])
    except (OSError, ValueError):
        return None

def watch(pid, token):
    state = json.loads((ROOT / "status.json").read_text())
    cancelled_at = None
    members = {}
    while True:
        alive = identity(pid) == token
        if (ROOT / "cancel.request").exists() and alive:
            if cancelled_at is None:
                # Keep start identities for children that outlive their leader.
                listing = subprocess.run(["ps", "-axo", "pid=,pgid="], capture_output=True, text=True, check=True)
                for line in listing.stdout.splitlines():
                    child, group = map(int, line.split())
                    if group == pid:
                        start = identity(child)
                        if start is not None:
                            members[child] = start
                cancelled_at = time.monotonic()
                try:
                    os.killpg(pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
        if cancelled_at is not None:
            survivors = {p: start for p, start in members.items() if identity(p) == start}
            alive = bool(survivors)
            if alive and time.monotonic() - cancelled_at >= 30:
                # A matching member proves this is still the original group.
                try:
                    os.killpg(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        if not alive:
            code = None
            try:
                code = json.loads((ROOT / "exit.json").read_text())["exit_code"]
            except (OSError, ValueError, KeyError):
                pass
            state.update(status="killed" if cancelled_at is not None else ("stopped" if code == 0 else "failed" if code is not None else "lost"),
                         exit_code=code, finished_at=int(time.time()))
            if code is None and cancelled_at is None:
                state["reason"] = "process disappeared; exit code unknown"
            write("status.json", state)
            return
        state["last_heartbeat_at"] = int(time.time())
        for key, name in [("last_output_at", "log.txt"), ("last_data_at", "results.jsonl")]:
            try:
                state[key] = int((ROOT / name).stat().st_mtime)
            except FileNotFoundError:
                pass
        write("status.json", state)
        # One second cancellation checks; heartbeat publication once per minute.
        for _ in range(60):
            time.sleep(1)
            if (ROOT / "cancel.request").exists() or identity(pid) != token:
                break

def launch():
    try:
        (ROOT / "started.lock").mkdir()
    except FileExistsError:
        try:
            details = (ROOT / "status.json").read_text()
        except OSError:
            details = "startup reserved; execution identity not yet published"
        raise SystemExit("consumer already started; submit a new consumer ID: " + details)
    if (ROOT / "cancel.request").exists():
        write("status.json", {"status": "killed", "finished_at": int(time.time()), "reason": "cancelled before launch"})
        raise SystemExit(0)
    os.setpgid(0, 0)
    pid = os.getpid()
    token = identity(pid)
    if token is None:
        raise SystemExit("cannot establish process identity")
    write("status.json", {"status": "running", "host": socket.gethostname(), "pid": pid, "pgid": pid,
                          "pid_start": token, "started_at": int(time.time()), "last_heartbeat_at": int(time.time())})
    with (ROOT / "watcher.log").open("ab", buffering=0) as log:
        subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "watch", str(pid), token],
                         stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True, close_fds=True)
    command = (ROOT / "command.sh").read_text()
    os.execv("/bin/bash", ["bash", "-c", command])

def cancel():
    write("cancel.request", {"requested_at": int(time.time())})
    try:
        (ROOT / "started.lock").mkdir()
    except FileExistsError:
        return  # The worker watcher owns cancellation after launch reservation.
    write("status.json", {"status": "killed", "finished_at": int(time.time()), "reason": "cancelled before launch"})

if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "watch":
        watch(int(sys.argv[2]), sys.argv[3])
    elif len(sys.argv) > 1 and sys.argv[1] == "cancel":
        cancel()
    else:
        launch()
