"""The commercial GA proof must fail closed, and say CHECK for known non-pass states.

Why this file exists
--------------------
`scripts/commercial-ga-proof.sh` runs daily on main. It used to pass two things
that prove no purchase:

- its signup API check accepted any 2xx-4xx, and GET /v1/signup is a 404
  whether signup works or not (the only route there is POST), so it reported
  `PASS signup api shell - HTTP 404` every day;
- its checkout check followed Dhanam's 302 to the sales booking page and
  counted that page's 200 as "checkout reachable".

The script now fails on a 404/5xx/no answer, and reports a checkout that lands
on the booking page as a named CHECK: not a pass, and not a failure either.
A failed scheduled run on main is the "latest main run" that
check_main_ci.py reads, so a standing known state that exited 1 would hold the
critical MainCIFailingForTooLong page open every day.

The tests run the real script against a local stub of the API, app, landing
page, Dhanam checkout and booking page. Nothing here talks to a real host.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts/commercial-ga-proof.sh"

pytestmark = pytest.mark.skipif(
    shutil.which("curl") is None or shutil.which("bash") is None,
    reason="needs bash and curl",
)


class Stub:
    """Mutable per-test behaviour of the stub server."""

    def __init__(self) -> None:
        self.port = 0
        self.signup_page = (200, "<html>signup</html>")
        self.signup_status = (400, '{"error":"invalid signup id"}')
        # "booking" → 302 to the booking host; "hosted" → 200 checkout page;
        # an int → that status code.
        self.checkout: str | int = "booking"
        self.metering: str | None = "not_running"


def make_handler(stub: Stub):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):  # keep pytest output clean
            pass

        def _send(self, code: int, body: str, ctype: str = "application/json", headers=None):
            raw = body.encode()
            self.send_response(code)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(raw)))
            for k, v in (headers or {}).items():
                self.send_header(k, v)
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):  # noqa: N802
            path = self.path.split("?", 1)[0]
            if path == "/health/public":
                return self._send(200, '{"ok": true}')
            if path == "/landing":
                return self._send(200, "<html>pricing Essentials Pro</html>", "text/html")
            if path == "/signup":
                code, body = stub.signup_page
                return self._send(code, body, "text/html")
            if path == "/v1/signup/ga-proof/status":
                code, body = stub.signup_status
                return self._send(code, body)
            if path == "/v1/signup":
                return self._send(404, "404 page not found", "text/plain")
            if path == "/checkout":
                if stub.checkout == "booking":
                    loc = f"http://localhost:{stub.port}/madfam?product=enclii"
                    return self._send(302, "", "text/html", {"Location": loc})
                if stub.checkout == "hosted":
                    return self._send(200, "<html>checkout</html>", "text/html")
                return self._send(int(stub.checkout), "error", "text/plain")
            if path == "/madfam":
                return self._send(200, "<html>book a call</html>", "text/html")
            if path == "/v1/projects/demo/billing/cost":
                body = {"total_cents": 0, "period_start": "a", "period_end": "b"}
                if stub.metering is not None:
                    body["metering"] = {"status": stub.metering}
                    body["spend_known"] = stub.metering == "ok"
                return self._send(200, json.dumps(body))
            if path == "/v1/projects/demo/billing/budgets":
                return self._send(200, '{"budgets": []}')
            if path == "/v1/projects/demo/billing/throttles":
                return self._send(200, '{"throttles": []}')
            if path == "/v1/admin/db/schema":
                return self._send(200, '{"healthy": true}')
            if path == "/v1/dashboard/stats":
                auth = self.headers.get("Authorization")
                return self._send(200 if auth else 401, "{}")
            return self._send(404, "not found", "text/plain")

        def do_POST(self):  # noqa: N802
            length = int(self.headers.get("Content-Length") or 0)
            self.rfile.read(length)
            if self.path == "/v1/providers/resend/credentials":
                return self._send(200, '{"apiKeyPresent": true}')
            return self._send(404, "not found", "text/plain")

    return Handler


@pytest.fixture()
def stub():
    s = Stub()
    server = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(s))
    s.port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    yield s
    server.shutdown()
    server.server_close()


def run_proof(stub: Stub, tmp_path: Path, *, token: bool = True, strict: bool = False, api_port=None):
    base = f"http://127.0.0.1:{stub.port}"
    api = f"http://127.0.0.1:{api_port}" if api_port else base
    summary = tmp_path / "proof.json"
    step_summary = tmp_path / "step-summary.md"
    env = {
        "PATH": os.environ["PATH"],
        "HOME": str(tmp_path),
        "API_URL": api,
        "APP_URL": base,
        "LANDING_URL": f"{base}/landing",
        "TIMEOUT": "5",
        "ENCLII_COMMERCIAL_GA_STRICT": "true" if strict else "false",
        "SUMMARY_PATH": str(summary),
        "DHANAM_CHECKOUT_URL": f"{base}/checkout?plan=enclii_pro&product=enclii",
        "CHECKOUT_HOSTS": f"127.0.0.1:{stub.port}",
        "SALES_BOOKING_HOSTS": f"localhost:{stub.port}",
        "GITHUB_ACTIONS": "true",
        "GITHUB_STEP_SUMMARY": str(step_summary),
    }
    if token:
        env["ENCLII_SYNTHETICS_TOKEN"] = "fixture-token"  # pragma: allowlist secret
        env["ENCLII_SYNTHETIC_PROJECT_SLUG"] = "demo"
    proc = subprocess.run(
        ["bash", str(SCRIPT)], env=env, capture_output=True, text=True, timeout=120, cwd=tmp_path
    )
    data = json.loads(summary.read_text())
    return proc, data, step_summary.read_text() if step_summary.exists() else ""


def statuses(data) -> dict[str, str]:
    return {c["name"]: c["status"] for c in data["checks"]}


def test_current_reality_is_check_not_pass_and_exits_zero(stub, tmp_path):
    # Production today: signup works, checkout lands on the booking page, and
    # the aggregator is not running.
    proc, data, step = run_proof(stub, tmp_path)
    assert proc.returncode == 0, proc.stdout + proc.stderr
    s = statuses(data)
    assert s["signup api live"] == "pass"
    assert s["checkout routes to sales booking (hosted checkout off in production)"] == "check"
    assert "Dhanam checkout reachability" not in s, "a booking page must never be reported as checkout reachable"
    assert s["billing metering not_running (spend unknown)"] == "check"
    assert data["failed"] == 0
    assert data["checks_pending"] == 2
    assert data["verdict"] == "not_proven"
    assert "CHECK checkout routes to sales booking" in proc.stdout
    assert "::warning title=CHECK checkout routes to sales booking" in proc.stdout
    assert "NOT PROVEN" in step and "| CHECK |" in step


def test_strict_mode_does_not_turn_check_into_failure(stub, tmp_path):
    proc, data, _ = run_proof(stub, tmp_path, strict=True)
    assert proc.returncode == 0, proc.stdout
    assert data["failed"] == 0 and data["checks_pending"] == 2


def test_hosted_checkout_on_and_metering_ok_is_proven(stub, tmp_path):
    stub.checkout = "hosted"
    stub.metering = "ok"
    proc, data, step = run_proof(stub, tmp_path)
    assert proc.returncode == 0, proc.stdout
    s = statuses(data)
    assert s["Dhanam checkout reachability"] == "pass"
    assert s["billing metering"] == "pass"
    assert data["verdict"] == "proven", data
    assert "PROVEN" in step


def test_signup_api_404_fails(stub, tmp_path):
    stub.signup_status = (404, '{"error":"not found"}')
    proc, data, _ = run_proof(stub, tmp_path)
    assert proc.returncode == 1
    assert statuses(data)["signup api live"] == "fail"
    assert data["verdict"] == "failed"
    assert "::error title=FAIL signup api live" in proc.stdout


@pytest.mark.parametrize("code", [500, 503])
def test_signup_api_5xx_fails(stub, tmp_path, code):
    stub.signup_status = (code, "oops")
    proc, data, _ = run_proof(stub, tmp_path)
    assert proc.returncode == 1
    assert statuses(data)["signup api live"] == "fail"


def test_signup_api_400_without_handler_marker_fails(stub, tmp_path):
    # A 400 from something other than the signup handler is not proof.
    stub.signup_status = (400, "bad request")
    proc, data, _ = run_proof(stub, tmp_path)
    assert proc.returncode == 1
    assert statuses(data)["signup api live"] == "fail"


@pytest.mark.parametrize("code", [404, 401, 502])
def test_signup_page_non_2xx_fails(stub, tmp_path, code):
    stub.signup_page = (code, "nope")
    proc, data, _ = run_proof(stub, tmp_path)
    assert proc.returncode == 1
    assert statuses(data)["signup page shell"] == "fail"


@pytest.mark.parametrize("code", [404, 500])
def test_checkout_error_fails(stub, tmp_path, code):
    stub.checkout = code
    proc, data, _ = run_proof(stub, tmp_path)
    assert proc.returncode == 1
    assert statuses(data)["Dhanam checkout reachability"] == "fail"


def test_checkout_on_unknown_host_fails(stub, tmp_path):
    # 200 on a host that is neither the checkout nor the booking page.
    stub.checkout = "hosted"
    base = f"http://127.0.0.1:{stub.port}"
    env_proc = subprocess.run(
        ["bash", str(SCRIPT)],
        env={
            "PATH": os.environ["PATH"],
            "HOME": str(tmp_path),
            "API_URL": base,
            "APP_URL": base,
            "LANDING_URL": f"{base}/landing",
            "TIMEOUT": "5",
            "SUMMARY_PATH": str(tmp_path / "other.json"),
            "DHANAM_CHECKOUT_URL": f"{base}/checkout",
            "CHECKOUT_HOSTS": "app.dhan.am",
            "SALES_BOOKING_HOSTS": "kalya.app",
        },
        capture_output=True,
        text=True,
        timeout=120,
        cwd=tmp_path,
    )
    other = json.loads((tmp_path / "other.json").read_text())
    assert env_proc.returncode == 1
    assert statuses(other)["Dhanam checkout reachability"] == "fail"
    assert "unexpected host" in {c["name"]: c["detail"] for c in other["checks"]}["Dhanam checkout reachability"]


def test_metering_unreported_is_check(stub, tmp_path):
    stub.metering = None  # an API build that predates the metering block
    proc, data, _ = run_proof(stub, tmp_path)
    assert proc.returncode == 0
    assert statuses(data)["billing metering not reported (spend unverified)"] == "check"


def test_unreachable_api_fails(stub, tmp_path):
    # A port nothing listens on: every API read is HTTP 000, which must fail.
    import socket

    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    dead_port = sock.getsockname()[1]
    sock.close()
    proc, data, _ = run_proof(stub, tmp_path, token=False, api_port=dead_port)
    assert proc.returncode == 1
    s = statuses(data)
    assert s["api health"] == "fail"
    assert s["signup api live"] == "fail"
