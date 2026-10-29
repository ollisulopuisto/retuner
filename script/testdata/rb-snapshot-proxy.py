#!/usr/bin/env python3
"""Plain-HTTP snapshot proxy for the real Radio Browser API (opt-in, read-only).
The Pascal binary needs an OpenSSL it may not find, so both binaries talk to
this proxy over http. The first request for a path is fetched live from
UPSTREAM (default https://de1.api.radio-browser.info) and stored; every later
request, from either binary, gets the stored bytes, so both see the same data.
  rb-snapshot-proxy.py PORT [LOGFILE]"""
import sys, urllib.request, http.server, os
UP = os.environ.get("UPSTREAM", "https://de1.api.radio-browser.info")
store = {}
log = open(sys.argv[2], "a") if len(sys.argv) > 2 else None
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if log: log.write(self.path + "\n"); log.flush()
        if self.path not in store:
            try:
                req = urllib.request.Request(UP + self.path, headers={"User-Agent": self.headers.get("User-Agent", "Retuner")})
                with urllib.request.urlopen(req, timeout=60) as r:
                    store[self.path] = (r.status, r.read())
            except Exception as e:
                self.send_error(502, str(e)); return
        st, b = store[self.path]
        self.send_response(st); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
