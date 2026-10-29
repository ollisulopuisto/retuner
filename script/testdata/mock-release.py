#!/usr/bin/env python3
"""Stands in for GitHub's release endpoints, for script/test-update.sh.

    mock-release.py <port> <version> <dir-with-assets> [digest-mode]

Serves the three things the updater asks for:

    /<owner>/<repo>/releases/latest                     -> 302 to the tag URL
    /<owner>/<repo>/releases/download/v<ver>/<asset>    -> the file
    /<owner>/<repo>/releases/download/v<ver>/SHA256SUMS -> the checksums

digest-mode is "good" (the real sha256), "bad" (a wrong one, so the updater
must refuse) or "none" (no SHA256SUMS published at all, which must not stop
it - releases made before that file existed have none).

The checksums come from a published file rather than the REST API because the
API has no per-asset digest. The first version of this served one in JSON, and
so tested a field GitHub does not send.
"""
import hashlib
import http.server

from localserver import serve
import json
import os
import sys

PORT = int(sys.argv[1])
VERSION = sys.argv[2]
ASSET_DIR = sys.argv[3]
DIGEST_MODE = sys.argv[4] if len(sys.argv) > 4 else "good"


def checksums():
    lines = []
    for name in sorted(os.listdir(ASSET_DIR)):
        if not name.endswith(".tar.gz"):
            continue
        with open(os.path.join(ASSET_DIR, name), "rb") as f:
            real = hashlib.sha256(f.read()).hexdigest()
        lines.append("%s  %s" % ("0" * 64 if DIGEST_MODE == "bad" else real, name))
    return "\n".join(lines) + "\n"


# ASSET_DIR luetaan lazily ja uudelleen jokaisella download-pyynnöllä (ei
# välimuistissa), jotta testien myöhään luomat assetit näkyvät ja vanha
# sisältö ei vuoda testistä toiseen. Lukeminen itsessään on turvallista:
# ASSET_DIR tulee komentoriviltä, ei koskaan HTTP-pyynnöstä, ja tiedostot
# avataan os.listdirin palauttamilla nimillä - ei koskaan pyynnöstä
# johdetulla nimellä. Sen jälkeen assetti palvellaan tästä sanakirjasta, joten
# pyyntöpolusta johdettu data ei koskaan muodostu tiedostopoluksi. Tämä
# katkaisee CodeQL:n py/path-injection -datan kulkun pyynnöstä levylle:
# open()-kutsussa ei ole enää pyyntödataa lainkaan.
def assets():
    """Palauta {nimi: tavut} -sanakirja ASSET_DIRin .tar.gz-tiedostoista.

    Avainten nimien on oltavia todellisia, suoraan ASSET_DIRissa olevia
    tiedostoja; symlinkit ohitetaan, ettei koskaan seurata polkua joka voi
    johtaa ASSET_DIRin ulkopuolelle.
    """
    result = {}
    try:
        names = os.listdir(ASSET_DIR)
    except OSError:
        return result
    for name in sorted(names):
        if not name.endswith(".tar.gz"):
            continue
        full = os.path.join(ASSET_DIR, name)
        if os.path.islink(full) or not os.path.isfile(full):
            continue
        with open(full, "rb") as f:
            result[name] = f.read()
    return result


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, code, body, ctype="application/json"):
        body = body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path
        if path.endswith("/releases/latest") and path.startswith("/repos/"):
            self._send(200, json.dumps({"tag_name": "v" + VERSION}))
            return
        # Served behind a redirect, because that is what GitHub does: a release
        # download URL answers 302 to release-assets.githubusercontent.com. The
        # first version of this handed the file back directly, so the suite
        # passed against an updater whose curl had no -L and which therefore
        # never read a checksum in its life.
        #
        # The redirect target is tested first on purpose: it also ends in
        # /SHA256SUMS, so checking the suffix first sends it back to itself and
        # curl gives up after fifty hops - which looks exactly like the missing
        # -L this phase exists to catch.
        if path == "/redirected/SHA256SUMS":
            self._send(200, checksums(), "text/plain")
            return
        if path.endswith("/SHA256SUMS"):
            if DIGEST_MODE == "none":
                self._send(404, "not published", "text/plain")
            else:
                self.send_response(302)
                self.send_header("Location", "/redirected/SHA256SUMS")
                self.send_header("Content-Length", "0")
                self.end_headers()
            return
        if path.endswith("/releases/latest"):
            # The updater reads the version out of where this lands, exactly as
            # it does against the real github.com.
            self.send_response(302)
            self.send_header("Location", path[: -len("latest")] + "tag/v" + VERSION)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if "/releases/tag/" in path:
            self._send(200, "", "text/html")
            return
        if "/releases/download/" in path:
            name = path.rsplit("/", 1)[-1]
            # Assetti haetaan yllä olevasta sanakirjasta: pyyntöpolusta
            # johdettu nimi käy vain avaimena, eikä se koskaan päädy
            # tiedostopoluksi. Kaikki polunmuokkausyritykset (.., symlinkit,
            # koodatut merkit) jäävät sanakirjan ulkopuolelle ja palauttavat
            # 404:n.
            body = assets().get(name)
            if body is None:
                self._send(404, "no such asset", "text/plain")
                return
            self.send_response(200)
            self.send_header("Content-Type", "application/gzip")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self._send(404, "not found", "text/plain")


serve(PORT, Handler)
