#!/usr/bin/env python3
"""Upstream for relay-parity.sh. /ok sends ICY headers and 3000 bytes of audio,
/plain sends audio with no Content-Type, /dead answers 404. Every request is
logged to $RL_LOG with the Icy-MetaData header it carried."""
import os, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOG = os.environ.get("RL_LOG")


class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"

    def log_message(self, *a):
        pass

    def do_GET(self):
        if LOG:
            with open(LOG, "a") as f:
                f.write("%s icy=%s\n" % (self.path, self.headers.get("Icy-MetaData")))
        p = self.path.split("?")[0]
        if p == "/dead":
            self.send_response(404); self.send_header("Content-Length", "0"); self.end_headers(); return
        body = bytes(range(256)) * 12
        self.send_response(200)
        if p == "/ok":
            self.send_header("Content-Type", "audio/aacp")
            self.send_header("icy-name", "Test FM")
            self.send_header("icy-genre", "Pop")
            self.send_header("icy-br", "64")
            self.send_header("icy-metaint", "16000")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
