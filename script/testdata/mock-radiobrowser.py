#!/usr/bin/env python3
"""A stand-in for radio-browser.info, for script/test-radiobrowser.sh.

Serves a fixed, deliberately awkward station list: bitrates as numbers, a
station with no bitrate field at all, and one whose bitrate is a string. Those
three shapes are what the filtering code has to survive -- comparing a JSON
number against '' is what used to raise "Invalid variant type cast" and take
the process down with a double free.

    mock-radiobrowser.py <port> [mode]

modes:  ok (default) | empty | malformed

Set RB_LOG to a file path and every request line is appended to it, query
string included. That is the only way to assert on what Retuner *asked* for
rather than what it did with the answer -- ordering is a property of the
upstream query, and it is invisible in the response the mock sends back.
"""
import json
import os
import sys
from http.server import BaseHTTPRequestHandler

from localserver import serve

MODE = sys.argv[2] if len(sys.argv) > 2 else "ok"
REQUEST_LOG = os.environ.get("RB_LOG")

STATIONS = [
    # name, url, bitrate (None = field absent, str = wrong type), codec
    ("Low Bitrate FM",   "http://example.com/low.mp3",    64,    "MP3"),
    ("Mid Bitrate FM",   "http://example.com/mid.mp3",    128,   "MP3"),
    ("Exactly At Limit", "http://example.com/limit.mp3",  192,   "MP3"),
    ("Over The Limit",   "http://example.com/over.mp3",   320,   "MP3"),
    ("No Bitrate Field", "http://example.com/none.mp3",   None,  "MP3"),
    ("String Bitrate",   "http://example.com/str.mp3",    "128", "MP3"),
    # The name carries the blocked word; the URL is clean. Only a filter that
    # tests the name can exclude this one.
    ("Some AAC Station", "http://example.com/clean.mp3",  96,    "MP3"),
    # The mirror image: the URL carries it, the name does not.
    ("Perfectly Fine",   "http://example.com/thing.aac",  96,    "MP3"),
]


def station(i, name, url, bitrate, codec):
    # Field ORDER matters, not just the field names. The UUID list is parsed by
    # splitting on the regex `","serveruuid".*?"stationuuid":"`, so every
    # station must carry serveruuid immediately after stationuuid exactly as
    # the real API emits it. Drop serveruuid and the whole response parses as a
    # single UUID -- which is a fair imitation of what a schema change upstream
    # would do to us, and worth keeping the mock honest about.
    s = {
        "changeuuid": "1111111%d-0000-0000-0000-11111111111%d" % (i, i),
        "stationuuid": "0000000%d-0000-0000-0000-00000000000%d" % (i, i),
        "serveruuid": "2222222%d-0000-0000-0000-22222222222%d" % (i, i),
        "name": name + (" \u00d1andú \u0141ód\u017a \u01c5 \u0191\u00df \u0153 \u00e9\u00e8 caf\u00e9" if os.environ.get("RB_DIAC") else ""),
        "url": url,
        "url_resolved": url,
        "homepage": "http://example.com/",
        "favicon": "",
        "tags": "test",
        "country": "Testland",
        "countrycode": "TL",
        "language": "testish",
        "votes": 10 - i,
        "codec": codec,
        "lastcheckok": 1,
    }
    if bitrate is not None:
        s["bitrate"] = bitrate
    return s


STATION_JSON = [station(i, *row) for i, row in enumerate(STATIONS)]
COUNTRIES = [{"name": "Testland", "stationcount": len(STATIONS), "iso_3166_1": "TL"}]


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, payload):
        if MODE == "malformed":
            body = b'{"this is not": an array'
        elif MODE == "empty":
            body = b"[]"
        else:
            # Compact, exactly as the real API emits it. json.dumps' default
            # ", " separator would put a space inside `","serveruuid"` and the
            # UUID parser, which is a regex over the raw text, would match
            # nothing at all.
            body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if REQUEST_LOG:
            with open(REQUEST_LOG, "a") as fh:
                fh.write(self.path + "\n")
        p = self.path.split("?")[0]
        if p == "/json/countries":
            self._send(COUNTRIES)
        elif p.startswith("/json/stations"):
            self._send(STATION_JSON)
        elif p == "/json/tags":
            self._send([{"name": "test", "stationcount": len(STATIONS)}])
        elif p == "/json/languages":
            self._send([{"name": "testish", "stationcount": len(STATIONS), "iso_639": "ts"}])
        elif p == "/json/codecs":
            self._send([{"name": c, "stationcount": 1} for c in sorted({r[3] for r in STATIONS})])
        elif p == "/json/servers":
            self._send([{"name": "127.0.0.1", "ip": "127.0.0.1"}])
        else:
            self._send([])


serve(sys.argv[1], H)
