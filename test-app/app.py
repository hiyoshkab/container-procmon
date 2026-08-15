import multiprocessing
import threading
import time

from flask import Flask, jsonify, request

app = Flask(__name__)


def _burn_cpu(deadline: float) -> None:
    """Busy-loop until the monotonic deadline is reached."""
    while time.monotonic() < deadline:
        pass


def _hold_memory(size_bytes: int, seconds: float) -> None:
    """Allocate size_bytes, touch every page so it's resident, then release."""
    block = bytearray(size_bytes)
    # Write one byte per 4 KiB page to force the pages to be committed (counted in RSS).
    for i in range(0, size_bytes, 4096):
        block[i] = 1
    time.sleep(seconds)
    del block


def _positive_int(name: str, default: int, maximum: int) -> int:
    raw = request.values.get(name, default)
    try:
        value = int(raw)
    except (TypeError, ValueError):
        raise ValueError(f"'{name}' must be an integer")
    if value < 1:
        raise ValueError(f"'{name}' must be >= 1")
    if value > maximum:
        raise ValueError(f"'{name}' must be <= {maximum}")
    return value


@app.errorhandler(ValueError)
def _handle_value_error(err: ValueError):
    return jsonify(error=str(err)), 400

@app.route("/", methods=["GET", "POST"])
def index():
    return jsonify(
        endpoints={
            "/cpu": "burn CPU for a number of seconds. Use seconds and workers query parameters to control the load. example: /cpu?seconds=10&workers=4",
            "/mem": "allocate memory for a number of seconds. Use seconds and mb query parameters to control the load. example: /mem?seconds=10&mb=128",
            "/health": "check if the service is healthy"
        },
    )

@app.route("/cpu", methods=["GET", "POST"])
def cpu():
    seconds = _positive_int("seconds", default=10, maximum=3600)
    workers = _positive_int("workers", default=1, maximum=multiprocessing.cpu_count())

    deadline = time.monotonic() + seconds
    # Separate processes are required to load multiple cores past the GIL.
    procs = [multiprocessing.Process(target=_burn_cpu, args=(deadline,)) for _ in range(workers)]
    for p in procs:
        p.start()
    threading.Thread(target=lambda: [p.join() for p in procs], daemon=True).start()

    return jsonify(status="started", load="cpu", seconds=seconds, workers=workers)


@app.route("/mem", methods=["GET", "POST"])
def mem():
    seconds = _positive_int("seconds", default=10, maximum=3600)
    mb = _positive_int("mb", default=128, maximum=8192)

    threading.Thread(
        target=_hold_memory, args=(mb * 1024 * 1024, seconds), daemon=True
    ).start()

    return jsonify(status="started", load="mem", seconds=seconds, mb=mb)


@app.route("/health")
def health():
    return jsonify(status="ok")


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
