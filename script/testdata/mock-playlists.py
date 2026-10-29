#!/usr/bin/env python3
"""Serves playlists of every shape the resolver handles. Logs each request path
(with the User-Agent) to $PL_LOG so two binaries can be compared."""
import os, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1])
LOG = os.environ.get("PL_LOG")
B = "http://127.0.0.1:%d" % PORT
PAGES = {
    "/a.pls": "[playlist]\nNumberOfEntries=2\nFile1=%s/stream1.mp3\nTitle1=x\nFile2=%s/stream2.mp3\n" % (B, B),
    "/b.m3u": "#EXTM3U\n#EXTINF:1,x\n%s/stream3.mp3\n" % B,
    "/c.asx": "<asx version=\"3.0\"><entry><ref href=\"%s/stream4.mp3\"/></entry></asx>" % B,
    "/d.xspf": "<playlist><trackList><track><location>%s/stream5.mp3</location></track></trackList></playlist>" % B,
    "/nest.m3u": "%s/a.pls\n" % B,
    "/nest2.m3u": "%s/nest.m3u\n" % B,
    "/empty.m3u": "# nothing here\n",
    "/self.m3u": "%s/self.m3u\n" % B,
    "/big.m3u": "#" + "x" * 70000 + "\n%s/late.mp3\n" % B,
}


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if LOG:
            with open(LOG, "a") as f:
                f.write(self.path + "\n")
        body = PAGES.get(self.path.split("?")[0])
        if body is None:
            self.send_response(404)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        b = body.encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)


ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
