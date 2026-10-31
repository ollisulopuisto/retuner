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
| ini defaults + avr.ini (common.pas, avr.pas)        | partial | smoke-test default-port checks pass; INIVersion: iniversion-parity.sh (version missing/old in retuner.ini and avr.ini) leaves the same INIVersion lines in both files and logs the same warning text as Pascal (log prefix/timestamp not compared; tested for the common avr.ini only, not <mac>.ini); section/key lookup is case-insensitive like Pascal's TIniFile: existing keys are updated in place keeping the user's spelling and position, no differently-cased duplicates (iniversion-parity.sh compares the whole rewritten files incl. a mixed-case input) |
| login/about/empty/statxml routes, /ytuner/ legacy, Frontier deep path | done | smoke-test 14/14, parity-diff 16/16 |
| icons (httpserver.pas GetIcon)                      | done*   | icon-parity.sh: podcast-episode logo fetch (feed logo URL from the cache only, like Pascal), placeholder fallback (no logo, unknown id), bad-id 404 (ctNone rendered as text/html; charset=utf-8, as Pascal's response layer does), HEAD, refetch behaviour (mock request counts) - statuses, headers, image format and 200x200 dimensions all identical to Pascal. *Body bytes diverge BY DESIGN: Pascal 26.08.28.4's FPC JPEG writer emits a defective stream (Go image/jpeg rejects it outright; ffmpeg decodes only the top MCU rows then reports 66 errors; a stray LF follows EOI), while Go serves a valid JPEG of the same mark - byte-parity with the defect is neither possible nor desirable. Go sends Connection: close on icon 200s where Pascal closes silently (curl -X HEAD in the smoke test waits otherwise) |
| my stations (my_stations.pas)                       | partial | ini+yaml parse, merge incl. preset files, own-file auto refresh by content CRC, config-dir migration, paging, play redirect, id lookup; parity-diff 28 same. Not ported: relay URLs, ResolvePlaylists, logo fetch |
| bookmarks (bookmark.pas)                            | partial | add/del/list/lookup, same file format incl. mixed <X/> vs <X></X>; 24-step stateful sequence identical to Pascal incl. file bytes. Not ported: per-MAC avr.ini translator, non-common MAC files only lightly tested. Deliberate divergence: MAC is sanitised before use as a file name (CodeQL fix commits cfe1f3b/8397f18, and the favourite handler now answers 400 for a mac outside `[A-Za-z0-9_-]{1,64}`: that is Pascal's own IsSafeAVRMAC rule (src/avr.pas, AVR_MAC_MAX_LENGTH; a Denon sends 32 hex), but Pascal's bookmark handler itself does not reject, so a colon/dot MAC is refused here and accepted there; real receivers' MAC formats unverified; test TestPerMACFileNames) |
| radio-browser (radiobrowser.pas)                    | partial | test-radiobrowser.sh passes (28/28, same as Pascal); rb-parity.sh: pages, paging, search, id lookup, play redirect and the upstream requests are identical against a mock (icon: status/type only). catNone and catMemStr work; catFile is on disk: catfile-parity.sh shows byte-identical .cache files (same serialisation, category file names identical), cache reused after restart without upstream requests, and the same refetch count when files are older than RBCacheTTL; station list file names reproduce FreePascal's string.GetHashCode (h = h*31 xor c over the ASCII-folded bytes, Abs), so Go and Pascal share the whole cache folder, station lists included (catfile-parity.sh: file names and sizes identical, cross reads need zero upstream requests); Pascal's startup removal of old files (RemoveOldRBCacheFiles) is ported (`RemoveOldCacheFiles`, oldcache-parity.sh: same files removed and kept as Pascal, TTL 1 and 0; Pascal's startup index is ported too, see `catindex-parity.sh`: a file dropped into the cache folder while running is ignored and refetched, in both). The SQLite types (catDB/catMemDB/catPermMemDB) are implemented with modernc.org/sqlite (pure Go, BSD-3) driving the unchanged src/sql/*.sql scripts (embedded); the default catPermMemDB builds rb.db at first start (5 bulk upstream requests, then none per request), restores it on restart when fresh, rebuilds when RBCacheTTL hours have passed. rb-parity.sh with RB_CACHE=catPermMemDB, catDB, catMemDB: pages and upstream requests identical to Pascal (against the mock, 8-station data only). A failed build disables Radio Browser with an ERROR; it never falls back to live API calls. Unverified: Pascal-exact behaviour with large real directories (chunking, memory), TTL refresh timer, the "{empty}" placeholder mapping, rb.db files from Pascal being read by Go and vice versa. rbuuids.txt is read/written like Pascal. Relay URLs, playlist resolution and the translator (Translate/ReplaceDiacritics) are not applied. Radio Browser database queries bind category/search/id as SQL parameters and check the view name and ORDER BY column as identifiers (CodeQL findings; repaired after the autofix commits left db.go uncompilable). Deliberate divergence: a search text with a space is URL-encoded (Pascal sends it raw and the request fails) |
| per-MAC avr.ini (CommonAVRini=0)                    | done    | config/<mac>.ini read on first sight, filters/sorting/menu/protocol per receiver; MAC validated like IsSafeAVRMAC; unit test; rb-parity covers the test-radiobrowser CommonAVRini cases. Outdated-INIVersion warning not ported |
| presets (presets.pas)                               | done*   | test-presets.sh 15/15 (Pascal also 15/15), unit tests. *Oversize download cap behaviour (Pascal terminates the transfer) only assumed |
| podcasts (podcasts.pas)                             | done*   | podcasts-parity.sh: feed list, paging, episode lists (100 limit, non-audio/no-enclosure/empty-title/date rules, bad XML, 404, empty), station info, play redirects and the requests the feed mock saw (UA, refetch counts) identical to Pascal over 44 replies. *Mock feeds only for the suite (the opt-in podcasts-real-parity.sh matches on six real feeds, 442 episodes); no TLS feed fetch tested; favourite-add of an episode is not covered by a parity check; the podcast icon fetch is covered by icon-parity.sh (see the icons row) - Go's XML parser may differ from laz2_XMLRead on exotic markup (DTDs, odd encodings) |
| DNS server (dnsserver.pas)                          | partial | miekg/dns. fuzz-test.sh DNS phase passes (malformed packets, intercept list 14/14, control name); dns-parity.sh: raw replies identical for 8 cases (A, AAAA, MX, case, apex, not intercepted without upstream). Go unit tests (loopback upstream) cover forwarding, RestrictForwarding, AllowedClients, 24 h known-client expiry and upstream-down (rcode 4); still NOT compared against Pascal on the wire (needs a controllable upstream for the reference binary). TCP is not offered, matching the Pascal comment that interception is UDP only. Later fuzz-test phases (radio-browser, icons) fail until those are ported |
| web GUI stations editor (webgui.pas)                | done*   | test-webgui.sh 13/13 (Pascal 13/13); webgui-parity.sh: pages, JSON replies (same fpjson layout), save-to-ini output identical. *Divergence: a body that is not JSON answers "Malformed request." instead of the Pascal parser message. Listener takes all interfaces and filters by WebGUIIPAddress, like the release build |
| receiver remote (avrremote.pas)                     | done*   | test-remote.sh 42/42 checks (Pascal 42) against mock-denon. *Network error wording differs (Go net errors); not tested against a real receiver |
| updater (script/retuner-update.sh)                  | done*   | test-update.sh 32/32 for Pascal and Go. *The shell script is shared; it had a pre-existing `-p "$PORT"` bug on master (fixed). The Go binary is only probed, not self-updating |
| playlist resolution (ResolvePlaylists)              | done*   | playlist-parity.sh (pls/m3u/asx/xspf, nested, empty, self-reference, 70 KB, 404; resolve=1 and 0): listings, play redirects and the requests the mock saw identical to Pascal. *Relay URLs (NeedsRelay) are not handled; Content-Length now always sent (Pascal never chunks) |
| web GUI security                                    | tested  | webgui_test.go: client filter runs before auth (503, no challenge), IPv6/mapped/malformed entries refused, foreign Host without credentials gets 401 only, non-JSON POST refused before any write. Like Pascal there is no Host-header check: credentials plus the client filter are the only gates |
| updater: functional parity                          | partial | The updater is the shared shell script, not code in either binary, and Pascal has no self-update. It works with a Go release only if the archive keeps the retuner-<ver>-<target>/retuner layout; packaging exists in the build workflow (see Coordination), not changed here |
| HTTPS relay (relayserver.pas)                       | done*   | relay-parity.sh: listing points https stations at the relay; relay replies (headers, first 3000 bytes, ICY handling, 404, 502 path, reconnects) and upstream request logs identical to Pascal. *Upstream is plain http in the test (no TLS mock), so https fetch is Go's TLS stack, untested against a real https stream. Podcast episode ids resolve for the relay (not covered by relay-parity). Timing-based (2 s reconnect pause) TLS upstream: internal/relay TestHTTPSUpstream relays an https upstream (test CA trusted through the Transport hook) and refuses an untrusted certificate with 502 and no audio (Go verifies against the system roots, no insecure fallback). Not compared with Pascal: whether Pascal's OpenSSL client verifies certificates is unverified, and no real TLS stream was tried. |
| maintenance (/retuner/down)                         | done*   | maintenance-parity.sh: 503 for another client address (127.0.0.2 vs 127.0.0.1), 404, shutdown reply text and the process exiting: identical to Pascal incl. content types. *Shutdown is os.Exit after the reply; Pascal frees resources in order and logs each step (log wording shortened) |
| translator: ReplaceDiacritics                       | partial | internal/translator (tables generated from translator.pas). Radio Browser station names/countries: rb-parity.sh with RB_DIAC=1 (mock names with Latin-1/Ext-A/Ext-B letters, avr.ini flags on) identical to Pascal, 138 compared bytes-for-bytes. Bookmark listing names too (bookmark-diacritics-parity.sh, flags 0 and 1 identical to Pascal, one station). NOT done: the online Google Translate step (Translate/GoogleTranslate/translator file) - unsupported, never calls the network, so [Translator] Enable=1 has no effect; the Go build logs a WARNING about it (not covered by a test) |
| translator: online translate                        | todo    | no suite beyond test-leaks (not meaningful yet) |
| translator, threadtimer                             | todo    | |

## Notes

* An earlier stub Go scaffold (about 530 lines, merged in #45) answered only the
  token handshake and a main menu; it has been replaced in this branch.
* The Pascal server closes the connection after every response. The Go icon
  handler does the same for HEAD because `curl -X HEAD` in the smoke test waits
  for a body otherwise.
* `AppVersion` is pinned to the Pascal build under comparison (26.08.28.4) so
  bodies that embed it match. Versioning of the Go binary is an open decision.

## Coordination

This repository has no AGENTS.md, CODEX.md, board, roadmap, TODO or bugs file (checked on master b6d15c8); `CLAUDE.md` is guidance only. Until one exists, this file is the record for the Go port.

- Claim: the Go port, branch `go-port-parity`, draft PR #49. No other open PR touches Go code (PR #49 was the only open PR when checked). Shared files touched outside the port: `script/retuner-update.sh` (a `-p "$PORT"` fix); no workflow files.
- Merges are human only. Not released, not a drop-in.
- Replaces an earlier Go scaffold: master already carried "Initial Go port of Retuner foundation" (a7e3ccc, a bot-authored skeleton: `common/`, `conf/`, `dns/`, `http/`, `vtuner/`, a small `main.go`, `-p`/`-c` flags). This branch deletes those packages and builds the port under `internal/` with a new `main.go` (Pascal ignores command-line arguments, so `-p`/`-c` are gone on purpose). The scaffold stays recoverable at a7e3ccc and in master's history.
- Rollback: the branch is additive to the Pascal sources, which are untouched. Reverting the merge (or not merging) restores the scaffold and the Pascal build; nothing else depends on it.
- Packaging fix, APPLIED by the maintainer as 4e16edb (the PAT lacks `workflow` scope, so the patch was applied by hand; first GitHub run of it is on 4e16edb): the bot scaffold (7d5af41) had replaced the Pascal release workflow with a Go matrix that hard-coded `2.0.0`, lost `workflow_call`/`release_tag` (so `release.yml` could not call it), lost the publish job and SHA256SUMS, and used the retired `macos-13` label (jobs queue for ever). The patch restores: `workflow_call` with `release_tag`, archive version from the tag (else `APP_VERSION` in `src/common.pas`), concurrency group, the publish job with SHA256SUMS (runs only when a tag is passed), `macos-15-intel`. A Go test keeps `AppVersion` equal to `APP_VERSION`. No new triggers: nothing publishes unless a human runs `release.yml`. Tested locally: YAML parses, test-update.sh passes; the workflow has not run on GitHub.
- Earlier packaging notes: `script/build.sh` and the `build` workflow already build and package Go (from the scaffold commit). Checked locally: `script/build.sh` builds this branch, and the archive laid out as the workflow does (`retuner-<ver>-<target>/retuner`, `retuner.ini`, `config/`) extracts and starts. Not changed on this branch (no workflow edits). Existing mismatch worth a human look: `binaries.yml` hard-codes version `2.0.0` in the archive name, while `script/retuner-update.sh` asks for `retuner-<release tag>-<target>.tar.gz` and `release.yml` reads the version from `src/common.pas`. The Go binary's AppVersion is pinned to 26.08.28.4 to match the Pascal release.
- Open work, in order: the gaps in the table above (real receivers, TLS relay upstream against a real https stream, favourite-add parity for podcast episodes and bookmarks beyond the diacritics sequence, online translation unsupported by choice). Real Radio Browser data and six real podcast feeds are covered by the opt-in rb-real-parity.sh / podcasts-real-parity.sh (both matching; not in CI because they need the network).
- Blockers: none at the time of writing.

## Podcast exotic-feed parity (added)

`script/podcasts-parity.sh` now also serves ~17 odd feeds (CDATA, entities, namespaces, BOM, Latin-1, invalid UTF-8, doctype, comments, unclosed, Atom, ...). First comparison found three real differences, now fixed and matching Pascal (bodies, headers, request counts):
1. `itunes:title` before `title` - Go matched the prefixed element; now element names keep their prefix.
2. ISO-8859-1 feeds were not decoded; now converted. Invalid UTF-8 bytes become `?`; a BOM is stripped.
3. Mismatched end tags are a parse error (as in Pascal), so fetch counts agree.
Caveat: only these synthetic feeds were compared; real-world feeds are untested.

## CI note

Smoke/Test steps hung for six hours on the old scaffold build. Fixed by curl timeouts in `script/` and job timeouts in the workflows; push runs are limited to master, PRs run on their merge result, `workflow_dispatch` is check-only. `release.yml` is the only release path.

## Real Radio Browser data (opt-in, not CI)

`script/rb-real-parity.sh` runs both binaries against the REAL public directory (62k stations, read-only GETs) through a snapshot proxy, so both see identical bytes; the Pascal binary here has no usable OpenSSL, hence plain http to the proxy. With catPermMemDB (the default) and catMemStr: country, genre, language, popular, search and two category pages, byte-identical after one fix. The real data found a difference synthetic mocks could not: Pascal's StripHttps replaces the FIRST `https://` anywhere in the URL (for `http://host/proxy/?q=https://...`), Go only a leading one. Fixed. Still untested: real TLS from the binaries, all-station fan-out beyond those pages, real receivers.
Also added: the `Status: <code> OK` response header Pascal sends (`404 OK` too).

`script/podcasts-real-parity.sh` (opt-in): six real feeds (NPR, BBC, Lex Fridman, a 20 MB Simplecast feed, Guardian, Huberman; 442 episodes listed) served as identical local bytes: lists, paging, station info and play redirects are byte-identical between Pascal and Go. Real feeds fetched by the binaries over https were not tested.

Go's own https to the real Radio Browser (de1.api.radio-browser.info, catMemStr, 14:26 on 2026-10-29, manual check, no script): country list (200 items), Finland (148 stations), search "yle" (100) and popular (100) all served, no TLS or certificate errors. Pascal could not be compared here (no OpenSSL). The TTL refresh timer was not exercised against the real service.
