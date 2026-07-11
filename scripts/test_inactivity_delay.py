#!/usr/bin/env python3
"""Remote reconnect test for the native macOS client and direct-buffer server."""

import json
import os
import signal
import subprocess
import sys
import time
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
LOG_DIR = ROOT / "test-results" / "reconnect-review"
CLIENT_BIN = ROOT / "macos" / "LLrdc.app" / "Contents" / "MacOS" / "llrdc-client"

REMOTE_HOST = os.environ.get("LLRDC_REMOTE_HOST", "nzxt5")
REMOTE_DIR = os.environ.get("LLRDC_REMOTE_DIR", "~/code/LLrdc")
IMAGE_TAG = os.environ.get("LLRDC_IMAGE_TAG", "latest")
CONTAINER = os.environ.get("LLRDC_CONTAINER", "llrdc-reconnect-test")
CLIENT_TIMEOUT = os.environ.get("CLIENT_TIMEOUT", "10")
SERVER_URL = os.environ.get("LLRDC_SERVER_URL", "http://nzxt5:8080")
CONTROL_URL = os.environ.get("LLRDC_CONTROL_URL", "http://127.0.0.1:18080")
MAX_FIRST_PRESENT_SECONDS = float(os.environ.get("LLRDC_MAX_FIRST_PRESENT_SECONDS", "8"))


def run(command, *, check=True, capture=False):
    return subprocess.run(command, check=check, text=True, capture_output=capture)


def ssh(command, *, check=True, capture=False):
    return run(["ssh", REMOTE_HOST, command], check=check, capture=capture)


def get_json(path):
    try:
        with urllib.request.urlopen(f"{CONTROL_URL}{path}", timeout=1.0) as response:
            return json.loads(response.read().decode())
    except Exception:
        return {}


def wait_for_presentation(process, timeout=30):
    started_at = time.monotonic()
    deadline = time.monotonic() + timeout
    connected_at = None
    wt_at = None
    while time.monotonic() < deadline:
        if process.poll() is not None:
            break
        state = get_json("/statez")
        stats = get_json("/statsz")
        now = time.monotonic()
        if connected_at is None and state.get("connected"):
            connected_at = now
        if wt_at is None and state.get("webtransportConnected"):
            wt_at = now
        if state.get("presenting") and stats.get("presentedFrames", 0) > 0:
            return {
                "firstPresentationSeconds": now - started_at,
                "connectedSeconds": None if connected_at is None else now - connected_at,
                "webtransportSeconds": None if wt_at is None else now - wt_at,
                "state": state,
                "stats": stats,
            }
        time.sleep(0.1)
    return None


def stop_client(process):
    if process.poll() is not None:
        return
    process.send_signal(signal.SIGKILL)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def start_client(label):
    log_path = LOG_DIR / f"client-{label}.log"
    log_file = log_path.open("w")
    process = subprocess.Popen(
        [
            str(CLIENT_BIN),
            "--server",
            SERVER_URL,
            "--control-addr",
            "127.0.0.1:18080",
            "--stats",
            "--auto-start",
        ],
        stdout=log_file,
        stderr=subprocess.STDOUT,
    )
    return process, log_file


def main():
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    if not CLIENT_BIN.is_file():
        print(f"Missing macOS client: {CLIENT_BIN}", file=sys.stderr)
        return 1

    ssh(f"docker rm -f {CONTAINER}", check=False, capture=True)
    ssh(
        f"cd {REMOTE_DIR} && IMAGE_TAG={IMAGE_TAG} CLIENT_TIMEOUT={CLIENT_TIMEOUT} "
        f"./docker-run.sh --nvidia --direct-buffer --detach --name {CONTAINER}",
        capture=True,
    )

    process = None
    log_file = None
    try:
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(f"{SERVER_URL}/healthz", timeout=1.0):
                    break
            except Exception:
                time.sleep(1)
        else:
            print("Server did not become ready", file=sys.stderr)
            return 1

        process, log_file = start_client("reconnect")
        first = wait_for_presentation(process)
        if first is None:
            print("Initial presentation failed", file=sys.stderr)
            return 1
        if first["firstPresentationSeconds"] > MAX_FIRST_PRESENT_SECONDS:
            print("Initial presentation exceeded the latency threshold", file=sys.stderr)
            return 1
        print(json.dumps({"initial": first}, default=str))

        stop_client(process)
        process = None
        if CLIENT_TIMEOUT != "0":
            time.sleep(int(CLIENT_TIMEOUT) + 2)
            server_state = ssh(f"docker logs {CONTAINER}", check=False, capture=True)
            if "Pausing streaming due to client inactivity timeout" not in server_state.stdout:
                print("Server did not enter the inactivity pause", file=sys.stderr)
                return 1

        if log_file is not None:
            log_file.close()
            log_file = None

        process, log_file = start_client("reconnect-2")
        second = wait_for_presentation(process)
        if second is None:
            print("Reconnect presentation failed", file=sys.stderr)
            return 1
        if second["firstPresentationSeconds"] > MAX_FIRST_PRESENT_SECONDS:
            print("Reconnect presentation exceeded the latency threshold", file=sys.stderr)
            return 1
        print(json.dumps({"reconnect": second}, default=str))

        if second["state"].get("lastError"):
            print(f"Reconnect reported client error: {second['state']['lastError']}", file=sys.stderr)
            return 1
        return 0
    finally:
        if process is not None:
            stop_client(process)
        if log_file is not None:
            log_file.close()
        server_log = LOG_DIR / "server-inactivity-test.log"
        with server_log.open("w") as output:
            result = ssh(f"docker logs --timestamps {CONTAINER}", check=False, capture=True)
            output.write(result.stdout)
            output.write(result.stderr)
        ssh(f"docker rm -f {CONTAINER}", check=False)


if __name__ == "__main__":
    raise SystemExit(main())
