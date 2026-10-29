# Go port of Retuner (work in progress)

Status of the port from Free Pascal to Go. The Pascal sources in `src/` are
untouched and remain the reference implementation.

## Method

The shell suites in `script/` start a real binary and speak HTTP to it, so they
are an implementation-independent spec (see `CLAUDE.md`). The port is done in
stages; each stage is "done" when the relevant suite passes against the Go
binary **and** `script/parity-diff.sh` shows the Go port answering byte for byte
like the Pascal release for the same requests.

    go build -o retuner-go .
    ./script/smoke-test.sh ./retuner-go
    ./script/parity-diff.sh /path/to/pascal/retuner ./retuner-go
    go test ./...

## Layout

    main.go                  flags, config, listen
    internal/config          retuner.ini / avr.ini: defaults, key order, fill-missing
    internal/vtuner          vTuner XML pages (byte-compatible with src/vtuner.pas)
    internal/server          routes and handlers (src/httpserver.pas)

## Status

| Area (Pascal unit)                                  | Go      | Evidence |
|-----------------------------------------------------|---------|----------|
| vTuner XML (vtuner.pas)                             | done    | unit tests + parity-diff |
| ini defaults + avr.ini (common.pas, avr.pas)        | partial | smoke-test default-port checks pass; INIVersion upgrade/outdated-notice not ported |
| login/about/empty/statxml routes, /ytuner/ legacy, Frontier deep path | done | smoke-test 14/14, parity-diff 16/16 |
| icon placeholder (httpserver.pas)                   | partial | placeholder only; station logo fetch/scale/cache not ported |
| my stations (my_stations.pas)                       | partial | ini+yaml parse, merge incl. preset files, own-file auto refresh by content CRC, config-dir migration, paging, play redirect, id lookup; parity-diff 28 same. Not ported: relay URLs, ResolvePlaylists, logo fetch |
| bookmarks (bookmark.pas)                            | partial | add/del/list/lookup, same file format incl. mixed <X/> vs <X></X>; 24-step stateful sequence identical to Pascal incl. file bytes. Not ported: per-MAC avr.ini translator, non-common MAC files only lightly tested. Deliberate divergence: MAC is sanitised before use as a file name |
| radio-browser (radiobrowser.pas)                    | partial | test-radiobrowser.sh passes (28/28, same as Pascal); rb-parity.sh: pages, paging, search, id lookup, play redirect and the upstream requests are identical against a mock (icon: status/type only). catNone and catMemStr work; catFile is memory-only (no on-disk station cache yet). The SQLite types (catDB/catMemDB/catPermMemDB, radiobrowserdb.pas) are NOT implemented: the build logs an ERROR and keeps Radio Browser DISABLED rather than silently using the live API. Note the shipped retuner.ini default is catPermMemDB, so this build is not a drop-in until the DB cache is ported (plan: modernc.org/sqlite, pure Go BSD-3, driving the unchanged src/sql/*.sql scripts). rbuuids.txt is read/written like Pascal. Relay URLs, playlist resolution and the translator (Translate/ReplaceDiacritics) are not applied. Deliberate divergence: a search text with a space is URL-encoded (Pascal sends it raw and the request fails) |
| per-MAC avr.ini (CommonAVRini=0)                    | done    | config/<mac>.ini read on first sight, filters/sorting/menu/protocol per receiver; MAC validated like IsSafeAVRMAC; unit test; rb-parity covers the test-radiobrowser CommonAVRini cases. Outdated-INIVersion warning not ported |
| presets (presets.pas)                               | done*   | test-presets.sh 15/15 (Pascal also 15/15), unit tests. *Oversize download cap behaviour (Pascal terminates the transfer) only assumed |
| podcasts (podcasts.pas)                             | todo    | |
| DNS server (dnsserver.pas)                          | partial | miekg/dns. fuzz-test.sh DNS phase passes (malformed packets, intercept list 14/14, control name); dns-parity.sh: raw replies identical for 8 cases (A, AAAA, MX, case, apex, not intercepted without upstream). Go unit tests (loopback upstream) cover forwarding, RestrictForwarding, AllowedClients, 24 h known-client expiry and upstream-down (rcode 4); still NOT compared against Pascal on the wire (needs a controllable upstream for the reference binary). TCP is not offered, matching the Pascal comment that interception is UDP only. Later fuzz-test phases (radio-browser, icons) fail until those are ported |
| web GUI / remote / relay / maintenance / updater    | todo    | test-webgui.sh, test-remote.sh, test-update.sh |
| translator, threadtimer                             | todo    | |

## Notes

* An earlier stub Go scaffold (about 530 lines, merged in #45) answered only the
  token handshake and a main menu; it has been replaced in this branch.
* The Pascal server closes the connection after every response. The Go icon
  handler does the same for HEAD because `curl -X HEAD` in the smoke test waits
  for a body otherwise.
* `AppVersion` is pinned to the Pascal build under comparison (26.08.28.4) so
  bodies that embed it match. Versioning of the Go binary is an open decision.
