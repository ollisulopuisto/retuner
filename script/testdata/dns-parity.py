#!/usr/bin/env python3
"""Send the same DNS queries to two servers and compare the raw replies.

    dns-parity.py <ref-port> <new-port>

Used by script/dns-parity.sh to hold the Go port to what the Pascal build puts
on the wire, down to header flags and the compression pointer.
"""
import socket
import struct
import sys

REF, NEW = int(sys.argv[1]), int(sys.argv[2])


def ask(port, host, qtype=1, flags=0x0100, ident=0x1234):
    name = b"".join(bytes([len(p)]) + p for p in host.encode().split(b".")) + b"\0"
    pkt = struct.pack(">HHHHHH", ident, flags, 1, 0, 0, 0) + name + struct.pack(">HH", qtype, 1)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
        s.settimeout(2)
        s.sendto(pkt, ("127.0.0.1", port))
        try:
            return s.recv(4096).hex()
        except OSError:
            return "no reply"


cases = [
    ("radio.vtuner.com", 1), ("VTUNER.com", 1), ("vtuner.com", 1), ("a.b.c.wifiradiofrontier.com", 1),
    ("radio.vtuner.com", 28), ("radio.vtuner.com", 15), ("example.org", 1), ("notvtuner.com", 1),
]
bad = 0
for host, qt in cases:
    a, b = ask(REF, host, qt), ask(NEW, host, qt)
    if a == b:
        print("  same  %s type %d" % (host, qt))
    else:
        bad += 1
        print("  DIFF  %s type %d\n    ref %s\n    new %s" % (host, qt, a, b))
sys.exit(1 if bad else 0)
