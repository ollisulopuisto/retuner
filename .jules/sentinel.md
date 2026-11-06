## 2026-10-31 - Receiver MAC Parameter Path Traversal Prevention
**Vulnerability:** HTTP query parameter `mac` was passed directly to bookmark lookup routines in `internal/server/server.go`, potentially allowing path traversal sequences (such as `../`) to be processed.
**Learning:** While the `favorite` endpoint had regex validation on the `mac` parameter, other endpoints (`bookmarkList`, `station`, and `login`) lacked equivalent validation at the HTTP boundary.
**Prevention:** Validate all receiver identifiers against `safeBookmarkMAC` (`^[A-Za-z0-9_-]{1,64}$`) at the request boundary before handing them to internal storage layers.

## 2026-11-01 - INI section and line injection via CRLF in structured file generators
**Vulnerability:** Structured text generators (such as INI, YAML, or XML writers) receiving string fields from JSON endpoints must sanitize line terminators (`\r`, `\n`) and syntactic delimiters (e.g. `[` and `]`) in additions and updates.
**Learning:** Even when field separators like `=` and `|` were replaced, unescaped newlines in JSON payloads allowed injecting entire sections and arbitrary key-value pairs into the generated `.ini` file.
**Prevention:** Always strip or sanitize carriage returns, newlines, and section delimiter brackets when serializing user-supplied JSON data to line-based config formats.

## 2026-11-02 - Query-Specific Fallback Path Validation Across Multi-Purpose Endpoints
**Vulnerability:** Multi-purpose endpoints that route secondary flows (such as search endpoints multiplexed for direct playback requests) can bypass parameter validation enforced on dedicated endpoints if parameters are handled conditionally.
**Learning:** When endpoint parameters like `mac` or fallback values like `r.Host` are forwarded internally to file lookup or storage routines, any secondary control paths must enforce the same boundary validation as primary endpoints.
**Prevention:** Perform boundary validation on receiver identifiers across all routed query types before internal dispatch.

## 2026-11-05 - File Cache Identifier Sanitization in Multi-Tenant Caching Layers
**Vulnerability:** File cache keying schemes that incorporate receiver identifiers into on-disk file paths can allow path traversal if the caching subsystem does not independently validate the identifier format.
**Learning:** While perimeter request handlers perform input validation, internal caching abstractions (such as catFile cache) constructing file paths via `filepath.Join` must not assume inputs are already validated; any unvalidated or empty identifier must safely fall back.
**Prevention:** Apply defense-in-depth boundary validation (`^[A-Za-z0-9_-]{1,64}$`) within internal file caching layers before constructing paths or glob patterns in cache directories.

## 2026-11-06 - Receiver Configuration File Path Validation
**Vulnerability:** Configuration loaders constructing file paths in configuration directories could traverse outside the target directory if passed relative path sequences in filenames.
**Learning:** Lower-level configuration loaders called by multi-tenant subsystems must independently enforce path containment rather than assuming callers have validated file arguments.
**Prevention:** Require configuration file names to be strictly local single filenames without path separators using `filepath.IsLocal` and separator checks before joining paths.