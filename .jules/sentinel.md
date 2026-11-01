## 2026-10-31 - Receiver MAC Parameter Path Traversal Prevention
**Vulnerability:** HTTP query parameter `mac` was passed directly to bookmark lookup routines in `internal/server/server.go`, potentially allowing path traversal sequences (such as `../`) to be processed.
**Learning:** While the `favorite` endpoint had regex validation on the `mac` parameter, other endpoints (`bookmarkList`, `station`, and `login`) lacked equivalent validation at the HTTP boundary.
**Prevention:** Validate all receiver identifiers against `safeBookmarkMAC` (`^[A-Za-z0-9_-]{1,64}$`) at the request boundary before handing them to internal storage layers.

## 2026-11-01 - INI section and line injection via CRLF in structured file generators
**Vulnerability:** Structured text generators (such as INI, YAML, or XML writers) receiving string fields from JSON endpoints must sanitize line terminators (`\r`, `\n`) and syntactic delimiters (e.g. `[` and `]`) in additions and updates.
**Learning:** Even when field separators like `=` and `|` were replaced, unescaped newlines in JSON payloads allowed injecting entire sections and arbitrary key-value pairs into the generated `.ini` file.
**Prevention:** Always strip or sanitize carriage returns, newlines, and section delimiter brackets when serializing user-supplied JSON data to line-based config formats.