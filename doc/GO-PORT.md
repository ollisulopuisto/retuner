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
| my stations (my_stations.pas)                       | todo    | |
| bookmarks (bookmark.pas)                            | todo    | |
| radio-browser + db (radiobrowser*.pas)              | todo    | test-radiobrowser.sh |
| presets (presets.pas)                               | todo    | test-presets.sh |
| podcasts (podcasts.pas)                             | todo    | |
| DNS server (dnsserver.pas)                          | todo    | fuzz-test.sh (DNS packets) |
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
