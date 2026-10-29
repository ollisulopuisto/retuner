#!/usr/bin/env python3
"""Serves RSS feeds of the shapes the podcast parser meets. Logs each request
path and User-Agent to $PL_LOG so two binaries can be compared."""
import os, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1])
LOG = os.environ.get("PL_LOG")
B = "http://127.0.0.1:%d" % PORT


def item(title, url, typ="audio/mpeg", date="Mon, 05 Oct 2026 06:00:00 +0000"):
    t = "<title>%s</title>" % title if title is not None else ""
    e = '<enclosure url="%s" type="%s" length="1"/>' % (url, typ) if url is not None else ""
    d = "<pubDate>%s</pubDate>" % date if date else ""
    return "<item>%s%s%s</item>" % (t, e, d)


def rss(*items):
    return '<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>T</title>%s</channel></rss>' % "".join(items)


PAGES = {
    "/good.xml": rss(
        item("First", B + "/a.mp3"),
        item("Second, with comma & more", B + "/b.mp3", date="Sun, 04 Oct 2026 06:00:00 GMT"),
        item("Video one", B + "/v.mp4", typ="video/mp4"),
        item("No enclosure", None),
        item(None, B + "/c.mp3", typ="", date=""),
        item("Secure", "https://127.0.0.1:%d/secure.mp3" % PORT, date="5 Oct 2026"),
        item("Short date", B + "/d.mp3", date="soon"),
        item("Quote \"q\" <&>", B + "/e.mp3"),
    ),
    "/many.xml": rss(*[item("Ep %d" % i, B + "/m%d.mp3" % i) for i in range(1, 140)]),
    "/empty.xml": rss(),
    "/noaudio.xml": rss(item("v", B + "/v.mp4", typ="video/mp4")),
    "/bad.xml": "<rss><channel><item>",
    "/notrss.xml": "<html><body>hi</body></html>",
}


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if LOG:
            with open(LOG, "a") as f:
                f.write("%s UA=%s\n" % (self.path, self.headers.get("User-Agent", "")))
        body = PAGES.get(self.path.split("?")[0])
        if body is None:
            self.send_response(404); self.send_header("Content-Length", "0"); self.end_headers(); return
        b = body.encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/rss+xml")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)


ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
