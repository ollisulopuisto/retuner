"""Tests for script/testdata/mock-release.py.

Verifies that an asset path coming from the HTTP request can never resolve
outside ASSET_DIR, even with path-traversal attempts.
"""

import http.client
import os
import sys
import threading

PORT = 19701

# mock-release.py calls serve() at import time; stub it so importing does not
# block forever. test-update.sh runs it as a script, so production behaviour
# is unchanged.
import localserver

localserver.serve = lambda port, handler: None

import importlib.util

# mock-release.py reads sys.argv at import time; stub it so importing in a test
# does not fail. test-update.sh passes real args when run as a script.
ASSET_DIR = os.path.join(os.path.dirname(__file__), "assets")
_orig_argv = sys.argv
sys.argv = ["mock-release.py", "19701", "1.0.0", ASSET_DIR, "good"]

_spec = importlib.util.spec_from_file_location(
    "mock_release", os.path.join(os.path.dirname(__file__), "mock-release.py")
)
mock_release = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mock_release)
sys.argv = _orig_argv


class QuietHandler(mock_release.Handler):
    def log_message(self, *a):
        pass


def start_server():
    from localserver import LocalHTTPServer

    server = LocalHTTPServer(("127.0.0.1", PORT), QuietHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server


def get(path):
    conn = http.client.HTTPConnection("127.0.0.1", PORT, timeout=5)
    try:
        conn.request("GET", path)
        return conn.getresponse()
    finally:
        conn.close()


def test_traversal_asset_path_is_not_served():
    server = start_server()
    try:
        for evil in [
            "/repos/o/r/releases/download/v1.0.0/../escape.tar.gz",
            "/repos/o/r/releases/download/v1.0.0/../../../etc/passwd",
            "/repos/o/r/releases/download/v1.0.0/%2e%2e/escape.tar.gz",
            "/repos/o/r/releases/download/v1.0.0/././escape.tar.gz",
        ]:
            resp = get(evil)
            assert resp.status == 404, f"{evil!r} -> {resp.status}, expected 404"
            resp.read()
    finally:
        server.shutdown()


def test_legitimate_asset_is_served():
    os.makedirs(ASSET_DIR, exist_ok=True)
    asset = os.path.join(ASSET_DIR, "retuner-1.0.0-darwin-aarch64.tar.gz")
    with open(asset, "wb") as f:
        f.write(b"fake-binary")
    try:
        server = start_server()
        try:
            resp = get(
                "/repos/o/r/releases/download/v1.0.0/"
                "retuner-1.0.0-darwin-aarch64.tar.gz"
            )
            assert resp.status == 200, f"expected 200, got {resp.status}"
            assert resp.read() == b"fake-binary"
        finally:
            server.shutdown()
    finally:
        os.remove(asset)
