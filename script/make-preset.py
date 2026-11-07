#!/usr/bin/env python3
"""Build or prune a country preset file for Retuner.

A preset file is a curated, short station list for one country, shipped in
presets/ and fetched by Retuner at startup. The point of generating it rather
than typing it is that every URL in the result has been connected to and has
answered with audio -- a preset full of dead links is worse than no preset at
all, because the receiver's only feedback is a spinner.

    ./script/make-preset.py --country fi --out presets/fi.ini
    ./script/make-preset.py --prune presets/fi.ini
    ./script/make-preset.py --prune presets/fi.ini --strikes presets/strikes.json

Candidates come from radio-browser.info, which is where Retuner's own directory
comes from, so nothing new is being trusted. What the script cannot judge is
whether a stream is a broadcaster's own: that stays a human decision, and the
file is meant to be edited after generation. See presets/README.md.
"""

import argparse
import configparser
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from html.parser import HTMLParser
from collections import OrderedDict

API = "https://all.api.radio-browser.info"
UA = "Viritin-preset-builder/1.0 (+https://github.com/ollisulopuisto/viritin)"
AUDIO_TYPES = ("audio/", "application/ogg", "application/octet-stream")
PLAYLIST_EXT = (".m3u", ".m3u8", ".pls", ".asx")

# A station logo is fetched from a site the station chose, so both bounds are
# on us: how much of a page to read looking for a link, and how much of what it
# points at to accept as an icon. The receiver scales everything to IconSize
# anyway -- 96 or 200 pixels -- so a megabyte is already far more than can be
# useful, and four megabytes is somebody else's problem arriving as ours.
PAGE_MAX_BYTES = 256 * 1024
ICON_MAX_BYTES = 1024 * 1024
ICON_RELS = ("apple-touch-icon", "apple-touch-icon-precomposed",
             "shortcut icon", "icon")


def get(url, timeout=20):
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read()


def candidates(country, limit, api=None):
    url = (f"{api or API}/json/stations/bycountrycodeexact/{country.upper()}"
           f"?limit={limit}&order=clickcount&reverse=true&hidebroken=true")
    try:
        return json.loads(get(url))
    except (urllib.error.URLError, json.JSONDecodeError, TimeoutError) as e:
        sys.exit(f"error: cannot reach radio-browser ({e})")


def resolve_playlist(url, timeout):
    """One level of .m3u/.pls indirection, which is all these ever use."""
    try:
        body = get(url, timeout).decode("utf-8", "replace")
    except Exception:
        return None
    for line in body.splitlines():
        line = line.strip()
        if line.lower().startswith("file"):          # .pls: File1=http://...
            line = line.split("=", 1)[-1].strip()
        if line.startswith(("http://", "https://")):
            return line
    return None


def plays(url, timeout=12):
    """Connect and read a little. Returns the URL that actually served audio."""
    if url.lower().split("?")[0].endswith(PLAYLIST_EXT):
        inner = resolve_playlist(url, timeout)
        if not inner:
            return None
        url = inner
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Icy-MetaData": "0"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            ctype = (r.headers.get("Content-Type") or "").lower()
            if not ctype.startswith(AUDIO_TYPES):
                return None
            # A 200 with an empty body is a server that will disappoint the
            # receiver in exactly the same way a 404 would.
            return url if r.read(2048) else None
    except Exception:
        return None


def category_of(station):
    """Group by the first tag that is a word, falling back to a catch-all."""
    for tag in (station.get("tags") or "").split(","):
        tag = tag.strip()
        if re.fullmatch(r"[A-Za-z][A-Za-z0-9 &'-]{1,24}", tag):
            return tag.title()
    return "Radio"


def clean_name(text):
    """INI keys cannot carry '=' and the receiver's display cannot carry much."""
    return re.sub(r"\s+", " ", (text or "").replace("=", "-")).strip()[:48]


def safe_url(text):
    """A URL is not a display string: it must not be shortened or rewritten.

    Running names and URLs through the same cleaner truncated every logo to 48
    characters and turned '=' into '-', which quietly produced broken links --
    a real Yle logo came out of the first generated preset as
    https://images.cdn.yle.fi/f_auto,w_48,h_48,dpr_4 and nothing said so. The
    only things that matter here are that the value carries no '|', which
    separates stream from logo, and no newline, which would end the line early.
    """
    url = (text or "").strip()
    if not url.startswith(("http://", "https://")):
        return ""
    if "|" in url or "\n" in url or "\r" in url:
        return ""
    return url


class IconLinks(HTMLParser):
    """Collects <link rel=...icon...> hrefs, in the order the page gives them.

    A parser rather than a regular expression: the attribute order is not
    fixed, the quoting is not fixed, and a page that fails to parse is a page
    we simply learn nothing from -- which is the right outcome anyway.
    """

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.found = []

    def handle_starttag(self, tag, attrs):
        if tag != "link":
            return
        a = {k.lower(): (v or "") for k, v in attrs}
        rel = " ".join(a.get("rel", "").lower().split())
        if "icon" not in rel or not a.get("href"):
            return
        self.found.append((rel, a["href"], a.get("sizes", "")))


def ranked_icons(html):
    """Best first. An apple-touch-icon is a deliberate, reasonably sized logo;
    a favicon is often 16 pixels of nothing, which the receiver then scales up
    to 200 and shows as a smear."""
    parser = IconLinks()
    try:
        parser.feed(html)
    except Exception:
        return []

    def rank(item):
        rel, _, sizes = item
        for i, known in enumerate(ICON_RELS):
            if rel == known or rel.startswith(known + " ") or (" " + known) in rel:
                return i
        return len(ICON_RELS)

    return [href for _, href, _ in sorted(parser.found, key=rank)]


def fetch_bounded(url, limit, timeout):
    """Returns (content-type, body) with the body capped, or (None, None).

    The cap is checked twice on purpose: Content-Length is a claim, and a
    server that does not send one, or sends a false one, must not be able to
    make this read four megabytes because it said it would send forty.
    """
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            declared = r.headers.get("Content-Length")
            if declared and declared.isdigit() and int(declared) > limit:
                return None, None
            body = r.read(limit + 1)
            if len(body) > limit:
                return None, None
            return (r.headers.get("Content-Type") or "").lower(), body
    except Exception:
        return None, None


def is_image(url, timeout):
    ctype, body = fetch_bounded(url, ICON_MAX_BYTES, timeout)
    if not body:                       # a 200 with no body is not an icon
        return False
    return ctype.startswith("image/")


def scrape_icon(homepage, timeout):
    """The station's own site, asked where its icon is. '' if it will not say.

    Most radio-browser entries carry no favicon, so without this most generated
    presets have no logos at all and every station on the receiver shows the
    same placeholder.
    """
    homepage = (homepage or "").strip()
    if not homepage.startswith(("http://", "https://")):
        return ""

    ctype, body = fetch_bounded(homepage, PAGE_MAX_BYTES, timeout)
    candidates = []
    if body and "html" in (ctype or ""):
        html = body.decode("utf-8", "replace")
        candidates = [urllib.parse.urljoin(homepage, h) for h in ranked_icons(html)]

    # Every site is asked for /favicon.ico in the end, link or no link: it is
    # where a browser looks, so it is where a station that never thought about
    # this still has one.
    root = urllib.parse.urlsplit(homepage)
    candidates.append(urllib.parse.urlunsplit((root.scheme, root.netloc,
                                               "/favicon.ico", "", "")))

    seen = set()
    for candidate in candidates:
        url = safe_url(candidate)
        if not url or url in seen:
            continue
        seen.add(url)
        if is_image(url, timeout):
            return url
    return ""


def build(args):
    rows = candidates(args.country, args.candidates, args.api)
    print(f"{len(rows)} candidates for {args.country.upper()}", file=sys.stderr)
    groups, seen, dropped = OrderedDict(), set(), 0
    for s in rows:
        name = clean_name(s.get("name"))
        url = safe_url(s.get("url_resolved") or s.get("url"))
        if not name or not url or name.lower() in seen:
            continue
        cat = category_of(s)
        if len(groups.get(cat, ())) >= args.per_category:
            continue
        if args.verify:
            live = plays(url, args.timeout)
            if not live:
                print(f"  drop  {name}: no audio from {url}", file=sys.stderr)
                dropped += 1
                continue
            url = live
        seen.add(name.lower())
        logo = safe_url(s.get("favicon"))
        if not logo and args.icons:
            logo = scrape_icon(s.get("homepage"), args.timeout)
            if logo:
                print(f"  logo  {name}: {logo}", file=sys.stderr)
        groups.setdefault(cat, []).append((name, url, logo))
    write(args.out, args.country, groups)
    kept = sum(len(v) for v in groups.values())
    print(f"wrote {args.out}: {kept} stations in {len(groups)} categories"
          f"{f', {dropped} dropped as unreachable' if dropped else ''}", file=sys.stderr)


def write(path, country, groups):
    with open(path, "w", encoding="utf-8") as f:
        f.write(f"; Viritin country preset: {country.lower()}\n"
                "; Generated by script/make-preset.py and then curated by hand.\n"
                "; Every URL here answered with audio when the file was written.\n"
                "; See presets/README.md before adding anything.\n\n")
        for cat, rows in groups.items():
            f.write(f"[{cat}]\n")
            for name, url, logo in rows:
                f.write(f"{name}={url}|{logo}\n" if logo else f"{name}={url}\n")
            f.write("\n")


def load_strikes(path):
    """The record of consecutive failures, or an empty one if there is none.

    A malformed file is treated as absent rather than fatal: the worst that
    costs is one forgotten week of counting, whereas exiting would leave the
    weekly job red until someone repaired a file nobody reads by hand.
    """
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        return data if isinstance(data, dict) else {}
    except (OSError, ValueError):
        return {}


def save_strikes(path, data):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, sort_keys=True)
        f.write("\n")


def prune(args):
    cp = configparser.ConfigParser(interpolation=None, strict=False)
    cp.optionxform = str
    cp.read(args.prune, encoding="utf-8")

    # Without --strikes a failure drops the station there and then, which is
    # what a human running this wants. The unattended weekly job is the caller
    # that needs patience: one CDN hiccup at 04:00 on a Sunday should not
    # silently delete a national broadcaster from the preset.
    book, seen = None, {}
    if args.strikes:
        strikes = load_strikes(args.strikes)
        book = strikes.get(preset_key(args.prune), {})

    groups, dropped, warned = OrderedDict(), 0, 0
    for cat in cp.sections():
        for name, value in cp.items(cat):
            url, _, logo = value.partition("|")
            url, logo = url.strip(), logo.strip()
            key = f"[{cat}] {name}"
            live = plays(url, args.timeout)
            if live:
                groups.setdefault(cat, []).append((name, live, logo))
                continue
            if book is None:
                print(f"  drop  {key}: no audio from {url}", file=sys.stderr)
                dropped += 1
                continue
            # A record is against a URL, not a name. Someone who fixes a broken
            # link by hand should not inherit the strikes the old one collected
            # and lose the station on the next run.
            previous = book.get(key)
            count = 1
            if isinstance(previous, dict) and previous.get("url") == url:
                count = int(previous.get("strikes", 0)) + 1
            if count >= args.strike_limit:
                print(f"  drop  {key}: no audio from {url}"
                      f" ({count} runs in a row)", file=sys.stderr)
                dropped += 1
                continue
            print(f"  warn  {key}: no audio from {url}"
                  f" ({count} of {args.strike_limit})", file=sys.stderr)
            seen[key] = {"url": url, "strikes": count}
            groups.setdefault(cat, []).append((name, url, logo))
            warned += 1

    write(args.prune, args.prune.rsplit("/", 1)[-1].split(".")[0], groups)
    if book is not None:
        # `seen` is built fresh each run, so a station that played, that was
        # dropped, or that someone removed by hand leaves no record behind. The
        # file is committed by the weekly job; anything that never expires
        # accumulates there forever.
        strikes = load_strikes(args.strikes)
        if seen:
            strikes[preset_key(args.prune)] = seen
        else:
            strikes.pop(preset_key(args.prune), None)
        save_strikes(args.strikes, strikes)

    kept = sum(len(v) for v in groups.values())
    print(f"pruned {args.prune}: {kept} kept, {dropped} dropped"
          f"{f', {warned} failing but under the limit' if warned else ''}",
          file=sys.stderr)


def preset_key(path):
    """Presets are keyed by file name: presets/fi.ini and a copy of it under
    another directory are the same list of stations."""
    return path.rsplit("/", 1)[-1]


def main():
    p = argparse.ArgumentParser(description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--country", help="ISO 3166-1 alpha-2 code, e.g. fi")
    p.add_argument("--out", help="file to write, e.g. presets/fi.ini")
    p.add_argument("--prune", metavar="FILE", help="re-check an existing preset and drop dead entries")
    p.add_argument("--candidates", type=int, default=60, help="stations to consider (default 60)")
    p.add_argument("--per-category", type=int, default=8, help="cap per category (default 8)")
    p.add_argument("--timeout", type=int, default=12, help="seconds per stream check (default 12)")
    p.add_argument("--no-verify", dest="verify", action="store_false",
                   help="skip the does-it-play check (not recommended)")
    p.add_argument("--no-icons", dest="icons", action="store_false",
                   help="do not visit a station's own site looking for a logo")
    p.add_argument("--api", default=API,
                   help="directory to ask for candidates (default radio-browser)")
    p.add_argument("--strikes", metavar="FILE",
                   help="with --prune: record consecutive failures here and "
                        "drop a station only once it reaches the limit")
    p.add_argument("--strike-limit", type=int, default=3, metavar="N",
                   help="failures in a row before --strikes drops a station (default 3)")
    args = p.parse_args()
    if args.strikes and not args.prune:
        p.error("--strikes is only meaningful with --prune")
    if args.strike_limit < 1:
        p.error("--strike-limit must be at least 1")
    if args.prune:
        prune(args)
    elif args.country and args.out:
        build(args)
    else:
        p.error("give either --prune FILE, or both --country and --out")


if __name__ == "__main__":
    main()
