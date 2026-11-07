#!/usr/bin/env python3
"""Renders the Viritin mark to PNG without any imaging library.

Shapes are signed distance fields, so edges get analytic anti-aliasing and
there is no supersampling to pay for. An antenna mast (amber) throwing signal arcs (teal): radio, over a network.
"""
import argparse
import math
import struct
import zlib

BG     = (0x16, 0x20, 0x2b)   # deep slate, same family as the editor's dark theme
SCALE  = (0x3f, 0xc1, 0xcb)   # editor accent teal
NEEDLE = (0xff, 0xb4, 0x54)   # dial-lamp amber
HUB    = (0xe6, 0xeb, 0xef)

def sd_round_box(px, py, cx, cy, hw, hh, r):
    dx, dy = abs(px - cx) - (hw - r), abs(py - cy) - (hh - r)
    outside = math.hypot(max(dx, 0.0), max(dy, 0.0))
    return outside + min(max(dx, dy), 0.0) - r

def sd_ring(px, py, cx, cy, rad, half):
    return abs(math.hypot(px - cx, py - cy) - rad) - half

def sd_capsule(px, py, ax, ay, bx, by, r):
    vx, vy, wx, wy = bx - ax, by - ay, px - ax, py - ay
    L = vx * vx + vy * vy
    t = 0.0 if L == 0 else max(0.0, min(1.0, (wx * vx + wy * vy) / L))
    return math.hypot(wx - vx * t, wy - vy * t) - r

def sd_disc(px, py, cx, cy, r):
    return math.hypot(px - cx, py - cy) - r

def sd_arc(px, py, cx, cy, rad, half, a0, a1):
    """Ring segment between two angles, with rounded caps."""
    dx, dy = px - cx, -(py - cy)
    ang = math.degrees(math.atan2(dy, dx)) % 360
    lo, hi = a0 % 360, a1 % 360
    inside = (lo <= ang <= hi) if lo <= hi else (ang >= lo or ang <= hi)
    if inside:
        return abs(math.hypot(dx, dy) - rad) - half
    best = 1e9
    for a in (a0, a1):
        ex, ey = polar(cx, cy, a, rad)
        best = min(best, math.hypot(px - ex, py - ey) - half)
    return best

def polar(cx, cy, ang, rad):
    a = math.radians(ang)
    return cx + rad * math.cos(a), cy - rad * math.sin(a)

def build(w, h):
    # An antenna mast with signal arcs either side: the mast is radio, the arcs
    # are the network. Symmetrical rather than the one-sided Wi-Fi fan, so it
    # reads as broadcast rather than as a router.
    cx = 0.5
    top, foot = 0.375, 0.715       # mast ends, unit coords of the shorter side
    px_unit = 1.0 / min(w, h)
    arcs = [(0.145, 0.030), (0.245, 0.030)]   # radius, half-width

    rows = []
    for y in range(h):
        row = bytearray()
        uy = (y + 0.5) / min(w, h)
        ux_off = (w - min(w, h)) / (2.0 * min(w, h))
        for x in range(w):
            ux = (x + 0.5) / min(w, h) - ux_off
            half_w = (w / min(w, h)) * 0.5
            d = sd_round_box(ux, uy, 0.5, 0.5, half_w, 0.5, 0.225)
            a_bg = max(0.0, min(1.0, 0.5 - d / px_unit))
            r, g, b = BG
            a = a_bg

            def over(sd, col):
                nonlocal r, g, b, a
                cov = max(0.0, min(1.0, 0.5 - sd / px_unit))
                if cov <= 0.0:
                    return
                cr, cg, cb = col
                r = cr * cov + r * (1 - cov)
                g = cg * cov + g * (1 - cov)
                b = cb * cov + b * (1 - cov)
                a = cov + a * (1 - cov)

            for rad, half in arcs:
                over(sd_arc(ux, uy, cx, top, rad, half, 18.0, 72.0), SCALE)
                over(sd_arc(ux, uy, cx, top, rad, half, 108.0, 162.0), SCALE)
            over(sd_capsule(ux, uy, cx, top, cx, foot, 0.024), NEEDLE)
            over(sd_capsule(ux, uy, cx - 0.115, foot, cx + 0.115, foot, 0.024), NEEDLE)
            over(sd_disc(ux, uy, cx, top, 0.052), HUB)

            row += bytes((int(r + 0.5), int(g + 0.5), int(b + 0.5), int(a * 255 + 0.5)))
        rows.append(bytes(row))
    return rows

def write_png(path, w, h, rows):
    raw = b"".join(b"\x00" + r for r in rows)
    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xffffffff)
    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 9))
           + chunk(b"IEND", b""))
    if path is not None:
        open(path, "wb").write(png)
    return png if path is None else len(png)


def png_bytes(w, h, rows):
    return write_png(None, w, h, rows)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--out", help="write one file here instead of the defaults")
    ap.add_argument("--size", type=int, nargs=2, metavar=("W", "H"), default=(512, 512))
    a = ap.parse_args()
    if a.out:
        n = write_png(a.out, a.size[0], a.size[1], build(a.size[0], a.size[1]))
        print(f"{a.out}  {a.size[0]}x{a.size[1]}  {n/1024:.1f} KB")
        return
    for path, w, h in (("viritin/icon.png", 512, 512), ("viritin/logo.png", 640, 320)):
        n = write_png(path, w, h, build(w, h))
        print(f"{path}  {w}x{h}  {n/1024:.1f} KB")


if __name__ == "__main__":
    main()
