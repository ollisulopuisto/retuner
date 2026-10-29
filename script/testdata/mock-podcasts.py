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


def feed(body, head='<?xml version="1.0" encoding="UTF-8"?>'):
    return head + '<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>T</title>' + body + "</channel></rss>"


# Markup the two XML parsers could read differently. Values are str (UTF-8) or bytes.
PAGES.update({
    "/x-cdata.xml": feed('<item><title><![CDATA[CDATA <b>bold</b> & more]]></title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B),
    "/x-entities.xml": feed('<item><title>A &amp; B &lt;c&gt; &#233; &#x263A; &quot;q&quot; &apos;s&apos;</title><enclosure url="%s/a.mp3?x=1&amp;y=2" type="audio/mpeg"/></item>' % B),
    "/x-ns.xml": feed('<item><itunes:title>NS title</itunes:title><title>Plain</title><itunes:duration>12</itunes:duration><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B),
    "/x-image.xml": '<?xml version="1.0"?><rss><channel><image><title>IMG</title><url>x</url></image><title>Chan</title><item><title>Only</title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item></channel></rss>' % B,
    "/x-bom.xml": b"\xef\xbb\xbf" + feed('<item><title>BOM</title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B).encode(),
    "/x-latin1.xml": feed('<item><title>caf\xe9 \xfc</title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B, head='<?xml version="1.0" encoding="ISO-8859-1"?>').encode("latin-1") if False else b'<?xml version="1.0" encoding="ISO-8859-1"?><rss><channel><title>T</title><item><title>caf\xe9 \xfc</title><enclosure url="' + B.encode() + b'/a.mp3" type="audio/mpeg"/></item></channel></rss>',
    "/x-badutf8.xml": b'<?xml version="1.0" encoding="UTF-8"?><rss><channel><title>T</title><item><title>bad \xff\xfe bytes</title><enclosure url="' + B.encode() + b'/a.mp3" type="audio/mpeg"/></item></channel></rss>',
    "/x-single.xml": feed("<item><title>Single</title><enclosure type='audio/mpeg' url='%s/a.mp3'/></item>" % B),
    "/x-upper.xml": feed('<ITEM><TITLE>Upper</TITLE><ENCLOSURE url="%s/a.mp3" type="audio/mpeg"/></ITEM>' % B),
    "/x-comment.xml": feed('<!-- c --><?pi x?><item><title>Cm<!-- inner -->t</title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B),
    "/x-doctype.xml": feed('<item><title>Doc</title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B, head='<?xml version="1.0"?><!DOCTYPE rss [<!ENTITY e "ent">]>'),
    "/x-two-enc.xml": feed('<item><title>Two</title><enclosure url="%s/a.mp3" type="audio/mpeg"/><enclosure url="%s/b.mp3" type="audio/mpeg"/></item>' % (B, B)),
    "/x-ws.xml": feed('<item><title>\n   spaced   title \t</title><enclosure url="  %s/a.mp3  " type=" audio/mpeg "/></item>' % B),
    "/x-longtitle.xml": feed('<item><title>%s</title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % ("L" * 5000, B)),
    "/x-nested.xml": feed('<item><title>Outer<item><title>Inner</title></item></title><enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B),
    "/x-unclosed.xml": feed('<item><title>Unclosed<enclosure url="%s/a.mp3" type="audio/mpeg"/></item>' % B),
    "/x-atom.xml": '<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Atom</title><link rel="enclosure" href="%s/a.mp3" type="audio/mpeg"/></entry></feed>' % B,
})


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
        b = body if isinstance(body, bytes) else body.encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/rss+xml")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)


ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
